package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/core"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
	gatewayHttp "github.com/SalvucciFacundo/media-engine-microservices/internal/handlers/http"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
	"github.com/google/uuid"
)

// Mock JobRepository for HTTP Handler tests
type mockRepo struct {
	mu        sync.Mutex
	jobs      map[string]*domain.Job
	artifacts map[string]*domain.Artifact
}

func newMockRepo() *mockRepo {
	return &mockRepo{
		jobs:      make(map[string]*domain.Job),
		artifacts: make(map[string]*domain.Artifact),
	}
}

func (m *mockRepo) CreateJob(ctx context.Context, job *domain.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	jCopy := *job
	m.jobs[job.ID] = &jCopy
	return nil
}

func (m *mockRepo) GetJobByID(ctx context.Context, id string) (*domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, domain.ErrJobNotFound
	}
	jCopy := *j
	var arts []domain.Artifact
	for _, art := range m.artifacts {
		if art.JobID == id {
			arts = append(arts, *art)
		}
	}
	jCopy.Artifacts = arts
	return &jCopy, nil
}

func (m *mockRepo) UpdateJobStatus(ctx context.Context, id string, status domain.JobStatus, errMsg *string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return domain.ErrJobNotFound
	}
	var errMsgs []string
	if errMsg != nil {
		errMsgs = append(errMsgs, *errMsg)
	}
	return j.TransitionTo(status, errMsgs...)
}

func (m *mockRepo) AddArtifact(ctx context.Context, artifact domain.Artifact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	artCopy := artifact
	m.artifacts[artifact.ID] = &artCopy
	return nil
}

func (m *mockRepo) ListExpiredJobs(ctx context.Context, now time.Time, limit int) ([]*domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var list []*domain.Job
	for _, j := range m.jobs {
		jCopy := *j
		list = append(list, &jCopy)
		if limit > 0 && len(list) >= limit {
			break
		}
	}
	return list, nil
}

func (m *mockRepo) DeleteJob(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, id)
	return nil
}

func (m *mockRepo) GetArtifactByID(ctx context.Context, id string) (*domain.Artifact, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	art, ok := m.artifacts[id]
	if !ok {
		return nil, errors.New("artifact not found")
	}
	artCopy := *art
	return &artCopy, nil
}

// Mock FileStore
type mockStore struct {
	mu    sync.Mutex
	files map[string][]byte
}

func newMockStore() *mockStore {
	return &mockStore{
		files: make(map[string][]byte),
	}
}

func (m *mockStore) Save(ctx context.Context, relativePath string, data io.Reader) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}
	m.files[relativePath] = b
	return relativePath, nil
}

func (m *mockStore) Open(ctx context.Context, relativeOrFullPath string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[relativeOrFullPath]
	if !ok {
		return nil, errors.New("file not found")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *mockStore) Delete(ctx context.Context, relativeOrFullPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, relativeOrFullPath)
	return nil
}

func (m *mockStore) Exists(ctx context.Context, relativeOrFullPath string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.files[relativeOrFullPath]
	return ok, nil
}

func (m *mockStore) GetPath(relativePath string) string {
	return relativePath
}

// Mock EventBus
type mockBus struct {
	mu        sync.Mutex
	published []ports.Event
	handlers  map[string]ports.EventHandler
}

func newMockBus() *mockBus {
	return &mockBus{
		handlers: make(map[string]ports.EventHandler),
	}
}

func (m *mockBus) Publish(ctx context.Context, subject string, event ports.Event) error {
	m.mu.Lock()
	m.published = append(m.published, event)
	handler, hasHandler := m.handlers[subject]
	m.mu.Unlock()

	if hasHandler && handler != nil {
		_ = handler(ctx, event)
	}
	return nil
}

func (m *mockBus) Subscribe(ctx context.Context, subject string, handler ports.EventHandler) (ports.SubscriptionCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[subject] = handler
	return &mockSubCloser{bus: m, subject: subject}, nil
}

func (m *mockBus) SubscribeQueue(ctx context.Context, subject, queueGroup string, handler ports.EventHandler) (ports.SubscriptionCloser, error) {
	return m.Subscribe(ctx, subject, handler)
}

type mockSubCloser struct {
	bus     *mockBus
	subject string
}

func (c *mockSubCloser) Unsubscribe() error {
	c.bus.mu.Lock()
	defer c.bus.mu.Unlock()
	delete(c.bus.handlers, c.subject)
	return nil
}

func setupTestServer(t *testing.T) (*gatewayHttp.Server, *mockRepo, *mockStore, *mockBus) {
	t.Helper()
	repo := newMockRepo()
	store := newMockStore()
	bus := newMockBus()
	uploadSvc := core.NewUploadService(repo, store, bus, 1*time.Hour)

	server := gatewayHttp.NewServer(repo, store, bus, uploadSvc)
	return server, repo, store, bus
}

func createMultipartRequest(t *testing.T, url, fieldName, filename, contentType string, content []byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	h := make(map[string][]string)
	h["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, filename)}
	h["Content-Type"] = []string{contentType}
	part, err := writer.CreatePart(h)
	if err != nil {
		t.Fatalf("failed to create part: %v", err)
	}
	if _, err := part.Write(content); err != nil {
		t.Fatalf("failed to write content: %v", err)
	}
	_ = writer.Close()

	req := httptest.NewRequest(http.MethodPost, url, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func TestHandler_Dashboard_RendersHTML(t *testing.T) {
	server, repo, _, _ := setupTestServer(t)

	// Seed one job
	_ = repo.CreateJob(context.Background(), &domain.Job{
		ID:               uuid.New().String(),
		MediaType:        "image/png",
		OriginalFilename: "dashboard_img.png",
		FilePath:         "uploads/dashboard_img.png",
		FileSize:         1024,
		Status:           domain.StatusPending,
		CreatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Media Engine Microservices") {
		t.Errorf("expected HTML body to contain header title, got: %s", body)
	}
	if !strings.Contains(body, "dashboard_img.png") {
		t.Errorf("expected HTML body to contain job filename, got: %s", body)
	}
}

func TestHandler_Upload_HTMX_ReturnsJobCardFragment(t *testing.T) {
	server, _, store, _ := setupTestServer(t)

	fileContent := []byte("image-data-sample")
	req := createMultipartRequest(t, "/upload", "file", "photo.png", "image/png", fileContent)
	req.Header.Set("HX-Request", "true")

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK && rec.Code != http.StatusAccepted {
		t.Fatalf("expected 200 or 202, got %d: %s", rec.Code, rec.Body.String())
	}

	body := rec.Body.String()
	if !strings.Contains(body, "photo.png") {
		t.Errorf("expected response to contain photo.png, got: %s", body)
	}
	if !strings.Contains(body, "sse-connect") {
		t.Errorf("expected HTMX response to include sse-connect attribute, got: %s", body)
	}

	if len(store.files) != 1 {
		t.Errorf("expected 1 file in store, got %d", len(store.files))
	}
}

func TestHandler_Upload_JSON_ReturnsJobJSON(t *testing.T) {
	server, _, _, _ := setupTestServer(t)

	fileContent := []byte("pdf-data-sample")
	req := createMultipartRequest(t, "/api/v1/jobs/upload", "file", "document.pdf", "application/pdf", fileContent)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted && rec.Code != http.StatusOK {
		t.Fatalf("expected 202 Accepted, got %d", rec.Code)
	}

	var res domain.Job
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("expected valid JSON response: %v", err)
	}

	if res.OriginalFilename != "document.pdf" {
		t.Errorf("expected document.pdf, got %s", res.OriginalFilename)
	}
	if res.Status != domain.StatusPending {
		t.Errorf("expected pending status, got %s", res.Status)
	}
}

func TestHandler_Upload_UnsupportedMediaType_Returns400(t *testing.T) {
	server, _, _, _ := setupTestServer(t)

	fileContent := []byte("mp3-audio-data")
	req := createMultipartRequest(t, "/upload", "file", "song.mp3", "audio/mp3", fileContent)

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for unsupported media type, got %d", rec.Code)
	}
}

func TestHandler_Upload_EmptyFile_Returns400(t *testing.T) {
	server, _, _, _ := setupTestServer(t)

	req := createMultipartRequest(t, "/upload", "file", "empty.png", "image/png", []byte{})

	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for empty file, got %d", rec.Code)
	}
}

func TestHandler_SSE_TerminalJob_EmitsSingleEventAndCloses(t *testing.T) {
	server, repo, _, _ := setupTestServer(t)
	ctx := context.Background()

	jobID := uuid.New().String()
	job := &domain.Job{
		ID:               jobID,
		MediaType:        "image/png",
		OriginalFilename: "completed_pic.png",
		FilePath:         "uploads/completed_pic.png",
		FileSize:         1024,
		Status:           domain.StatusCompleted,
		CreatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
	}
	_ = repo.CreateJob(ctx, job)
	_ = repo.AddArtifact(ctx, domain.Artifact{
		ID:           uuid.New().String(),
		JobID:        jobID,
		ArtifactType: "thumbnail",
		FilePath:     "artifacts/thumb.webp",
		FileSize:     256,
	})

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/jobs/%s/events", jobID), nil)
	rec := httptest.NewRecorder()

	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}

	headers := rec.Header()
	if !strings.Contains(headers.Get("Content-Type"), "text/event-stream") {
		t.Errorf("expected text/event-stream content type, got: %s", headers.Get("Content-Type"))
	}

	body := rec.Body.String()
	if !strings.Contains(body, "event: job-update") {
		t.Errorf("expected event: job-update in SSE stream, got: %s", body)
	}
	if !strings.Contains(body, "completed_pic.png") {
		t.Errorf("expected body to contain job details, got: %s", body)
	}
}

func TestHandler_SSE_LiveProgress_StreamsEventsUntilCompleted(t *testing.T) {
	server, repo, _, bus := setupTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	jobID := uuid.New().String()
	job := &domain.Job{
		ID:               jobID,
		MediaType:        "image/png",
		OriginalFilename: "live_stream.png",
		FilePath:         "uploads/live_stream.png",
		FileSize:         1024,
		Status:           domain.StatusPending,
		CreatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
	}
	_ = repo.CreateJob(ctx, job)

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/jobs/%s/events", jobID), nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		server.ServeHTTP(rec, req)
		close(done)
	}()

	// Wait briefly for subscription setup
	time.Sleep(30 * time.Millisecond)

	// Simulate Worker transition to Processing
	_ = repo.UpdateJobStatus(ctx, jobID, domain.StatusProcessing, nil)
	_ = bus.Publish(ctx, fmt.Sprintf("jobs.status.%s", jobID), ports.Event{
		EventID:   "evt-1",
		JobID:     jobID,
		EventType: "processing",
		Timestamp: time.Now().UTC(),
	})

	time.Sleep(20 * time.Millisecond)

	// Simulate Worker transition to Completed + Artifact
	_ = repo.AddArtifact(ctx, domain.Artifact{
		ID:           uuid.New().String(),
		JobID:        jobID,
		ArtifactType: "thumbnail",
		FilePath:     "artifacts/thumb.webp",
		FileSize:     128,
	})
	_ = repo.UpdateJobStatus(ctx, jobID, domain.StatusCompleted, nil)
	_ = bus.Publish(ctx, fmt.Sprintf("jobs.status.%s", jobID), ports.Event{
		EventID:   "evt-2",
		JobID:     jobID,
		EventType: "completed",
		Timestamp: time.Now().UTC(),
	})

	select {
	case <-done:
		// Completed cleanly
	case <-time.After(1 * time.Second):
		t.Fatal("SSE handler did not close connection after reaching terminal completed state")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "Processing") && !strings.Contains(body, "Queued") {
		t.Errorf("expected initial/processing state in stream")
	}
	if !strings.Contains(body, "Completed") {
		t.Errorf("expected completed final state in stream, got: %s", body)
	}
}

func TestHandler_DownloadArtifact_SuccessAndNotFound(t *testing.T) {
	server, repo, store, _ := setupTestServer(t)
	ctx := context.Background()

	jobID := uuid.New().String()
	artID := uuid.New().String()
	artPath := "artifacts/preview.webp"
	artBytes := []byte("webp-image-bytes")

	_, _ = store.Save(ctx, artPath, bytes.NewReader(artBytes))
	_ = repo.AddArtifact(ctx, domain.Artifact{
		ID:           artID,
		JobID:        jobID,
		ArtifactType: "thumbnail",
		FilePath:     artPath,
		FileSize:     int64(len(artBytes)),
	})

	// 1. Existing Artifact
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/artifacts/%s/download", artID), nil)
	rec := httptest.NewRecorder()
	server.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	if rec.Body.String() != string(artBytes) {
		t.Errorf("expected downloaded bytes to match, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Header().Get("Content-Disposition"), "attachment") {
		t.Errorf("expected Content-Disposition attachment header")
	}

	// 2. Non-existent Artifact
	reqNotFound := httptest.NewRequest(http.MethodGet, "/artifacts/missing-art/download", nil)
	recNotFound := httptest.NewRecorder()
	server.ServeHTTP(recNotFound, reqNotFound)

	if recNotFound.Code != http.StatusNotFound {
		t.Errorf("expected 404 for missing artifact, got %d", recNotFound.Code)
	}
}
