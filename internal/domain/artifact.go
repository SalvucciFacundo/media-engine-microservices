package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Artifact represents a generated media or text output associated with a Job.
type Artifact struct {
	ID           string         `json:"id"`
	JobID        string         `json:"job_id"`
	ArtifactType string         `json:"artifact_type"`
	FilePath     string         `json:"file_path"`
	FileSize     int64          `json:"file_size"`
	Metadata     map[string]any `json:"metadata,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
}

// NewArtifact validates and constructs a new Artifact instance.
func NewArtifact(jobID, artifactType, filePath string, fileSize int64, metadata map[string]any) (Artifact, error) {
	if jobID == "" {
		return Artifact{}, fmt.Errorf("%w: jobID is required", ErrInvalidArtifact)
	}
	if artifactType == "" {
		return Artifact{}, fmt.Errorf("%w: artifactType is required", ErrInvalidArtifact)
	}
	if filePath == "" {
		return Artifact{}, fmt.Errorf("%w: filePath is required", ErrInvalidArtifact)
	}
	if fileSize < 0 {
		return Artifact{}, fmt.Errorf("%w: fileSize cannot be negative", ErrInvalidArtifact)
	}

	if metadata == nil {
		metadata = make(map[string]any)
	}

	return Artifact{
		ID:           uuid.New().String(),
		JobID:        jobID,
		ArtifactType: artifactType,
		FilePath:     filePath,
		FileSize:     fileSize,
		Metadata:     metadata,
		CreatedAt:    time.Now().UTC(),
	}, nil
}
