package core_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/core"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
)

// In-memory mock FileStore
type mockFileStore struct {
	mu      sync.Mutex
	files   map[string][]byte
	deleted map[string]bool
	saveErr error
}

func newMockFileStore() *mockFileStore {
	return &mockFileStore{
		files:   make(map[string][]byte),
		deleted: make(map[string]bool),
	}
}

func (m *mockFileStore) Save(ctx context.Context, relativePath string, data io.Reader) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return "", m.saveErr
	}
	buf, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}
	m.files[relativePath] = buf
	return relativePath, nil
}

func (m *mockFileStore) Open(ctx context.Context, relativeOrFullPath string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.files[relativeOrFullPath]
	if !ok || m.deleted[relativeOrFullPath] {
		return nil, errors.New("file not found")
	}
	return io.NopCloser(bytes.NewReader(b)), nil
}

func (m *mockFileStore) Delete(ctx context.Context, relativeOrFullPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted[relativeOrFullPath] = true
	delete(m.files, relativeOrFullPath)
	return nil
}

func (m *mockFileStore) Exists(ctx context.Context, relativeOrFullPath string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.files[relativeOrFullPath]
	return ok && !m.deleted[relativeOrFullPath], nil
}

func (m *mockFileStore) GetPath(relativePath string) string {
	return relativePath
}

// Failing repo mock for rollback testing
type failingRepo struct {
	*mockJobRepo
	createErr error
}

func (f *failingRepo) CreateJob(ctx context.Context, job *domain.Job) error {
	if f.createErr != nil {
		return f.createErr
	}
	return f.mockJobRepo.CreateJob(ctx, job)
}

// Failing publisher mock for rollback testing
type failingPublisher struct {
	*mockEventBus
	pubErr error
}

func (f *failingPublisher) Publish(ctx context.Context, subject string, event ports.Event) error {
	if f.pubErr != nil {
		return f.pubErr
	}
	return f.mockEventBus.Publish(ctx, subject, event)
}

func TestUploadService_ProcessUpload_Success(t *testing.T) {
	repo := newMockJobRepo()
	store := newMockFileStore()
	bus := newMockEventBus()
	ctx := context.Background()

	svc := core.NewUploadService(repo, store, bus, 1*time.Hour)

	fileData := []byte("sample-png-data")
	filename := "test_image.png"
	contentType := "image/png"

	job, err := svc.ProcessUpload(ctx, filename, contentType, bytes.NewReader(fileData), int64(len(fileData)), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if job == nil {
		t.Fatal("expected non-nil job")
	}
	if job.ID == "" {
		t.Errorf("expected non-empty job ID")
	}
	if job.Status != domain.StatusPending {
		t.Errorf("expected status pending, got %s", job.Status)
	}
	if job.OriginalFilename != filename {
		t.Errorf("expected original filename %s, got %s", filename, job.OriginalFilename)
	}
	if job.MediaType != contentType {
		t.Errorf("expected media type %s, got %s", contentType, job.MediaType)
	}
	if job.FileSize != int64(len(fileData)) {
		t.Errorf("expected file size %d, got %d", len(fileData), job.FileSize)
	}

	// Verify file is saved in FileStore
	exists, err := store.Exists(ctx, job.FilePath)
	if err != nil || !exists {
		t.Errorf("expected file %s to exist in store", job.FilePath)
	}

	// Verify job is saved in Repository
	savedJob, err := repo.GetJobByID(ctx, job.ID)
	if err != nil {
		t.Fatalf("expected job to exist in repo: %v", err)
	}
	if savedJob.ID != job.ID {
		t.Errorf("expected saved job ID %s, got %s", job.ID, savedJob.ID)
	}

	// Verify event published to NATS
	bus.mu.Lock()
	defer bus.mu.Unlock()
	if len(bus.published) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(bus.published))
	}
	if bus.published[0].JobID != job.ID {
		t.Errorf("expected event job ID %s, got %s", job.ID, bus.published[0].JobID)
	}
	if bus.publishedSub[0] != "jobs.created" {
		t.Errorf("expected subject jobs.created, got %s", bus.publishedSub[0])
	}
}

func TestUploadService_ProcessUpload_SupportedMediaTypes(t *testing.T) {
	types := []string{
		"image/png",
		"image/jpeg",
		"image/jpg",
		"image/webp",
		"image/gif",
		"application/pdf",
	}

	for _, ct := range types {
		t.Run(ct, func(t *testing.T) {
			repo := newMockJobRepo()
			store := newMockFileStore()
			bus := newMockEventBus()
			svc := core.NewUploadService(repo, store, bus, 1*time.Hour)

			data := []byte("some-file-data")
			job, err := svc.ProcessUpload(context.Background(), "sample.ext", ct, bytes.NewReader(data), int64(len(data)), 0)
			if err != nil {
				t.Fatalf("expected media type %s to be supported, got error: %v", ct, err)
			}
			if job == nil {
				t.Fatalf("expected non-nil job for %s", ct)
			}
		})
	}
}

func TestUploadService_ProcessUpload_UnsupportedMediaType(t *testing.T) {
	repo := newMockJobRepo()
	store := newMockFileStore()
	bus := newMockEventBus()
	svc := core.NewUploadService(repo, store, bus, 1*time.Hour)

	data := []byte("audio-content")
	_, err := svc.ProcessUpload(context.Background(), "music.mp3", "audio/mp3", bytes.NewReader(data), int64(len(data)), 0)
	if !errors.Is(err, domain.ErrUnsupportedMediaType) {
		t.Fatalf("expected ErrUnsupportedMediaType, got %v", err)
	}

	// Verify nothing was saved or published
	if len(store.files) != 0 {
		t.Errorf("expected no files in store, got %d", len(store.files))
	}
	if len(repo.jobs) != 0 {
		t.Errorf("expected no jobs in repo, got %d", len(repo.jobs))
	}
	if len(bus.published) != 0 {
		t.Errorf("expected no events published, got %d", len(bus.published))
	}
}

func TestUploadService_ProcessUpload_EmptyFile(t *testing.T) {
	repo := newMockJobRepo()
	store := newMockFileStore()
	bus := newMockEventBus()
	svc := core.NewUploadService(repo, store, bus, 1*time.Hour)

	_, err := svc.ProcessUpload(context.Background(), "empty.png", "image/png", strings.NewReader(""), 0, 0)
	if !errors.Is(err, domain.ErrInvalidJob) {
		t.Fatalf("expected ErrInvalidJob for empty file, got %v", err)
	}
}

func TestUploadService_ProcessUpload_RepoFailure_RollbackFile(t *testing.T) {
	repo := &failingRepo{
		mockJobRepo: newMockJobRepo(),
		createErr:   errors.New("db connection failed"),
	}
	store := newMockFileStore()
	bus := newMockEventBus()
	svc := core.NewUploadService(repo, store, bus, 1*time.Hour)

	data := []byte("image-data")
	_, err := svc.ProcessUpload(context.Background(), "test.png", "image/png", bytes.NewReader(data), int64(len(data)), 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify store rollback: no files left in store
	if len(store.files) != 0 {
		t.Errorf("expected store to rollback and have 0 files, got %d", len(store.files))
	}
}

func TestUploadService_ProcessUpload_PublisherFailure_RollbackRepoAndFile(t *testing.T) {
	repo := newMockJobRepo()
	store := newMockFileStore()
	bus := &failingPublisher{
		mockEventBus: newMockEventBus(),
		pubErr:       errors.New("nats connection lost"),
	}
	svc := core.NewUploadService(repo, store, bus, 1*time.Hour)

	data := []byte("image-data")
	_, err := svc.ProcessUpload(context.Background(), "test.png", "image/png", bytes.NewReader(data), int64(len(data)), 0)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Verify rollback
	if len(store.files) != 0 {
		t.Errorf("expected store to have 0 files after rollback, got %d", len(store.files))
	}
	if len(repo.jobs) != 0 {
		t.Errorf("expected repo to have 0 jobs after rollback, got %d", len(repo.jobs))
	}
}
