package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
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
	repo       ports.JobRepository
	store      ports.FileStore
	bus        ports.EventSubscriber
	uploadSvc  *core.UploadService
	janitorSvc *core.JanitorService
	mux        *http.ServeMux
}

// NewServer initializes routes and returns a new Server instance.
func NewServer(repo ports.JobRepository, store ports.FileStore, bus ports.EventSubscriber, uploadSvc *core.UploadService, janitorSvc *core.JanitorService) *Server {
	s := &Server{
		repo:       repo,
		store:      store,
		bus:        bus,
		uploadSvc:  uploadSvc,
		janitorSvc: janitorSvc,
		mux:        http.NewServeMux(),
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
	s.mux.HandleFunc("POST /demo/upload", s.handleDemoUpload)
	s.mux.HandleFunc("POST /admin/janitor/cleanup", s.handleJanitorCleanup)
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
	jobs, err := s.repo.ListExpiredJobs(r.Context(), time.Now().UTC().Add(365*24*time.Hour), 50)
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

func (s *Server) handleDemoUpload(w http.ResponseWriter, r *http.Request) {
	demoType := r.URL.Query().Get("type")
	if demoType == "" {
		demoType = "image"
	}

	var filename string
	var contentType string
	var buf bytes.Buffer

	if demoType == "pdf" {
		filename = fmt.Sprintf("demo_invoice_%d.pdf", time.Now().Unix()%1000)
		contentType = "application/pdf"
		// Minimal valid PDF document
		buf.WriteString("%PDF-1.4\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>\nendobj\n4 0 obj\n<< /Length 55 >>\nstream\nBT /F1 24 Tf 100 700 Td (Sample Demo PDF Invoice) Tj ET\nendstream\nendobj\nxref\n0 5\n0000000000 65535 f \n0000000009 00000 n \n0000000058 00000 n \n0000000115 00000 n \n0000000214 00000 n \ntrailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n320\n%%EOF\n")
	} else {
		filename = fmt.Sprintf("demo_render_%d.png", time.Now().Unix()%1000)
		contentType = "image/png"
		// Generate colorful test image
		img := image.NewRGBA(image.Rect(0, 0, 400, 400))
		draw.Draw(img, img.Bounds(), &image.Uniform{color.RGBA{R: 79, G: 70, B: 229, A: 255}}, image.Point{}, draw.Src)
		for x := 0; x < 400; x++ {
			for y := 0; y < 400; y++ {
				if (x/20+y/20)%2 == 0 {
					img.Set(x, y, color.RGBA{R: uint8((x * 255) / 400), G: uint8((y * 255) / 400), B: 240, A: 255})
				}
			}
		}
		_ = png.Encode(&buf, img)
	}

	job, err := s.uploadSvc.ProcessUpload(r.Context(), filename, contentType, bytes.NewReader(buf.Bytes()), int64(buf.Len()), 0)
	if err != nil {
		http.Error(w, fmt.Sprintf("demo upload failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = templates.JobCard(job).Render(r.Context(), w)
}

func (s *Server) handleJanitorCleanup(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "true"

	if force {
		// Prune all jobs
		jobs, _ := s.repo.ListExpiredJobs(r.Context(), time.Now().UTC().Add(365*24*time.Hour), 1000)
		for _, j := range jobs {
			_ = s.store.Delete(r.Context(), j.FilePath)
			for _, a := range j.Artifacts {
				_ = s.store.Delete(r.Context(), a.FilePath)
			}
			_ = s.repo.DeleteJob(r.Context(), j.ID)
		}
	} else if s.janitorSvc != nil {
		_, _ = s.janitorSvc.PruneExpired(r.Context(), 100)
	}

	// Fetch remaining jobs and render updated list
	jobs, err := s.repo.ListExpiredJobs(r.Context(), time.Now().UTC().Add(365*24*time.Hour), 50)
	if err != nil {
		jobs = []*domain.Job{}
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var buf bytes.Buffer
	if len(jobs) == 0 {
		buf.WriteString(`<div class="text-center py-16 border border-dashed border-[#1e1e2d] rounded-2xl bg-[#0e0e16]/40"><div class="w-12 h-12 rounded-2xl bg-indigo-600/10 border border-indigo-500/20 text-indigo-400 flex items-center justify-center mx-auto mb-3 text-xl">⚡</div><h3 class="text-base font-semibold text-white">No active media tasks</h3><p class="text-xs text-slate-400 mt-1 max-w-sm mx-auto">Storage and database clean. Upload an image or document above or click "Quick Test".</p></div>`)
	} else {
		for _, job := range jobs {
			_ = templates.JobCard(job).Render(r.Context(), &buf)
		}
	}
	_, _ = w.Write(buf.Bytes())
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

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	job, err := s.repo.GetJobByID(r.Context(), jobID)
	if err != nil {
		if errors.Is(err, domain.ErrJobNotFound) {
			http.Error(w, "job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	renderCard := func(j *domain.Job) error {
		var buf bytes.Buffer
		if err := templates.JobCard(j).Render(r.Context(), &buf); err != nil {
			return err
		}
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

	if err := renderCard(job); err != nil {
		return
	}

	if job.Status == domain.StatusCompleted || job.Status == domain.StatusFailed {
		return
	}

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

	for {
		select {
		case <-r.Context().Done():
			return
		case <-eventChan:
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
		jobs, _ := s.repo.ListExpiredJobs(r.Context(), time.Now().UTC().Add(365*24*time.Hour), 100)
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
