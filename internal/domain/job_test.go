package domain_test

import (
	"testing"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
)

func TestNewJob_Validations(t *testing.T) {
	t.Run("creates valid pending job with expiration", func(t *testing.T) {
		job, err := domain.NewJob("image/png", "photo.png", "/tmp/photo.png", 1024, 15*time.Minute)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
		if job.ID == "" {
			t.Errorf("expected generated ID, got empty")
		}
		if job.Status != domain.StatusPending {
			t.Errorf("expected status %s, got %s", domain.StatusPending, job.Status)
		}
		if job.MediaType != "image/png" {
			t.Errorf("expected media type image/png, got %s", job.MediaType)
		}
		if job.OriginalFilename != "photo.png" {
			t.Errorf("expected photo.png, got %s", job.OriginalFilename)
		}
		if job.FilePath != "/tmp/photo.png" {
			t.Errorf("expected /tmp/photo.png, got %s", job.FilePath)
		}
		if job.FileSize != 1024 {
			t.Errorf("expected 1024, got %d", job.FileSize)
		}
		if job.CreatedAt.IsZero() || job.UpdatedAt.IsZero() {
			t.Errorf("expected timestamps to be set")
		}
		if !job.ExpiresAt.After(job.CreatedAt) {
			t.Errorf("expected expires_at to be after created_at")
		}
	})

	t.Run("fails when media type is empty", func(t *testing.T) {
		_, err := domain.NewJob("", "photo.png", "/tmp/photo.png", 1024, 15*time.Minute)
		if err == nil {
			t.Fatal("expected error for empty media type, got nil")
		}
	})

	t.Run("fails when file path is empty", func(t *testing.T) {
		_, err := domain.NewJob("image/png", "photo.png", "", 1024, 15*time.Minute)
		if err == nil {
			t.Fatal("expected error for empty file path, got nil")
		}
	})

	t.Run("fails when file size is zero or negative", func(t *testing.T) {
		_, err := domain.NewJob("image/png", "photo.png", "/tmp/photo.png", 0, 15*time.Minute)
		if err == nil {
			t.Fatal("expected error for zero file size, got nil")
		}
		_, err = domain.NewJob("image/png", "photo.png", "/tmp/photo.png", -10, 15*time.Minute)
		if err == nil {
			t.Fatal("expected error for negative file size, got nil")
		}
	})

	t.Run("fails when TTL is non-positive", func(t *testing.T) {
		_, err := domain.NewJob("image/png", "photo.png", "/tmp/photo.png", 1024, 0)
		if err == nil {
			t.Fatal("expected error for zero TTL, got nil")
		}
	})
}

func TestJobStatus_Transitions(t *testing.T) {
	cases := []struct {
		name      string
		from      domain.JobStatus
		to        domain.JobStatus
		errMsg    string
		wantError bool
	}{
		{"pending to processing", domain.StatusPending, domain.StatusProcessing, "", false},
		{"pending to failed", domain.StatusPending, domain.StatusFailed, "decode error", false},
		{"pending to completed (invalid)", domain.StatusPending, domain.StatusCompleted, "", true},
		{"processing to completed", domain.StatusProcessing, domain.StatusCompleted, "", false},
		{"processing to failed", domain.StatusProcessing, domain.StatusFailed, "conversion error", false},
		{"processing to pending (invalid)", domain.StatusProcessing, domain.StatusPending, "", true},
		{"completed to processing (terminal)", domain.StatusCompleted, domain.StatusProcessing, "", true},
		{"completed to failed (terminal)", domain.StatusCompleted, domain.StatusFailed, "", true},
		{"failed to processing (terminal)", domain.StatusFailed, domain.StatusProcessing, "", true},
		{"failed to completed (terminal)", domain.StatusFailed, domain.StatusCompleted, "", true},
		{"failed to pending (terminal)", domain.StatusFailed, domain.StatusPending, "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			job, err := domain.NewJob("image/png", "test.png", "/tmp/test.png", 500, time.Hour)
			if err != nil {
				t.Fatalf("failed to create job: %v", err)
			}
			job.Status = tc.from

			err = job.TransitionTo(tc.to, tc.errMsg)
			if tc.wantError && err == nil {
				t.Errorf("expected transition error from %s to %s, got nil", tc.from, tc.to)
			}
			if !tc.wantError && err != nil {
				t.Errorf("unexpected error for transition from %s to %s: %v", tc.from, tc.to, err)
			}
			if !tc.wantError {
				if job.Status != tc.to {
					t.Errorf("expected status %s, got %s", tc.to, job.Status)
				}
				if tc.to == domain.StatusFailed && (job.ErrorMessage == nil || *job.ErrorMessage != tc.errMsg) {
					t.Errorf("expected error message %q, got %v", tc.errMsg, job.ErrorMessage)
				}
			}
		})
	}
}

func TestJob_AddArtifact(t *testing.T) {
	t.Run("successfully adds artifact to job", func(t *testing.T) {
		job, err := domain.NewJob("image/png", "test.png", "/tmp/test.png", 500, time.Hour)
		if err != nil {
			t.Fatalf("failed to create job: %v", err)
		}

		art, err := domain.NewArtifact(job.ID, "thumbnail", "/tmp/thumb.webp", 250, map[string]any{"width": 200, "height": 200})
		if err != nil {
			t.Fatalf("failed to create artifact: %v", err)
		}

		err = job.AddArtifact(art)
		if err != nil {
			t.Fatalf("unexpected error adding artifact: %v", err)
		}

		if len(job.Artifacts) != 1 {
			t.Fatalf("expected 1 artifact, got %d", len(job.Artifacts))
		}
		if job.Artifacts[0].ArtifactType != "thumbnail" {
			t.Errorf("expected thumbnail, got %s", job.Artifacts[0].ArtifactType)
		}
	})

	t.Run("fails when artifact job ID does not match", func(t *testing.T) {
		job, _ := domain.NewJob("image/png", "test.png", "/tmp/test.png", 500, time.Hour)
		art, _ := domain.NewArtifact("other-job-id", "thumbnail", "/tmp/thumb.webp", 250, nil)

		err := job.AddArtifact(art)
		if err == nil {
			t.Fatal("expected error when adding artifact with mismatched JobID, got nil")
		}
	})
}

func TestJob_IsExpired(t *testing.T) {
	job, _ := domain.NewJob("image/png", "test.png", "/tmp/test.png", 500, 10*time.Minute)

	if job.IsExpired(job.CreatedAt.Add(5 * time.Minute)) {
		t.Errorf("job should not be expired after 5 minutes")
	}

	if !job.IsExpired(job.CreatedAt.Add(11 * time.Minute)) {
		t.Errorf("job should be expired after 11 minutes")
	}
}
