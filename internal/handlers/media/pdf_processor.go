package media

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/SalvucciFacundo/media-engine-microservices/internal/domain"
	"github.com/SalvucciFacundo/media-engine-microservices/internal/ports"
	"github.com/google/uuid"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// PDFProcessor implements ports.MediaProcessor for PDF documents.
type PDFProcessor struct {
	store ports.FileStore
}

// NewPDFProcessor constructs a new PDFProcessor instance.
func NewPDFProcessor(store ports.FileStore) *PDFProcessor {
	return &PDFProcessor{store: store}
}

// CanProcess returns true for PDF MIME types.
func (p *PDFProcessor) CanProcess(mediaType string) bool {
	clean := strings.ToLower(strings.TrimSpace(mediaType))
	return clean == "application/pdf" || clean == "application/x-pdf"
}

// Process parses the PDF document, extracts text content, renders a cover thumbnail, and stores artifacts.
func (p *PDFProcessor) Process(ctx context.Context, job *domain.Job) ([]domain.Artifact, error) {
	if job == nil {
		return nil, domain.ErrJobNotFound
	}

	reader, err := p.store.Open(ctx, job.FilePath)
	if err != nil {
		return nil, fmt.Errorf("pdf_processor: failed to open source file: %w", err)
	}
	defer reader.Close()

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("pdf_processor: failed to read PDF data: %w", err)
	}

	// Validate PDF header
	if !bytes.HasPrefix(data, []byte("%PDF-")) && !bytes.Contains(data[:min(len(data), 1024)], []byte("%PDF-")) {
		return nil, fmt.Errorf("pdf_processor: invalid PDF header or corrupt file")
	}

	extractedText, pageCount := extractTextAndPagesFromPDF(data)
	if extractedText == "" {
		extractedText = fmt.Sprintf("[Document: %s - %d page(s)]", job.OriginalFilename, pageCount)
	}

	baseExt := filepath.Ext(job.FilePath)
	baseName := strings.TrimSuffix(filepath.Base(job.FilePath), baseExt)

	var artifacts []domain.Artifact

	// 1. Save Extracted Text Artifact
	textFilename := fmt.Sprintf("%s_extracted.txt", baseName)
	textBuf := bytes.NewBufferString(extractedText)
	if _, err := p.store.Save(ctx, textFilename, textBuf); err != nil {
		return nil, fmt.Errorf("pdf_processor: failed to save extracted text: %w", err)
	}

	artifacts = append(artifacts, domain.Artifact{
		ID:           uuid.New().String(),
		JobID:        job.ID,
		ArtifactType: "extracted_text",
		FilePath:     textFilename,
		FileSize:     int64(len(extractedText)),
		Metadata: map[string]any{
			"page_count":      pageCount,
			"character_count": len(extractedText),
			"filename":        job.OriginalFilename,
		},
		CreatedAt: time.Now().UTC(),
	})

	// 2. Render Cover Thumbnail Image (PNG)
	coverImg := renderPDFCoverCard(job.OriginalFilename, pageCount, extractedText)
	var coverBuf bytes.Buffer
	if err := png.Encode(&coverBuf, coverImg); err != nil {
		return nil, fmt.Errorf("pdf_processor: failed to encode cover image: %w", err)
	}

	coverFilename := fmt.Sprintf("%s_cover.png", baseName)
	coverSize := int64(coverBuf.Len())
	if _, err := p.store.Save(ctx, coverFilename, &coverBuf); err != nil {
		return nil, fmt.Errorf("pdf_processor: failed to save cover thumbnail: %w", err)
	}

	artifacts = append(artifacts, domain.Artifact{
		ID:           uuid.New().String(),
		JobID:        job.ID,
		ArtifactType: "thumbnail",
		FilePath:     coverFilename,
		FileSize:     coverSize,
		Metadata: map[string]any{
			"width":      coverImg.Bounds().Dx(),
			"height":     coverImg.Bounds().Dy(),
			"page_count": pageCount,
			"format":     "png",
		},
		CreatedAt: time.Now().UTC(),
	})

	return artifacts, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var (
	textObjRegex = regexp.MustCompile(`\((.*?)\)\s*Tj`)
	textArrRegex = regexp.MustCompile(`\[(.*?)\]\s*TJ`)
	pageObjRegex = regexp.MustCompile(`/Type\s*/Page\b`)
)

func extractTextAndPagesFromPDF(data []byte) (string, int) {
	content := string(data)

	// Count /Type /Page occurrences
	pageMatches := pageObjRegex.FindAllString(content, -1)
	pageCount := len(pageMatches)
	if pageCount == 0 {
		pageCount = 1
	}

	var sb strings.Builder
	// Extract text from (text) Tj commands
	for _, match := range textObjRegex.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			cleaned := strings.TrimSpace(match[1])
			if cleaned != "" {
				sb.WriteString(cleaned)
				sb.WriteString("\n")
			}
		}
	}

	// Extract text from [(text1) (text2)] TJ commands
	for _, match := range textArrRegex.FindAllStringSubmatch(content, -1) {
		if len(match) > 1 {
			inner := match[1]
			for _, sub := range textObjRegex.FindAllStringSubmatch(inner, -1) {
				if len(sub) > 1 {
					cleaned := strings.TrimSpace(sub[1])
					if cleaned != "" {
						sb.WriteString(cleaned)
						sb.WriteString(" ")
					}
				}
			}
			sb.WriteString("\n")
		}
	}

	return strings.TrimSpace(sb.String()), pageCount
}

func renderPDFCoverCard(filename string, pageCount int, previewText string) image.Image {
	w, h := 320, 440
	img := image.NewRGBA(image.Rect(0, 0, w, h))

	// Background
	bgColor := color.RGBA{R: 248, G: 250, B: 252, A: 255} // Slate-50
	draw.Draw(img, img.Bounds(), &image.Uniform{C: bgColor}, image.Point{}, draw.Src)

	// Border
	borderColor := color.RGBA{R: 203, G: 213, B: 225, A: 255} // Slate-300
	for x := 0; x < w; x++ {
		img.Set(x, 0, borderColor)
		img.Set(x, h-1, borderColor)
	}
	for y := 0; y < h; y++ {
		img.Set(0, y, borderColor)
		img.Set(w-1, y, borderColor)
	}

	// Red Header Banner for PDF
	bannerColor := color.RGBA{R: 239, G: 68, B: 68, A: 255} // Red-500
	bannerRect := image.Rect(0, 0, w, 40)
	draw.Draw(img, bannerRect, &image.Uniform{C: bannerColor}, image.Point{}, draw.Src)

	// Text rendering helper
	drawText(img, 16, 26, "PDF DOCUMENT", color.White)

	textColor := color.RGBA{R: 15, G: 23, B: 42, A: 255}
	displayTitle := filename
	if len(displayTitle) > 30 {
		displayTitle = displayTitle[:27] + "..."
	}
	drawText(img, 16, 75, displayTitle, textColor)

	subColor := color.RGBA{R: 100, G: 116, B: 139, A: 255}
	drawText(img, 16, 100, fmt.Sprintf("Pages: %d", pageCount), subColor)

	// Content preview
	previewColor := color.RGBA{R: 51, G: 65, B: 85, A: 255}
	lines := strings.Split(previewText, "\n")
	yPos := 140
	for i, line := range lines {
		if i > 12 || yPos > h-40 {
			break
		}
		trimmed := strings.TrimSpace(line)
		if len(trimmed) > 36 {
			trimmed = trimmed[:33] + "..."
		}
		if trimmed != "" {
			drawText(img, 16, yPos, trimmed, previewColor)
			yPos += 20
		}
	}

	return img
}

func drawText(img *image.RGBA, x, y int, label string, col color.Color) {
	point := fixed.Point26_6{
		X: fixed.I(x),
		Y: fixed.I(y),
	}
	d := &font.Drawer{
		Dst:  img,
		Src:  &image.Uniform{C: col},
		Face: basicfont.Face7x13,
		Dot:  point,
	}
	d.DrawString(label)
}
