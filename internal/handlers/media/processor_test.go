package media_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/adapters/localfs"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/handlers/media"
	"github.com/google/uuid"
)

func createSamplePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 100, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("failed to encode sample png: %v", err)
	}
	return buf.Bytes()
}

func createSampleJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: uint8(y % 256), B: uint8(x % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("failed to encode sample jpeg: %v", err)
	}
	return buf.Bytes()
}

func createSamplePDF(t *testing.T, text string) []byte {
	t.Helper()
	// Minimal standard compliant PDF 1.4 with text content stream
	pdfContent := "%PDF-1.4\n" +
		"1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\n" +
		"2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj\n" +
		"3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >> endobj\n" +
		"4 0 obj << /Length " + string(rune(len(text)+40)) + " >> stream\n" +
		"BT /F1 24 Tf 100 700 Td (" + text + ") Tj ET\n" +
		"endstream\n" +
		"endobj\n" +
		"5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj\n" +
		"xref\n" +
		"0 6\n" +
		"0000000000 65535 f \n" +
		"0000000009 00000 n \n" +
		"0000000058 00000 n \n" +
		"0000000115 00000 n \n" +
		"0000000244 00000 n \n" +
		"0000000344 00000 n \n" +
		"trailer << /Size 6 /Root 1 0 R >>\n" +
		"startxref\n" +
		"425\n" +
		"%%EOF\n"
	return []byte(pdfContent)
}

func TestImageProcessor_CanProcess(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}

	proc := media.NewImageProcessor(store)

	tests := []struct {
		mediaType string
		expected  bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"image/jpg", true},
		{"image/webp", true},
		{"image/gif", true},
		{"application/pdf", false},
		{"text/plain", false},
		{"video/mp4", false},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			if got := proc.CanProcess(tt.mediaType); got != tt.expected {
				t.Errorf("CanProcess(%q) = %v, expected %v", tt.mediaType, got, tt.expected)
			}
		})
	}
}

func TestImageProcessor_Process_PNG(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}

	proc := media.NewImageProcessor(store)
	ctx := context.Background()

	pngBytes := createSamplePNG(t, 1200, 800)
	filename := "test_upload.png"
	if _, err := store.Save(ctx, filename, bytes.NewReader(pngBytes)); err != nil {
		t.Fatalf("failed to save sample png: %v", err)
	}

	jobID := uuid.New().String()
	job := &domain.Job{
		ID:               jobID,
		MediaType:        "image/png",
		OriginalFilename: "photo.png",
		FilePath:         filename,
		FileSize:         int64(len(pngBytes)),
		Status:           domain.StatusProcessing,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
	}

	artifacts, err := proc.Process(ctx, job)
	if err != nil {
		t.Fatalf("Process failed: %v", err)
	}

	if len(artifacts) < 2 {
		t.Fatalf("expected at least 2 artifacts (thumbnail, resized/webp), got %d", len(artifacts))
	}

	var hasThumbnail, hasResized bool
	for _, art := range artifacts {
		if art.JobID != jobID {
			t.Errorf("artifact job_id mismatch: expected %s, got %s", jobID, art.JobID)
		}
		if art.FilePath == "" {
			t.Errorf("artifact file path should not be empty")
		}
		if art.FileSize <= 0 {
			t.Errorf("artifact file size should be > 0, got %d", art.FileSize)
		}

		fullPath := filepath.Join(tempDir, art.FilePath)
		if _, err := os.Stat(fullPath); err != nil {
			t.Errorf("artifact file was not persisted to store: %s", fullPath)
		}

		if art.ArtifactType == "thumbnail" {
			hasThumbnail = true
			if art.Metadata["width"] == nil || art.Metadata["height"] == nil {
				t.Errorf("thumbnail metadata missing dimensions: %+v", art.Metadata)
			}
		}
		if art.ArtifactType == "resized_medium" || art.ArtifactType == "webp_converted" {
			hasResized = true
		}
	}

	if !hasThumbnail {
		t.Errorf("expected thumbnail artifact in output")
	}
	if !hasResized {
		t.Errorf("expected resized/webp artifact in output")
	}
}

func TestImageProcessor_Process_JPEG(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}

	proc := media.NewImageProcessor(store)
	ctx := context.Background()

	jpegBytes := createSampleJPEG(t, 800, 600)
	filename := "test_photo.jpg"
	if _, err := store.Save(ctx, filename, bytes.NewReader(jpegBytes)); err != nil {
		t.Fatalf("failed to save sample jpeg: %v", err)
	}

	jobID := uuid.New().String()
	job := &domain.Job{
		ID:               jobID,
		MediaType:        "image/jpeg",
		OriginalFilename: "photo.jpg",
		FilePath:         filename,
		FileSize:         int64(len(jpegBytes)),
		Status:           domain.StatusProcessing,
	}

	artifacts, err := proc.Process(ctx, job)
	if err != nil {
		t.Fatalf("Process failed for JPEG: %v", err)
	}

	if len(artifacts) < 2 {
		t.Fatalf("expected at least 2 artifacts, got %d", len(artifacts))
	}
}

func TestImageProcessor_Process_CorruptedFile(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}

	proc := media.NewImageProcessor(store)
	ctx := context.Background()

	corruptedBytes := []byte("this is not an image file content")
	filename := "corrupted.png"
	if _, err := store.Save(ctx, filename, bytes.NewReader(corruptedBytes)); err != nil {
		t.Fatalf("failed to save corrupted file: %v", err)
	}

	job := &domain.Job{
		ID:               uuid.New().String(),
		MediaType:        "image/png",
		OriginalFilename: "corrupted.png",
		FilePath:         filename,
		FileSize:         int64(len(corruptedBytes)),
		Status:           domain.StatusProcessing,
	}

	_, err = proc.Process(ctx, job)
	if err == nil {
		t.Fatal("expected error processing corrupted image, got nil")
	}
}

func TestPDFProcessor_CanProcess(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}

	proc := media.NewPDFProcessor(store)

	tests := []struct {
		mediaType string
		expected  bool
	}{
		{"application/pdf", true},
		{"image/png", false},
		{"text/plain", false},
	}

	for _, tt := range tests {
		t.Run(tt.mediaType, func(t *testing.T) {
			if got := proc.CanProcess(tt.mediaType); got != tt.expected {
				t.Errorf("CanProcess(%q) = %v, expected %v", tt.mediaType, got, tt.expected)
			}
		})
	}
}

func TestPDFProcessor_Process_ValidPDF(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}

	proc := media.NewPDFProcessor(store)
	ctx := context.Background()

	sampleText := "Hello Media Engine PDF Processing"
	pdfBytes := createSamplePDF(t, sampleText)
	filename := "sample_document.pdf"
	if _, err := store.Save(ctx, filename, bytes.NewReader(pdfBytes)); err != nil {
		t.Fatalf("failed to save sample pdf: %v", err)
	}

	jobID := uuid.New().String()
	job := &domain.Job{
		ID:               jobID,
		MediaType:        "application/pdf",
		OriginalFilename: "document.pdf",
		FilePath:         filename,
		FileSize:         int64(len(pdfBytes)),
		Status:           domain.StatusProcessing,
		CreatedAt:        time.Now().UTC(),
		UpdatedAt:        time.Now().UTC(),
		ExpiresAt:        time.Now().UTC().Add(1 * time.Hour),
	}

	artifacts, err := proc.Process(ctx, job)
	if err != nil {
		t.Fatalf("PDF Process failed: %v", err)
	}

	if len(artifacts) < 2 {
		t.Fatalf("expected at least 2 artifacts (extracted_text, thumbnail/cover), got %d", len(artifacts))
	}

	var hasTextArtifact, hasCoverThumbnail bool
	for _, art := range artifacts {
		if art.JobID != jobID {
			t.Errorf("artifact job ID mismatch: expected %s, got %s", jobID, art.JobID)
		}
		fullPath := filepath.Join(tempDir, art.FilePath)
		data, err := os.ReadFile(fullPath)
		if err != nil {
			t.Errorf("failed to read artifact file %s: %v", fullPath, err)
		}

		if art.ArtifactType == "extracted_text" {
			hasTextArtifact = true
			if !strings.Contains(string(data), "Hello") && len(data) == 0 {
				t.Errorf("extracted text artifact should not be empty")
			}
		}
		if art.ArtifactType == "thumbnail" || art.ArtifactType == "cover_thumbnail" {
			hasCoverThumbnail = true
		}
	}

	if !hasTextArtifact {
		t.Errorf("expected extracted_text artifact")
	}
	if !hasCoverThumbnail {
		t.Errorf("expected thumbnail artifact for PDF cover")
	}
}

func TestPDFProcessor_Process_CorruptedPDF(t *testing.T) {
	tempDir := t.TempDir()
	store, err := localfs.NewFileStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create file store: %v", err)
	}

	proc := media.NewPDFProcessor(store)
	ctx := context.Background()

	corruptedBytes := []byte("not a real pdf file")
	filename := "corrupted.pdf"
	if _, err := store.Save(ctx, filename, bytes.NewReader(corruptedBytes)); err != nil {
		t.Fatalf("failed to save corrupted pdf: %v", err)
	}

	job := &domain.Job{
		ID:               uuid.New().String(),
		MediaType:        "application/pdf",
		OriginalFilename: "corrupted.pdf",
		FilePath:         filename,
		FileSize:         int64(len(corruptedBytes)),
		Status:           domain.StatusProcessing,
	}

	_, err = proc.Process(ctx, job)
	if err == nil {
		t.Fatal("expected error for corrupted PDF, got nil")
	}
}
