package core

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
	"github.com/google/uuid"
)

var supportedMediaTypes = map[string]bool{
	"image/png":       true,
	"image/jpeg":      true,
	"image/jpg":       true,
	"image/webp":      true,
	"image/gif":       true,
	"application/pdf": true,
}

// UploadService coordinates media file intake, persistence, DB registration, and NATS event publishing.
type UploadService struct {
	repo       ports.JobRepository
	store      ports.FileStore
	publisher  ports.EventPublisher
	defaultTTL time.Duration
}

// NewUploadService creates a new UploadService instance.
func NewUploadService(repo ports.JobRepository, store ports.FileStore, publisher ports.EventPublisher, defaultTTL time.Duration) *UploadService {
	if defaultTTL <= 0 {
		defaultTTL = 1 * time.Hour
	}
	return &UploadService{
		repo:       repo,
		store:      store,
		publisher:  publisher,
		defaultTTL: defaultTTL,
	}
}

// IsSupportedMediaType checks if a given MIME type is accepted by the processing engine.
func (s *UploadService) IsSupportedMediaType(mediaType string) bool {
	normalized := strings.ToLower(strings.TrimSpace(mediaType))
	return supportedMediaTypes[normalized]
}

// ProcessUpload validates the upload, writes the raw file to the FileStore, registers the pending Job in the repository,
// and publishes a jobs.created event to the event bus. Rollback mechanisms ensure no orphaned files or DB rows on failure.
func (s *UploadService) ProcessUpload(ctx context.Context, filename, mediaType string, reader io.Reader, fileSize int64, ttl time.Duration) (*domain.Job, error) {
	if fileSize <= 0 || reader == nil {
		return nil, fmt.Errorf("%w: file size must be positive", domain.ErrInvalidJob)
	}

	normMediaType := strings.ToLower(strings.TrimSpace(mediaType))
	if !s.IsSupportedMediaType(normMediaType) {
		return nil, fmt.Errorf("%w: %s", domain.ErrUnsupportedMediaType, mediaType)
	}

	if ttl <= 0 {
		ttl = s.defaultTTL
	}

	// Sanitize original filename and build unique storage path
	baseName := filepath.Base(filename)
	if baseName == "" || baseName == "." {
		baseName = "unnamed_upload"
	}
	uniqueID := uuid.New().String()
	ext := filepath.Ext(baseName)
	storedFilename := fmt.Sprintf("uploads/%s_%s%s", uniqueID, strings.TrimSuffix(baseName, ext), ext)

	// 1. Save to FileStore
	savedPath, err := s.store.Save(ctx, storedFilename, reader)
	if err != nil {
		return nil, fmt.Errorf("upload: failed to save file: %w", err)
	}

	// 2. Create Job domain entity
	job, err := domain.NewJob(normMediaType, baseName, savedPath, fileSize, ttl)
	if err != nil {
		_ = s.store.Delete(ctx, savedPath)
		return nil, fmt.Errorf("upload: failed to create job domain entity: %w", err)
	}

	// 3. Persist Job in Repository
	if err := s.repo.CreateJob(ctx, job); err != nil {
		// Rollback file
		_ = s.store.Delete(ctx, savedPath)
		return nil, fmt.Errorf("upload: failed to persist job in repository: %w", err)
	}

	// 4. Publish Event to NATS
	evt := ports.Event{
		EventID:   uuid.New().String(),
		JobID:     job.ID,
		EventType: "created",
		Timestamp: time.Now().UTC(),
		Payload: map[string]any{
			"job_id":            job.ID,
			"media_type":        job.MediaType,
			"original_filename": job.OriginalFilename,
			"file_path":         job.FilePath,
			"file_size":         job.FileSize,
		},
	}

	if err := s.publisher.Publish(ctx, "jobs.created", evt); err != nil {
		// Rollback repository record and storage file
		_ = s.repo.DeleteJob(ctx, job.ID)
		_ = s.store.Delete(ctx, savedPath)
		return nil, fmt.Errorf("upload: failed to publish job created event: %w", err)
	}

	return job, nil
}
