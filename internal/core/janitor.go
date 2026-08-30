package core

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
)

// JanitorService handles scheduled cleanup of expired media jobs and associated ephemeral files.
type JanitorService struct {
	repo   ports.JobRepository
	store  ports.FileStore
	cancel context.CancelFunc
	wg     sync.WaitGroup
	mu     sync.Mutex
}

// NewJanitorService creates a new JanitorService.
func NewJanitorService(repo ports.JobRepository, store ports.FileStore) *JanitorService {
	return &JanitorService{
		repo:  repo,
		store: store,
	}
}

// PruneExpired identifies and deletes jobs whose expiration time has passed, along with their physical files.
func (j *JanitorService) PruneExpired(ctx context.Context, limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}

	now := time.Now().UTC()
	expiredJobs, err := j.repo.ListExpiredJobs(ctx, now, limit)
	if err != nil {
		return 0, fmt.Errorf("janitor: failed to query expired jobs: %w", err)
	}

	prunedCount := 0
	for _, job := range expiredJobs {
		// 1. Delete source upload file
		if job.FilePath != "" {
			if err := j.store.Delete(ctx, job.FilePath); err != nil {
				log.Printf("janitor [warning]: failed to delete raw file %s for job %s: %v", job.FilePath, job.ID, err)
			}
		}

		// 2. Delete generated artifact files
		for _, art := range job.Artifacts {
			if art.FilePath != "" {
				if err := j.store.Delete(ctx, art.FilePath); err != nil {
					log.Printf("janitor [warning]: failed to delete artifact file %s for job %s: %v", art.FilePath, job.ID, err)
				}
			}
		}

		// 3. Delete job record from repository (cascades to job_artifacts table)
		if err := j.repo.DeleteJob(ctx, job.ID); err != nil {
			log.Printf("janitor [error]: failed to delete expired job %s from repository: %v", job.ID, err)
			continue
		}

		prunedCount++
	}

	return prunedCount, nil
}

// Start launches a background goroutine that periodically calls PruneExpired at the given interval.
func (j *JanitorService) Start(ctx context.Context, interval time.Duration) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if interval <= 0 {
		interval = 5 * time.Minute
	}

	runCtx, cancel := context.WithCancel(ctx)
	j.cancel = cancel
	j.wg.Add(1)

	go func() {
		defer j.wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				count, err := j.PruneExpired(runCtx, 100)
				if err != nil {
					log.Printf("janitor: error during periodic prune: %v", err)
				} else if count > 0 {
					log.Printf("janitor: pruned %d expired jobs", count)
				}
			}
		}
	}()
}

// Stop gracefully stops the background ticker loop and waits for running iterations to complete.
func (j *JanitorService) Stop() {
	j.mu.Lock()
	if j.cancel != nil {
		j.cancel()
		j.cancel = nil
	}
	j.mu.Unlock()
	j.wg.Wait()
}
