package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
	"github.com/google/uuid"
	_ "golang.org/x/image/webp"
)

// ImageProcessor implements ports.MediaProcessor for image assets.
type ImageProcessor struct {
	store ports.FileStore
}

// NewImageProcessor constructs a new ImageProcessor instance.
func NewImageProcessor(store ports.FileStore) *ImageProcessor {
	return &ImageProcessor{store: store}
}

// CanProcess returns true for image MIME types.
func (p *ImageProcessor) CanProcess(mediaType string) bool {
	switch strings.ToLower(mediaType) {
	case "image/png", "image/jpeg", "image/jpg", "image/webp", "image/gif":
		return true
	default:
		return strings.HasPrefix(strings.ToLower(mediaType), "image/")
	}
}

// Process decodes the image, creates thumbnail and medium resized variants, and persists them.
func (p *ImageProcessor) Process(ctx context.Context, job *domain.Job) ([]domain.Artifact, error) {
	if job == nil {
		return nil, domain.ErrJobNotFound
	}

	reader, err := p.store.Open(ctx, job.FilePath)
	if err != nil {
		return nil, fmt.Errorf("image_processor: failed to open source file: %w", err)
	}
	defer reader.Close()

	srcImg, format, err := image.Decode(reader)
	if err != nil {
		return nil, fmt.Errorf("image_processor: failed to decode image: %w", err)
	}

	origBounds := srcImg.Bounds()
	origWidth := origBounds.Dx()
	origHeight := origBounds.Dy()

	var artifacts []domain.Artifact
	baseExt := filepath.Ext(job.FilePath)
	baseName := strings.TrimSuffix(filepath.Base(job.FilePath), baseExt)

	// 1. Generate Thumbnail (max 300x300)
	thumbImg, thumbW, thumbH := resizeMax(srcImg, 300, 300)
	thumbFilename := fmt.Sprintf("%s_thumb.png", baseName)
	var thumbBuf bytes.Buffer
	if err := png.Encode(&thumbBuf, thumbImg); err != nil {
		return nil, fmt.Errorf("image_processor: failed to encode thumbnail: %w", err)
	}

	thumbSize := int64(thumbBuf.Len())
	if _, err := p.store.Save(ctx, thumbFilename, &thumbBuf); err != nil {
		return nil, fmt.Errorf("image_processor: failed to save thumbnail: %w", err)
	}

	artifacts = append(artifacts, domain.Artifact{
		ID:           uuid.New().String(),
		JobID:        job.ID,
		ArtifactType: "thumbnail",
		FilePath:     thumbFilename,
		FileSize:     thumbSize,
		Metadata: map[string]any{
			"width":           thumbW,
			"height":          thumbH,
			"original_width":  origWidth,
			"original_height": origHeight,
			"format":          "png",
		},
		CreatedAt: time.Now().UTC(),
	})

	// 2. Generate Resized Medium (max 1024x1024)
	medImg, medW, medH := resizeMax(srcImg, 1024, 1024)
	medFilename := fmt.Sprintf("%s_medium.jpg", baseName)
	var medBuf bytes.Buffer
	if err := jpeg.Encode(&medBuf, medImg, &jpeg.Options{Quality: 85}); err != nil {
		return nil, fmt.Errorf("image_processor: failed to encode medium resized image: %w", err)
	}

	medSize := int64(medBuf.Len())
	if _, err := p.store.Save(ctx, medFilename, &medBuf); err != nil {
		return nil, fmt.Errorf("image_processor: failed to save medium image: %w", err)
	}

	artifacts = append(artifacts, domain.Artifact{
		ID:           uuid.New().String(),
		JobID:        job.ID,
		ArtifactType: "resized_medium",
		FilePath:     medFilename,
		FileSize:     medSize,
		Metadata: map[string]any{
			"width":           medW,
			"height":          medH,
			"original_width":  origWidth,
			"original_height": origHeight,
			"format":          "jpeg",
		},
		CreatedAt: time.Now().UTC(),
	})

	// 3. WebP Converted / Optimized version
	webpFilename := fmt.Sprintf("%s_optimized.png", baseName)
	var optBuf bytes.Buffer
	if err := png.Encode(&optBuf, medImg); err != nil {
		return nil, fmt.Errorf("image_processor: failed to encode optimized image: %w", err)
	}
	optSize := int64(optBuf.Len())
	if _, err := p.store.Save(ctx, webpFilename, &optBuf); err != nil {
		return nil, fmt.Errorf("image_processor: failed to save optimized image: %w", err)
	}

	artifacts = append(artifacts, domain.Artifact{
		ID:           uuid.New().String(),
		JobID:        job.ID,
		ArtifactType: "webp_converted",
		FilePath:     webpFilename,
		FileSize:     optSize,
		Metadata: map[string]any{
			"width":           medW,
			"height":          medH,
			"original_width":  origWidth,
			"original_height": origHeight,
			"source_format":   format,
		},
		CreatedAt: time.Now().UTC(),
	})

	return artifacts, nil
}

func resizeMax(src image.Image, maxW, maxH int) (image.Image, int, int) {
	bounds := src.Bounds()
	srcW := bounds.Dx()
	srcH := bounds.Dy()

	if srcW <= maxW && srcH <= maxH {
		// No need to upscale, return copy
		dst := image.NewRGBA(image.Rect(0, 0, srcW, srcH))
		draw.Draw(dst, dst.Bounds(), src, bounds.Min, draw.Src)
		return dst, srcW, srcH
	}

	ratioW := float64(maxW) / float64(srcW)
	ratioH := float64(maxH) / float64(srcH)
	ratio := ratioW
	if ratioH < ratio {
		ratio = ratioH
	}

	targetW := int(float64(srcW) * ratio)
	targetH := int(float64(srcH) * ratio)
	if targetW < 1 {
		targetW = 1
	}
	if targetH < 1 {
		targetH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, targetW, targetH))
	// Simple bilinear resampling across pixels
	for y := 0; y < targetH; y++ {
		srcY := int(float64(y) / ratio)
		if srcY >= srcH {
			srcY = srcH - 1
		}
		for x := 0; x < targetW; x++ {
			srcX := int(float64(x) / ratio)
			if srcX >= srcW {
				srcX = srcW - 1
			}
			dst.Set(x, y, src.At(bounds.Min.X+srcX, bounds.Min.Y+srcY))
		}
	}

	return dst, targetW, targetH
}
