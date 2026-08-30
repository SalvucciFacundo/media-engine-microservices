package ports

import (
	"context"
	"io"
)

// FileStore defines operations for persisting and retrieving raw media and generated artifacts.
type FileStore interface {
	// Save writes data from reader to a destination file under subpath/filename and returns the absolute or canonical path.
	Save(ctx context.Context, relativePath string, data io.Reader) (string, error)

	// Open opens the file for reading.
	Open(ctx context.Context, relativeOrFullPath string) (io.ReadCloser, error)

	// Delete removes the file located at relativeOrFullPath.
	Delete(ctx context.Context, relativeOrFullPath string) error

	// Exists checks whether a file exists at relativeOrFullPath.
	Exists(ctx context.Context, relativeOrFullPath string) (bool, error)

	// GetPath returns the canonical filesystem path for a relative path.
	GetPath(relativePath string) string
}
