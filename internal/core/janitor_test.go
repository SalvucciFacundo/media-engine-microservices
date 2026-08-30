package core_test

import (
	"bytes"
	"context"
	"sync"
	"testing"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/core"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
	"github.com/google/uuid"
)

// In-memory mock JobRepo with expired jobs support
type mockJanitorRepo struct {
	mu           sync.Mutex
	jobs         map[string]*domain.Job
	artifacts    map[string][]domain.Artifact
	deletedJobs  []string
	deleteErrMap map[string]error
}

func newMockJanitorRepo() *mockJanitorRepo {
	return &mockJanitorRepo{
		jobs:         make(map[string]*domain.Job),
		artifacts:    make(map[string][]domain.Artifact),
		deleteErrMap: make(map[string]error),
	}
}

func (m *mockJanitorRepo) CreateJob(ctx context.Context, job *domain.Job) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	jCopy := *job
	m.jobs[job.ID] = &jCopy
	return nil
}

func (m *mockJanitorRepo) GetJobByID(ctx context.Context, id string) (*domain.Job, error) {
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

func (m *mockJanitorRepo) UpdateJobStatus(ctx context.Context, id string, status domain.JobStatus, errMsg *string) error {
	return nil
}

func (m *mockJanitorRepo) AddArtifact(ctx context.Context, artifact domain.Artifact) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.artifacts[artifact.JobID] = append(m.artifacts[artifact.JobID], artifact)
	return nil
}

func (m *mockJanitorRepo) ListExpiredJobs(ctx context.Context, now time.Time, limit int) ([]*domain.Job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var expired []*domain.Job
	for _, j := range m.jobs {
		if j.IsExpired(now) {
			jCopy := *j
			jCopy.Artifacts = append([]domain.Artifact(nil), m.artifacts[j.ID]...)
			expired = append(expired, &jCopy)
			if limit > 0 && len(expired) >= limit {
				break
			}
		}
	}
	return expired, nil
}

func (m *mockJanitorRepo) DeleteJob(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.deleteErrMap[id]; ok && err != nil {
		return err
	}
	delete(m.jobs, id)
	delete(m.artifacts, id)
	m.deletedJobs = append(m.deletedJobs, id)
	return nil
}

func TestJanitorService_PruneExpired_Success(t *testing.T) {
	repo := newMockJanitorRepo()
	store := newMockFileStore()
	ctx := context.Background()

	now := time.Now().UTC()

	// 1. Expired job with artifacts
	job1ID := uuid.New().String()
	file1Path := "uploads/job1.png"
	art1Path := "artifacts/job1_thumb.webp"
	_, _ = store.Save(ctx, file1Path, bytes.NewReader([]byte("job1-content")))
	_, _ = store.Save(ctx, art1Path, bytes.NewReader([]byte("job1-thumb")))

	job1 := &domain.Job{
		ID:               job1ID,
		MediaType:        "image/png",
		OriginalFilename: "job1.png",
		FilePath:         file1Path,
		FileSize:         100,
		Status:           domain.StatusCompleted,
		CreatedAt:        now.Add(-2 * time.Hour),
		UpdatedAt:        now.Add(-2 * time.Hour),
		ExpiresAt:        now.Add(-1 * time.Hour), // expired
	}
	_ = repo.CreateJob(ctx, job1)
	_ = repo.AddArtifact(ctx, domain.Artifact{
		ID:           uuid.New().String(),
		JobID:        job1ID,
		ArtifactType: "thumbnail",
		FilePath:     art1Path,
		FileSize:     50,
	})

	// 2. Non-expired job
	job2ID := uuid.New().String()
	file2Path := "uploads/job2.png"
	_, _ = store.Save(ctx, file2Path, bytes.NewReader([]byte("job2-content")))

	job2 := &domain.Job{
		ID:               job2ID,
		MediaType:        "image/png",
		OriginalFilename: "job2.png",
		FilePath:         file2Path,
		FileSize:         100,
		Status:           domain.StatusPending,
		CreatedAt:        now,
		UpdatedAt:        now,
		ExpiresAt:        now.Add(1 * time.Hour), // NOT expired
	}
	_ = repo.CreateJob(ctx, job2)

	janitor := core.NewJanitorService(repo, store)
	prunedCount, err := janitor.PruneExpired(ctx, 100)
	if err != nil {
		t.Fatalf("unexpected error during prune: %v", err)
	}

	if prunedCount != 1 {
		t.Errorf("expected 1 pruned job, got %d", prunedCount)
	}

	// Verify job 1 physical files are deleted
	exists1, _ := store.Exists(ctx, file1Path)
	if exists1 {
		t.Errorf("expected original file %s to be deleted from store", file1Path)
	}
	existsArt1, _ := store.Exists(ctx, art1Path)
	if existsArt1 {
		t.Errorf("expected artifact file %s to be deleted from store", art1Path)
	}

	// Verify job 1 record is deleted from DB
	_, err = repo.GetJobByID(ctx, job1ID)
	if err != domain.ErrJobNotFound {
		t.Errorf("expected job1 to be deleted from repo, got err: %v", err)
	}

	// Verify job 2 is untouched
	exists2, _ := store.Exists(ctx, file2Path)
	if !exists2 {
		t.Errorf("expected non-expired file %s to still exist in store", file2Path)
	}
	j2, err := repo.GetJobByID(ctx, job2ID)
	if err != nil || j2 == nil {
		t.Errorf("expected non-expired job2 to still exist in repo")
	}
}

func TestJanitorService_PruneExpired_MissingPhysicalFile_ContinuesGracefully(t *testing.T) {
	repo := newMockJanitorRepo()
	store := newMockFileStore()
	ctx := context.Background()

	now := time.Now().UTC()

	// Expired job whose file is already missing on disk
	jobID := uuid.New().String()
	missingFilePath := "uploads/missing.png"

	job := &domain.Job{
		ID:               jobID,
		MediaType:        "image/png",
		OriginalFilename: "missing.png",
		FilePath:         missingFilePath,
		FileSize:         100,
		Status:           domain.StatusCompleted,
		CreatedAt:        now.Add(-2 * time.Hour),
		UpdatedAt:        now.Add(-2 * time.Hour),
		ExpiresAt:        now.Add(-1 * time.Hour),
	}
	_ = repo.CreateJob(ctx, job)

	janitor := core.NewJanitorService(repo, store)
	prunedCount, err := janitor.PruneExpired(ctx, 10)
	if err != nil {
		t.Fatalf("expected prune to succeed even if physical file missing, got error: %v", err)
	}
	if prunedCount != 1 {
		t.Errorf("expected 1 pruned job, got %d", prunedCount)
	}

	_, err = repo.GetJobByID(ctx, jobID)
	if err != domain.ErrJobNotFound {
		t.Errorf("expected job to be deleted from repo, got err: %v", err)
	}
}

func TestJanitorService_BackgroundTicker_StartsAndStopsCleanly(t *testing.T) {
	repo := newMockJanitorRepo()
	store := newMockFileStore()
	ctx, cancel := context.WithCancel(context.Background())

	now := time.Now().UTC()
	jobID := uuid.New().String()
	filePath := "uploads/ticker_job.png"
	_, _ = store.Save(ctx, filePath, bytes.NewReader([]byte("content")))

	_ = repo.CreateJob(ctx, &domain.Job{
		ID:               jobID,
		MediaType:        "image/png",
		OriginalFilename: "ticker.png",
		FilePath:         filePath,
		FileSize:         100,
		Status:           domain.StatusCompleted,
		ExpiresAt:        now.Add(-1 * time.Minute),
	})

	janitor := core.NewJanitorService(repo, store)

	// Run with very fast tick for testing
	janitor.Start(ctx, 20*time.Millisecond)

	// Wait for tick to execute
	time.Sleep(70 * time.Millisecond)
	cancel()
	janitor.Stop()

	// Verify job was pruned by background ticker
	_, err := repo.GetJobByID(context.Background(), jobID)
	if err != domain.ErrJobNotFound {
		t.Errorf("expected job to be pruned by background ticker, got err: %v", err)
	}
}
