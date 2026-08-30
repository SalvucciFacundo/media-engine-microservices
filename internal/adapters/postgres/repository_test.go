package postgres_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/pashagolub/pgxmock/v4"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/postgres"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
)

func TestJobRepository_CreateJob(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgxmock: %v", err)
	}
	defer mock.Close()

	repo := postgres.NewJobRepositoryWithDB(mock)
	ctx := context.Background()

	job, _ := domain.NewJob("image/png", "photo.png", "/tmp/photo.png", 2048, 10*time.Minute)

	mock.ExpectExec("INSERT INTO jobs").
		WithArgs(
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
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.CreateJob(ctx, job)
	if err != nil {
		t.Fatalf("unexpected error creating job: %v", err)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Errorf("unfulfilled expectations: %v", err)
	}
}

func TestJobRepository_GetJobByID(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgxmock: %v", err)
	}
	defer mock.Close()

	repo := postgres.NewJobRepositoryWithDB(mock)
	ctx := context.Background()

	jobID := "job-123"
	now := time.Now().UTC()

	t.Run("found with artifacts", func(t *testing.T) {
		jobRows := pgxmock.NewRows([]string{
			"id", "media_type", "original_filename", "file_path", "file_size",
			"status", "error_message", "created_at", "updated_at", "expires_at",
		}).AddRow(
			jobID, "image/png", "sample.png", "/tmp/sample.png", int64(1000),
			"completed", nil, now, now, now.Add(time.Hour),
		)

		mock.ExpectQuery("SELECT (.+) FROM jobs WHERE id = \\$1").
			WithArgs(jobID).
			WillReturnRows(jobRows)

		metaBytes, _ := json.Marshal(map[string]any{"width": 800, "height": 600})
		artifactRows := pgxmock.NewRows([]string{
			"id", "job_id", "artifact_type", "file_path", "file_size", "metadata", "created_at",
		}).AddRow(
			"art-1", jobID, "thumbnail", "/tmp/thumb.webp", int64(250), metaBytes, now,
		)

		mock.ExpectQuery("SELECT (.+) FROM job_artifacts WHERE job_id = \\$1").
			WithArgs(jobID).
			WillReturnRows(artifactRows)

		job, err := repo.GetJobByID(ctx, jobID)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if job.ID != jobID {
			t.Errorf("expected ID %s, got %s", jobID, job.ID)
		}
		if len(job.Artifacts) != 1 {
			t.Fatalf("expected 1 artifact, got %d", len(job.Artifacts))
		}
		if job.Artifacts[0].ArtifactType != "thumbnail" {
			t.Errorf("expected artifact type thumbnail, got %s", job.Artifacts[0].ArtifactType)
		}
	})

	t.Run("not found", func(t *testing.T) {
		mock.ExpectQuery("SELECT (.+) FROM jobs WHERE id = \\$1").
			WithArgs("non-existent").
			WillReturnRows(pgxmock.NewRows([]string{"id"}))

		_, err := repo.GetJobByID(ctx, "non-existent")
		if err == nil || err != domain.ErrJobNotFound {
			t.Fatalf("expected ErrJobNotFound, got %v", err)
		}
	})
}

func TestJobRepository_UpdateJobStatus(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgxmock: %v", err)
	}
	defer mock.Close()

	repo := postgres.NewJobRepositoryWithDB(mock)
	ctx := context.Background()
	jobID := "job-1"

	t.Run("valid transition pending to processing", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT status FROM jobs WHERE id = \\$1 FOR UPDATE").
			WithArgs(jobID).
			WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow("pending"))

		mock.ExpectExec("UPDATE jobs SET status = \\$1, error_message = \\$2, updated_at = \\$3 WHERE id = \\$4").
			WithArgs(string(domain.StatusProcessing), (*string)(nil), pgxmock.AnyArg(), jobID).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))
		mock.ExpectCommit()

		err := repo.UpdateJobStatus(ctx, jobID, domain.StatusProcessing, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid transition completed to processing returns ErrInvalidStateTransition", func(t *testing.T) {
		mock.ExpectBegin()
		mock.ExpectQuery("SELECT status FROM jobs WHERE id = \\$1 FOR UPDATE").
			WithArgs(jobID).
			WillReturnRows(pgxmock.NewRows([]string{"status"}).AddRow("completed"))
		mock.ExpectRollback()

		err := repo.UpdateJobStatus(ctx, jobID, domain.StatusProcessing, nil)
		if err != domain.ErrInvalidStateTransition {
			t.Fatalf("expected ErrInvalidStateTransition, got %v", err)
		}
	})
}

func TestJobRepository_AddArtifact(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgxmock: %v", err)
	}
	defer mock.Close()

	repo := postgres.NewJobRepositoryWithDB(mock)
	ctx := context.Background()

	art, _ := domain.NewArtifact("job-123", "thumbnail", "/tmp/t.webp", 300, map[string]any{"page": 1})
	metaJSON, _ := json.Marshal(art.Metadata)

	mock.ExpectExec("INSERT INTO job_artifacts").
		WithArgs(art.ID, art.JobID, art.ArtifactType, art.FilePath, art.FileSize, metaJSON, art.CreatedAt).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = repo.AddArtifact(ctx, art)
	if err != nil {
		t.Fatalf("unexpected error adding artifact: %v", err)
	}
}

func TestJobRepository_ListExpiredJobs(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgxmock: %v", err)
	}
	defer mock.Close()

	repo := postgres.NewJobRepositoryWithDB(mock)
	ctx := context.Background()

	now := time.Now().UTC()

	rows := pgxmock.NewRows([]string{
		"id", "media_type", "original_filename", "file_path", "file_size",
		"status", "error_message", "created_at", "updated_at", "expires_at",
	}).AddRow("job-1", "image/png", "a.png", "/tmp/a.png", int64(100), "completed", nil, now, now, now.Add(-time.Minute))

	mock.ExpectQuery("SELECT (.+) FROM jobs WHERE expires_at <= \\$1 ORDER BY expires_at ASC LIMIT \\$2").
		WithArgs(now, 10).
		WillReturnRows(rows)

	mock.ExpectQuery("SELECT (.+) FROM job_artifacts WHERE job_id = \\$1").
		WithArgs("job-1").
		WillReturnRows(pgxmock.NewRows([]string{"id", "job_id", "artifact_type", "file_path", "file_size", "metadata", "created_at"}))

	jobs, err := repo.ListExpiredJobs(ctx, now, 10)
	if err != nil {
		t.Fatalf("unexpected error listing expired jobs: %v", err)
	}

	if len(jobs) != 1 {
		t.Fatalf("expected 1 expired job, got %d", len(jobs))
	}
}

func TestJobRepository_DeleteJob(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("failed to create pgxmock: %v", err)
	}
	defer mock.Close()

	repo := postgres.NewJobRepositoryWithDB(mock)
	ctx := context.Background()

	mock.ExpectExec("DELETE FROM jobs WHERE id = \\$1").
		WithArgs("job-123").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err = repo.DeleteJob(ctx, "job-123")
	if err != nil {
		t.Fatalf("unexpected error deleting job: %v", err)
	}
}
