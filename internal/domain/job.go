package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Job represents a top-level media processing task.
type Job struct {
	ID               string     `json:"id"`
	MediaType        string     `json:"media_type"`
	OriginalFilename string     `json:"original_filename"`
	FilePath         string     `json:"file_path"`
	FileSize         int64      `json:"file_size"`
	Status           JobStatus  `json:"status"`
	ErrorMessage     *string    `json:"error_message,omitempty"`
	Artifacts        []Artifact `json:"artifacts,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
}

// NewJob validates and initializes a new Job in pending status.
func NewJob(mediaType, originalFilename, filePath string, fileSize int64, ttl time.Duration) (*Job, error) {
	if mediaType == "" {
		return nil, fmt.Errorf("%w: mediaType is required", ErrInvalidJob)
	}
	if filePath == "" {
		return nil, fmt.Errorf("%w: filePath is required", ErrInvalidJob)
	}
	if fileSize <= 0 {
		return nil, fmt.Errorf("%w: fileSize must be greater than 0", ErrInvalidJob)
	}
	if ttl <= 0 {
		return nil, fmt.Errorf("%w: ttl must be positive", ErrInvalidJob)
	}

	now := time.Now().UTC()
	return &Job{
		ID:               uuid.New().String(),
		MediaType:        mediaType,
		OriginalFilename: originalFilename,
		FilePath:         filePath,
		FileSize:         fileSize,
		Status:           StatusPending,
		Artifacts:        make([]Artifact, 0),
		CreatedAt:        now,
		UpdatedAt:        now,
		ExpiresAt:        now.Add(ttl),
	}, nil
}

// TransitionTo validates and performs a state transition.
func (j *Job) TransitionTo(next JobStatus, errorMsg ...string) error {
	if !j.Status.CanTransitionTo(next) {
		return fmt.Errorf("%w: cannot transition from %s to %s", ErrInvalidStateTransition, j.Status, next)
	}

	j.Status = next
	j.UpdatedAt = time.Now().UTC()

	if next == StatusFailed && len(errorMsg) > 0 && errorMsg[0] != "" {
		msg := errorMsg[0]
		j.ErrorMessage = &msg
	}

	return nil
}

// AddArtifact appends a valid artifact associated with this job.
func (j *Job) AddArtifact(artifact Artifact) error {
	if artifact.JobID != j.ID {
		return fmt.Errorf("%w: expected %s, got %s", ErrMismatchedArtifactJobID, j.ID, artifact.JobID)
	}
	j.Artifacts = append(j.Artifacts, artifact)
	j.UpdatedAt = time.Now().UTC()
	return nil
}

// IsExpired checks if the job expiration timestamp is before or equal to the given time.
func (j *Job) IsExpired(at time.Time) bool {
	return !j.ExpiresAt.After(at)
}
