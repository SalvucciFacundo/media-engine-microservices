package core_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/localfs"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/core"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/handlers/media"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
	"github.com/google/uuid"
)

// In-memory mock JobRepository
type mockJobRepo struct {
	mu        sync.Mutex
	jobs      map[string]*domain.Job
	artifacts map[string][]domain.Artifact
}

func newMockJobRepo() *mockJobRepo {
	return &mockJobRepo{
		jobs:      make(map[string]*domain.Job),
		artifacts: make(map[string][]domain.Artifact),
	}
}

func (m *mockJobRepo) CreateJob(ctx context.Context, job *domain.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	jCopy := *job
	m.jobs[job.ID] = &jCopy
	return nil
}

func (m *mockJobRepo) GetJobByID(ctx context.Context, id string) (*domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	if !ok {
		return nil, domain.ErrJobNotFound
	}
	jCopy := *j
	jCopy.Artifacts = append([]domain.Artifact(nil), m.artifacts[id]...)
	return &jCopy, nil
}

func (m *mockJobRepo) UpdateJobStatus(ctx context.Context, id string, status domain.JobStatus, errMsg *string) error {
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
	if err := j.TransitionTo(status, errMsgs...); err != nil {
		return err
	}
	return nil
}

func (m *mockJobRepo) AddArtifact(ctx context.Context, artifact domain.Artifact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.artifacts[artifact.JobID] = append(m.artifacts[artifact.JobID], artifact)
	return nil
}

func (m *mockJobRepo) ListExpiredJobs(ctx context.Context, now time.Time, limit int) ([]*domain.Job, error) {
	return nil, nil
}

func (m *mockJobRepo) DeleteJob(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.jobs, id)
	delete(m.artifacts, id)
	return nil
}

// In-memory mock EventBus
type mockEventBus struct {
	mu           sync.Mutex
	published    []ports.Event
	publishedSub []string
	handlers     map[string]ports.EventHandler
}

func newMockEventBus() *mockEventBus {
	return &mockEventBus{
		handlers: make(map[string]ports.EventHandler),
	}
}

func (m *mockEventBus) Publish(ctx context.Context, subject string, event ports.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.published = append(m.published, event)
	m.publishedSub = append(m.publishedSub, subject)
	return nil
}

func (m *mockEventBus) Subscribe(ctx context.Context, subject string, handler ports.EventHandler) (ports.SubscriptionCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[subject] = handler
	return &mockCloser{}, nil
}

func (m *mockEventBus) SubscribeQueue(ctx context.Context, subject, queueGroup string, handler ports.EventHandler) (ports.SubscriptionCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[subject] = handler
	return &mockCloser{}, nil
}

type mockCloser struct{}

func (c *mockCloser) Unsubscribe() error { return nil }

// Mock Processor
type mockProcessor struct {
	canProcessFunc func(mediaType string) bool
	processFunc    func(ctx context.Context, job *domain.Job) ([]domain.Artifact, error)
}

func (mp *mockProcessor) CanProcess(mediaType string) bool {
	return mp.canProcessFunc(mediaType)
}

func (mp *mockProcessor) Process(ctx context.Context, job *domain.Job) ([]domain.Artifact, error) {
	return mp.processFunc(ctx, job)
}

func TestWorkerService_ProcessJob_Success(t *testing.T) {
	repo := newMockJobRepo()
	bus := newMockEventBus()
	ctx := context.Background()

	jobID := uuid.New().String()
	job := &domain.Job{
		ID:               jobID,
		MediaType:        "image/png",
		OriginalFilename: "image.png",
		FilePath:         "uploads/image.png",
		FileSize:         1024,
		Status:           domain.StatusPending,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
	}
	_ = repo.CreateJob(ctx, job)

	imageProc := &mockProcessor{
		canProcessFunc: func(mt string) bool { return mt == "image/png" },
		processFunc: func(ctx context.Context, j *domain.Job) ([]domain.Artifact, error) {
			return []domain.Artifact{
				{
					ID:           uuid.New().String(),
					JobID:        j.ID,
					ArtifactType: "thumbnail",
					FilePath:     "uploads/image_thumb.png",
					FileSize:     256,
				},
			}, nil
		},
	}

	worker := core.NewWorkerService(repo, bus, bus, []ports.MediaProcessor{imageProc}, "test-queue")
	if err := worker.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}
	defer worker.Stop()

	// Simulate NATS jobs.created event
	evt := ports.Event{
		EventID:   "evt-123",
		JobID:     jobID,
		EventType: "created",
		Timestamp: time.Now().UTC(),
		Payload: map[string]any{
			"job_id":     jobID,
			"media_type": "image/png",
		},
	}

	handler := bus.handlers["jobs.created"]
	if handler == nil {
		t.Fatal("worker did not subscribe to jobs.created")
	}

	if err := handler(ctx, evt); err != nil {
		t.Fatalf("handler returned error: %v", err)
	}

	// Verify job status updated in repository
	updatedJob, err := repo.GetJobByID(ctx, jobID)
	if err != nil {
		t.Fatalf("failed to get job: %v", err)
	}
	if updatedJob.Status != domain.StatusCompleted {
		t.Errorf("expected job status completed, got %s", updatedJob.Status)
	}
	if len(updatedJob.Artifacts) != 1 {
		t.Errorf("expected 1 artifact, got %d", len(updatedJob.Artifacts))
	}

	// Verify published events (processing, status updates, completed)
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if len(bus.published) < 2 {
		t.Errorf("expected at least 2 events published (processing, completed), got %d", len(bus.published))
	}
}

func TestWorkerService_ProcessJob_UnsupportedMediaType(t *testing.T) {
	repo := newMockJobRepo()
	bus := newMockEventBus()
	ctx := context.Background()

	jobID := uuid.New().String()
	job := &domain.Job{
		ID:               jobID,
		MediaType:        "audio/mp3",
		OriginalFilename: "audio.mp3",
		FilePath:         "uploads/audio.mp3",
		FileSize:         1024,
		Status:           domain.StatusPending,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
	}
	_ = repo.CreateJob(ctx, job)

	worker := core.NewWorkerService(repo, bus, bus, nil, "test-queue")
	if err := worker.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}
	defer worker.Stop()

	handler := bus.handlers["jobs.created"]
	evt := ports.Event{
		EventID:   "evt-unsupported",
		JobID:     jobID,
		EventType: "created",
		Timestamp: time.Now().UTC(),
	}

	err := handler(ctx, evt)
	if err == nil {
		t.Fatal("expected error for unsupported media type, got nil")
	}

	updatedJob, _ := repo.GetJobByID(ctx, jobID)
	if updatedJob.Status != domain.StatusFailed {
		t.Errorf("expected job status failed, got %s", updatedJob.Status)
	}
	if updatedJob.ErrorMessage == nil || *updatedJob.ErrorMessage == "" {
		t.Errorf("expected non-empty error message on job")
	}
}

func TestWorkerService_ProcessJob_ProcessorFailure(t *testing.T) {
	repo := newMockJobRepo()
	bus := newMockEventBus()
	ctx := context.Background()

	jobID := uuid.New().String()
	job := &domain.Job{
		ID:               jobID,
		MediaType:        "image/png",
		OriginalFilename: "bad_image.png",
		FilePath:         "uploads/bad_image.png",
		FileSize:         1024,
		Status:           domain.StatusPending,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
	}
	_ = repo.CreateJob(ctx, job)

	failingProc := &mockProcessor{
		canProcessFunc: func(mt string) bool { return true },
		processFunc: func(ctx context.Context, j *domain.Job) ([]domain.Artifact, error) {
			return nil, errors.New("corrupt image data")
		},
	}

	worker := core.NewWorkerService(repo, bus, bus, []ports.MediaProcessor{failingProc}, "test-queue")
	if err := worker.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}
	defer worker.Stop()

	handler := bus.handlers["jobs.created"]
	evt := ports.Event{
		EventID:   "evt-fail",
		JobID:     jobID,
		EventType: "created",
		Timestamp: time.Now().UTC(),
	}

	err := handler(ctx, evt)
	if err == nil {
		t.Fatal("expected error from failing processor, got nil")
	}

	updatedJob, _ := repo.GetJobByID(ctx, jobID)
	if updatedJob.Status != domain.StatusFailed {
		t.Errorf("expected status failed, got %s", updatedJob.Status)
	}
}

func TestWorkerPipeline_EndToEndIntegration(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}

	repo := newMockJobRepo()
	bus := newMockEventBus()
	ctx := context.Background()

	imageProc := media.NewImageProcessor(store)
	pdfProc := media.NewPDFProcessor(store)

	worker := core.NewWorkerService(repo, bus, bus, []ports.MediaProcessor{imageProc, pdfProc}, "test-workers")
	if err := worker.Start(ctx); err != nil {
		t.Fatalf("failed to start worker: %v", err)
	}
	defer worker.Stop()

	// 1. Process Image Job
	imgBytes := createSamplePNG(t, 640, 480)
	imgFilename := "upload_img.png"
	if _, err := store.Save(ctx, imgFilename, bytes.NewReader(imgBytes)); err != nil {
		t.Fatalf("failed to save img: %v", err)
	}

	imageJobID := uuid.New().String()
	imgJob := &domain.Job{
		ID:               imageJobID,
		MediaType:        "image/png",
		OriginalFilename: "photo.png",
		FilePath:         imgFilename,
		FileSize:         int64(len(imgBytes)),
		Status:           domain.StatusPending,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
	}
	_ = repo.CreateJob(ctx, imgJob)

	handler := bus.handlers["jobs.created"]
	if err := handler(ctx, ports.Event{JobID: imageJobID, EventType: "created"}); err != nil {
		t.Fatalf("failed to handle image job: %v", err)
	}

	resJob, _ := repo.GetJobByID(ctx, imageJobID)
	if resJob.Status != domain.StatusCompleted {
		t.Errorf("expected completed image job status, got %s", resJob.Status)
	}
	if len(resJob.Artifacts) < 2 {
		t.Errorf("expected at least 2 image artifacts, got %d", len(resJob.Artifacts))
	}

	// 2. Process PDF Job
	pdfBytes := createSamplePDF(t, "Microservices Event Driven Worker")
	pdfFilename := "upload_doc.pdf"
	if _, err := store.Save(ctx, pdfFilename, bytes.NewReader(pdfBytes)); err != nil {
		t.Fatalf("failed to save pdf: %v", err)
	}

	pdfJobID := uuid.New().String()
	pdfJob := &domain.Job{
		ID:               pdfJobID,
		MediaType:        "application/pdf",
		OriginalFilename: "doc.pdf",
		FilePath:         pdfFilename,
		FileSize:         int64(len(pdfBytes)),
		Status:           domain.StatusPending,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
	}
	_ = repo.CreateJob(ctx, pdfJob)

	if err := handler(ctx, ports.Event{JobID: pdfJobID, EventType: "created"}); err != nil {
		t.Fatalf("failed to handle pdf job: %v", err)
	}

	resPDFJob, _ := repo.GetJobByID(ctx, pdfJobID)
	if resPDFJob.Status != domain.StatusCompleted {
		t.Errorf("expected completed pdf job status, got %s", resPDFJob.Status)
	}
	if len(resPDFJob.Artifacts) < 2 {
		t.Errorf("expected at least 2 pdf artifacts, got %d", len(resPDFJob.Artifacts))
	}
}

func createSamplePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 120, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode sample png: %v", err)
	}
	return buf.Bytes()
}

func createSamplePDF(t *testing.T, text string) []byte {
	t.Helper()
	pdfContent := "%PDF-1.4\n" +
		"1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n" +
		"2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n" +
		"3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >> endobj\n" +
		"4 0 obj << /Length 50 >> stream\n" +
		"BT /F1 24 Tf 100 700 Td (" + text + ") Tj ET\n" +
		"endstream\n" +
		"endobj\n" +
		"5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj\n" +
		"xref\n" +
		"0 6\n" +
		"0000000000 65535 f \n" +
		"0000000009 00000 n \n" +
		"0000000058 00000 n \n" +
		"0000000115 00000 n \n" +
		"0000000244 00000 n \n" +
		"0000000344 00000 n \n" +
		"trailer << /Size 6 /Root 1 0 R >>\n" +
		"startxref\n" +
		"425\n" +
		"%%EOF\n"
	return []byte(pdfContent)
}
