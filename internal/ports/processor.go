package ports

import (
	"context"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
)

// MediaProcessor defines the interface for polymorphic media processing handlers (Image, PDF, etc.).
type MediaProcessor interface {
	// CanProcess returns true if this processor can handle the specified media MIME type.
	CanProcess(mediaType string) bool

	// Process executes the transformation/extraction pipeline on the input job and returns generated artifacts.
	Process(ctx context.Context, job *domain.Job) ([]domain.Artifact, error)
}
