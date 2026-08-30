package ports

import (
	"context"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
)

// JobRepository defines the persistence operations for jobs and associated artifacts.
type JobRepository interface {
	// CreateJob persists a new job entity in the database.
	CreateJob(ctx context.Context, job *domain.Job) error

	// GetJobByID retrieves a job by its unique identifier along with its artifacts.
	GetJobByID(ctx context.Context, id string) (*domain.Job, error)

	// UpdateJobStatus updates the status and optional error message of a job with lifecycle invariant checks.
	UpdateJobStatus(ctx context.Context, id string, status domain.JobStatus, errorMsg *string) error

	// AddArtifact persists an artifact associated with an existing job.
	AddArtifact(ctx context.Context, artifact domain.Artifact) error

	// ListExpiredJobs returns jobs whose expires_at is before or equal to the given time, bounded by limit.
	ListExpiredJobs(ctx context.Context, now time.Time, limit int) ([]*domain.Job, error)

	// DeleteJob removes a job and its associated artifacts (via cascade).
	DeleteJob(ctx context.Context, id string) error
}
