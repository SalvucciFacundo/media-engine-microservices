package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
)

// DBPool abstracts the database connection pool interface for pgxpool.Pool and pgxmock.
type DBPool interface {
	Exec(ctx context.Context, sql string, arguments ...any) (commandTag pgconn.CommandTag, err error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
	Close()
	Ping(ctx context.Context) error
}

// JobRepository implements ports.JobRepository using PostgreSQL.
type JobRepository struct {
	db DBPool
}

// Compile-time check that JobRepository implements ports.JobRepository.
var _ ports.JobRepository = (*JobRepository)(nil)

// NewJobRepository creates a new JobRepository using pgxpool.
func NewJobRepository(ctx context.Context, connString string) (*JobRepository, error) {
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		return nil, fmt.Errorf("failed to parse postgres config: %w", err)
	}

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to postgres: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to ping postgres: %w", err)
	}

	return &JobRepository{db: pool}, nil
}

// NewJobRepositoryWithDB creates a repository with an existing DBPool (useful for testing and dependency injection).
func NewJobRepositoryWithDB(db DBPool) *JobRepository {
	return &JobRepository{db: db}
}

// Close closes the underlying connection pool.
func (r *JobRepository) Close() {
	if r.db != nil {
		r.db.Close()
	}
}

// CreateJob persists a new job entity.
func (r *JobRepository) CreateJob(ctx context.Context, job *domain.Job) error {
	query := `
		INSERT INTO jobs (
			id, media_type, original_filename, file_path, file_size,
			status, error_message, created_at, updated_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`
	_, err := r.db.Exec(ctx, query,
		job.ID,
		job.MediaType,
		job.OriginalFilename,
		job.FilePath,
		job.FileSize,
		string(job.Status),
		job.ErrorMessage,
		job.CreatedAt,
		job.UpdatedAt,
		job.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert job: %w", err)
	}
	return nil
}

// GetJobByID retrieves a job by its unique identifier along with its artifacts.
func (r *JobRepository) GetJobByID(ctx context.Context, id string) (*domain.Job, error) {
	query := `
		SELECT id, media_type, original_filename, file_path, file_size,
		       status, error_message, created_at, updated_at, expires_at
		FROM jobs
		WHERE id = $1
	`
	var job domain.Job
	var statusStr string
	err := r.db.QueryRow(ctx, query, id).Scan(
		&job.ID,
		&job.MediaType,
		&job.OriginalFilename,
		&job.FilePath,
		&job.FileSize,
		&statusStr,
		&job.ErrorMessage,
		&job.CreatedAt,
		&job.UpdatedAt,
		&job.ExpiresAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrJobNotFound
		}
		return nil, fmt.Errorf("failed to query job %s: %w", id, err)
	}
	job.Status = domain.JobStatus(statusStr)

	// Fetch artifacts
	artifacts, err := r.fetchArtifactsByJobID(ctx, id)
	if err != nil {
		return nil, err
	}
	job.Artifacts = artifacts

	return &job, nil
}

func (r *JobRepository) fetchArtifactsByJobID(ctx context.Context, jobID string) ([]domain.Artifact, error) {
	query := `
		SELECT id, job_id, artifact_type, file_path, file_size, metadata, created_at
		FROM job_artifacts
		WHERE job_id = $1
		ORDER BY created_at ASC
	`
	rows, err := r.db.Query(ctx, query, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to query artifacts for job %s: %w", jobID, err)
	}
	defer rows.Close()

	artifacts := make([]domain.Artifact, 0)
	for rows.Next() {
		var art domain.Artifact
		var metaRaw []byte
		err := rows.Scan(
			&art.ID,
			&art.JobID,
			&art.ArtifactType,
			&art.FilePath,
			&art.FileSize,
			&metaRaw,
			&art.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan artifact: %w", err)
		}
		if len(metaRaw) > 0 {
			var meta map[string]any
			if err := json.Unmarshal(metaRaw, &meta); err == nil {
				art.Metadata = meta
			}
		}
		if art.Metadata == nil {
			art.Metadata = make(map[string]any)
		}
		artifacts = append(artifacts, art)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("artifact rows iteration error: %w", err)
	}

	return artifacts, nil
}

// UpdateJobStatus updates the status and optional error message of a job with lifecycle invariant checks.
func (r *JobRepository) UpdateJobStatus(ctx context.Context, id string, nextStatus domain.JobStatus, errorMsg *string) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	var currentStatusStr string
	err = tx.QueryRow(ctx, "SELECT status FROM jobs WHERE id = $1 FOR UPDATE", id).Scan(&currentStatusStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrJobNotFound
		}
		return fmt.Errorf("failed to lock job %s: %w", id, err)
	}

	currentStatus := domain.JobStatus(currentStatusStr)
	if !currentStatus.CanTransitionTo(nextStatus) {
		return domain.ErrInvalidStateTransition
	}

	now := time.Now().UTC()
	updateQuery := `
		UPDATE jobs
		SET status = $1, error_message = $2, updated_at = $3
		WHERE id = $4
	`
	_, err = tx.Exec(ctx, updateQuery, string(nextStatus), errorMsg, now, id)
	if err != nil {
		return fmt.Errorf("failed to update job status: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit job status update: %w", err)
	}

	return nil
}

// AddArtifact persists an artifact associated with an existing job.
func (r *JobRepository) AddArtifact(ctx context.Context, artifact domain.Artifact) error {
	metaJSON, err := json.Marshal(artifact.Metadata)
	if err != nil {
		return fmt.Errorf("failed to marshal artifact metadata: %w", err)
	}

	query := `
		INSERT INTO job_artifacts (
			id, job_id, artifact_type, file_path, file_size, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, err = r.db.Exec(ctx, query,
		artifact.ID,
		artifact.JobID,
		artifact.ArtifactType,
		artifact.FilePath,
		artifact.FileSize,
		metaJSON,
		artifact.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("failed to insert artifact: %w", err)
	}
	return nil
}

// ListExpiredJobs returns jobs whose expires_at is before or equal to the given time, bounded by limit.
func (r *JobRepository) ListExpiredJobs(ctx context.Context, now time.Time, limit int) ([]*domain.Job, error) {
	if limit <= 0 {
		limit = 100
	}

	query := `
		SELECT id, media_type, original_filename, file_path, file_size,
		       status, error_message, created_at, updated_at, expires_at
		FROM jobs
		WHERE expires_at <= $1
		ORDER BY expires_at ASC
		LIMIT $2
	`
	rows, err := r.db.Query(ctx, query, now, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to query expired jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]*domain.Job, 0)
	for rows.Next() {
		var job domain.Job
		var statusStr string
		err := rows.Scan(
			&job.ID,
			&job.MediaType,
			&job.OriginalFilename,
			&job.FilePath,
			&job.FileSize,
			&statusStr,
			&job.ErrorMessage,
			&job.CreatedAt,
			&job.UpdatedAt,
			&job.ExpiresAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan expired job: %w", err)
		}
		job.Status = domain.JobStatus(statusStr)
		jobs = append(jobs, &job)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("expired jobs rows iteration error: %w", err)
	}

	// For each expired job, load its artifacts for cleanup
	for _, job := range jobs {
		artifacts, err := r.fetchArtifactsByJobID(ctx, job.ID)
		if err != nil {
			return nil, err
		}
		job.Artifacts = artifacts
	}

	return jobs, nil
}

// DeleteJob removes a job (and cascading job_artifacts).
func (r *JobRepository) DeleteJob(ctx context.Context, id string) error {
	query := `DELETE FROM jobs WHERE id = $1`
	_, err := r.db.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete job %s: %w", id, err)
	}
	return nil
}
