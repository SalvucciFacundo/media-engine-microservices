package domain

import "errors"

var (
	// ErrInvalidStateTransition is returned when a job transition violates lifecycle rules.
	ErrInvalidStateTransition = errors.New("invalid job state transition")

	// ErrJobNotFound is returned when a requested job does not exist.
	ErrJobNotFound = errors.New("job not found")

	// ErrUnsupportedMediaType is returned when media type is not supported.
	ErrUnsupportedMediaType = errors.New("unsupported media type")

	// ErrInvalidJob is returned when job attributes fail validation.
	ErrInvalidJob = errors.New("invalid job attributes")

	// ErrInvalidArtifact is returned when artifact attributes fail validation.
	ErrInvalidArtifact = errors.New("invalid artifact attributes")

	// ErrMismatchedArtifactJobID is returned when an artifact's JobID does not match the target job.
	ErrMismatchedArtifactJobID = errors.New("artifact job id does not match target job")
)
