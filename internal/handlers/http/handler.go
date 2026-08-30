package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/core"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/handlers/http/templates"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
)

// Server encapsulates the Web Gateway HTTP routing and handlers.
type Server struct {
	repo      ports.JobRepository
	store     ports.FileStore
	bus       ports.EventSubscriber
	uploadSvc *core.UploadService
	mux       *http.ServeMux
}

// NewServer initializes routes and returns a new Server instance.
func NewServer(repo ports.JobRepository, store ports.FileStore, bus ports.EventSubscriber, uploadSvc *core.UploadService) *Server {
	s := &Server{
		repo:      repo,
		store:     store,
		bus:       bus,
		uploadSvc: uploadSvc,
		mux:       http.NewServeMux(),
	}
	s.routes()
	return s
}

// ServeHTTP delegates request handling to the internal HTTP mux.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleDashboard)
	s.mux.HandleFunc("POST /upload", s.handleUpload)
	s.mux.HandleFunc("POST /api/v1/jobs/upload", s.handleUpload)
	s.mux.HandleFunc("GET /jobs/{id}/events", s.handleSSEEvents)
	s.mux.HandleFunc("GET /api/v1/jobs/{id}/stream", s.handleSSEEvents)
	s.mux.HandleFunc("GET /jobs/{id}", s.handleGetJob)
	s.mux.HandleFunc("GET /artifacts/{id}/download", s.handleDownloadArtifact)
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	// Fetch recent/active jobs
	jobs, err := s.repo.ListExpiredJobs(r.Context(), time.Now().UTC().Add(24*time.Hour), 50)
	if err != nil {
		jobs = []*domain.Job{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = templates.Dashboard(jobs).Render(r.Context(), w)
}

func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	// Parse multipart payload (up to 50MB)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		http.Error(w, fmt.Sprintf("invalid multipart form: %v", err), http.StatusBadRequest)
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "missing 'file' field in form data", http.StatusBadRequest)
		return
	}
	defer file.Close()

	contentType := header.Header.Get("Content-Type")
	if contentType == "" || contentType == "application/octet-stream" {
		// Infer from file extension if not provided
		ext := strings.ToLower(filepath.Ext(header.Filename))
		switch ext {
		case ".png":
			contentType = "image/png"
		case ".jpg", ".jpeg":
			contentType = "image/jpeg"
		case ".webp":
			contentType = "image/webp"
		case ".gif":
			contentType = "image/gif"
		case ".pdf":
			contentType = "application/pdf"
		}
	}

	job, err := s.uploadSvc.ProcessUpload(r.Context(), header.Filename, contentType, file, header.Size, 0)
	if err != nil {
		if errors.Is(err, domain.ErrUnsupportedMediaType) || errors.Is(err, domain.ErrInvalidJob) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		http.Error(w, fmt.Sprintf("upload failed: %v", err), http.StatusInternalServerError)
		return
	}

	// Check for HTMX request
	isHTMX := r.Header.Get("HX-Request") == "true"
	if isHTMX {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusAccepted)
		_ = templates.JobCard(job).Render(r.Context(), w)
		return
	}

	// Standard JSON response
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(job)
}

func (s *Server) handleSSEEvents(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		http.Error(w, "missing job id", http.StatusBadRequest)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	// SSE response headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	// 1. Initial State Query from PostgreSQL
	job, err := s.repo.GetJobByID(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, domain.ErrJobNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Helper to render and emit SSE card
	renderCard := func(j *domain.Job) error {
		var buf bytes.Buffer
		if err := templates.JobCard(j).Render(r.Context(), &buf); err != nil {
			return err
		}
		// SSE multiline payload format
		lines := strings.Split(buf.String(), "\n")
		fmt.Fprintf(w, "event: job-update\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				fmt.Fprintf(w, "data: %s\n", line)
			}
		}
		fmt.Fprintf(w, "\n")
		flusher.Flush()
		return nil
	}

	// Render initial state
	if err := renderCard(job); err != nil {
		return
	}

	// If already in terminal state, terminate connection cleanly
	if job.Status == domain.StatusCompleted || job.Status == domain.StatusFailed {
		return
	}

	// 2. Subscribe to live NATS status updates
	eventChan := make(chan ports.Event, 10)
	subject := fmt.Sprintf("jobs.status.%s", jobID)

	sub, err := s.bus.Subscribe(r.Context(), subject, func(ctx context.Context, evt ports.Event) error {
		select {
		case eventChan <- evt:
		default:
		}
		return nil
	})
	if err != nil {
		log.Printf("gateway: failed to subscribe to %s: %v", subject, err)
		return
	}
	defer func() {
		_ = sub.Unsubscribe()
	}()

	// 3. Event loop
	for {
		select {
		case <-r.Context().Done():
			return
		case <-eventChan:
			// Fetch updated job from repo to get full state + artifacts
			updatedJob, err := s.repo.GetJobByID(r.Context(), jobID)
			if err != nil {
				continue
			}

			_ = renderCard(updatedJob)

			if updatedJob.Status == domain.StatusCompleted || updatedJob.Status == domain.StatusFailed {
				return
			}
		}
	}
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	jobID := r.PathValue("id")
	if jobID == "" {
		http.Error(w, "missing job id", http.StatusBadRequest)
		return
	}

	job, err := s.repo.GetJobByID(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, domain.ErrJobNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = templates.JobCard(job).Render(r.Context(), w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(job)
}

func (s *Server) handleDownloadArtifact(w http.ResponseWriter, r *http.Request) {
	artID := r.PathValue("id")
	if artID == "" {
		http.Error(w, "missing artifact id", http.StatusBadRequest)
		return
	}

	// Check if repository supports GetArtifactByID or query across jobs
	type artifactGetter interface {
		GetArtifactByID(ctx context.Context, id string) (*domain.Artifact, error)
	}

	var targetArt *domain.Artifact
	if ag, ok := s.repo.(artifactGetter); ok {
		art, err := ag.GetArtifactByID(r.Context(), artID)
		if err == nil && art != nil {
			targetArt = art
		}
	}

	if targetArt == nil {
		// Fallback: search in recent jobs
		jobs, _ := s.repo.ListExpiredJobs(r.Context(), time.Now().UTC().Add(24*time.Hour), 100)
		for _, j := range jobs {
			for _, a := range j.Artifacts {
				if a.ID == artID {
					targetArt = &a
					break
				}
			}
			if targetArt != nil {
				break
			}
		}
	}

	if targetArt == nil {
		http.Error(w, "artifact not found", http.StatusNotFound)
		return
	}

	reader, err := s.store.Open(r.Context(), targetArt.FilePath)
	if err != nil {
		http.Error(w, "artifact file not found", http.StatusNotFound)
		return
	}
	defer reader.Close()

	ext := strings.ToLower(filepath.Ext(targetArt.FilePath))
	contentType := "application/octet-stream"
	switch ext {
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".webp":
		contentType = "image/webp"
	case ".pdf":
		contentType = "application/pdf"
	case ".txt":
		contentType = "text/plain; charset=utf-8"
	}

	downloadFilename := filepath.Base(targetArt.FilePath)
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, downloadFilename))
	if targetArt.FileSize > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", targetArt.FileSize))
	}

	_, _ = io.Copy(w, reader)
}
