package localfs

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
)

var (
	ErrInvalidPath       = errors.New("invalid or unsafe file path")
	ErrEmptyBaseDir      = errors.New("base directory cannot be empty")
	ErrNilReader         = errors.New("reader cannot be nil")
)

type FileStore struct {
	baseDir string
}

// Compile-time check that FileStore implements ports.FileStore.
var _ ports.FileStore = (*FileStore)(nil)

// NewFileStore creates a new LocalFS FileStore rooted at baseDir.
func NewFileStore(baseDir string) (*FileStore, error) {
	if strings.TrimSpace(baseDir) == "" {
		return nil, ErrEmptyBaseDir
	}

	cleanBase, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return nil, fmt.Errorf("failed to resolve base directory: %w", err)
	}

	if err := os.MkdirAll(cleanBase, 0755); err != nil {
		return nil, fmt.Errorf("failed to create base directory: %w", err)
	}

	return &FileStore{baseDir: cleanBase}, nil
}

// resolvePath securely ensures that relativePath does not escape baseDir.
func (f *FileStore) resolvePath(relativePath string) (string, error) {
	if relativePath == "" {
		return "", ErrInvalidPath
	}

	// If absolute path provided and is already inside baseDir, accept it
	if filepath.IsAbs(relativePath) {
		clean := filepath.Clean(relativePath)
		if clean == f.baseDir || strings.HasPrefix(clean, f.baseDir+string(filepath.Separator)) {
			return clean, nil
		}
		return "", fmt.Errorf("%w: path outside base directory", ErrInvalidPath)
	}

	cleanRel := filepath.Clean(relativePath)
	if strings.HasPrefix(cleanRel, "..") || filepath.IsAbs(cleanRel) {
		return "", fmt.Errorf("%w: path traversal detected", ErrInvalidPath)
	}

	joined := filepath.Join(f.baseDir, cleanRel)
	cleanJoined := filepath.Clean(joined)

	if cleanJoined != f.baseDir && !strings.HasPrefix(cleanJoined, f.baseDir+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: resolved path escapes base directory", ErrInvalidPath)
	}

	return cleanJoined, nil
}

// Save writes data to a destination file under relativePath.
func (f *FileStore) Save(ctx context.Context, relativePath string, data io.Reader) (string, error) {
	if data == nil {
		return "", ErrNilReader
	}

	targetPath, err := f.resolvePath(relativePath)
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(targetPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directories for %s: %w", targetPath, err)
	}

	out, err := os.Create(targetPath)
	if err != nil {
		return "", fmt.Errorf("failed to create file %s: %w", targetPath, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, data); err != nil {
		return "", fmt.Errorf("failed to write data to %s: %w", targetPath, err)
	}

	return targetPath, nil
}

// Open opens a file for reading.
func (f *FileStore) Open(ctx context.Context, relativeOrFullPath string) (io.ReadCloser, error) {
	targetPath, err := f.resolvePath(relativeOrFullPath)
	if err != nil {
		return nil, err
	}

	file, err := os.Open(targetPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file %s: %w", targetPath, err)
	}
	return file, nil
}

// Delete removes the file located at relativeOrFullPath. It is idempotent if file does not exist.
func (f *FileStore) Delete(ctx context.Context, relativeOrFullPath string) error {
	targetPath, err := f.resolvePath(relativeOrFullPath)
	if err != nil {
		return err
	}

	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete file %s: %w", targetPath, err)
	}
	return nil
}

// Exists checks if the file exists.
func (f *FileStore) Exists(ctx context.Context, relativeOrFullPath string) (bool, error) {
	targetPath, err := f.resolvePath(relativeOrFullPath)
	if err != nil {
		return false, err
	}

	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return !info.IsDir(), nil
}

// GetPath returns the canonical filesystem path for a relative path.
func (f *FileStore) GetPath(relativePath string) string {
	cleanRel := filepath.Clean(relativePath)
	if filepath.IsAbs(cleanRel) {
		return cleanRel
	}
	return filepath.Join(f.baseDir, cleanRel)
}
