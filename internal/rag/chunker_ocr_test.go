package rag

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func newOcrForTest(baseURL string) *OcrClient {
	client := openai.NewClient(option.WithAPIKey("test"), option.WithBaseURL(baseURL))
	return &OcrClient{client: &client, model: "test-vision"}
}

func writeTestJPEG(t *testing.T, w, h int) []byte {
	return writeTestJPEGVariant(t, w, h, 0)
}

func writeTestJPEGVariant(t *testing.T, w, h, variant int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: uint8((128 + variant) % 256), A: 255})
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func writeTestPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(y % 256), G: uint8(x % 256), B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// buildPdfFromImages creates a real PDF from encoded image files via pdfcpu.
// Each image lands on its own page. Returned PDF is written to a temp file.
func buildPdfFromImages(t *testing.T, imgs [][]byte, ext string) string {
	t.Helper()
	dir := t.TempDir()
	var files []string
	for i, data := range imgs {
		fn := filepath.Join(dir, fmt.Sprintf("img%03d.%s", i, ext))
		if err := os.WriteFile(fn, data, 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, fn)
	}
	out := filepath.Join(dir, "doc.pdf")
	if err := api.ImportImagesFile(files, out, nil, nil); err != nil {
		t.Fatalf("ImportImagesFile: %v", err)
	}
	return out
}

func TestChunkPdfTextOnlyNoOcr(t *testing.T) {
	path := buildPdfFromImages(t, [][]byte{writeTestJPEG(t, 800, 600)}, "jpg")
	// The generated PDF has no text layer, but no OCR client either.
	chunks, err := ChunkPathWithContext(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("ChunkPathWithContext: %v", err)
	}
	if len(chunks) != 0 {
		t.Fatalf("no ocr client -> no chunks expected, got %d", len(chunks))
	}
}

func TestChunkPdfImagePageOcr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"OCR result from image page"}}]}`)
	}))
	defer srv.Close()

	path := buildPdfFromImages(t, [][]byte{writeTestJPEG(t, 1240, 1754)}, "jpg")

	ocr := newOcrForTest(srv.URL)
	chunks, err := ChunkPathWithContext(context.Background(), path, ocr)
	if err != nil {
		t.Fatalf("ChunkPathWithContext: %v", err)
	}
	var found bool
	for _, c := range chunks {
		if c.ChunkType == "pdf_image" && strings.Contains(c.Content, "OCR result from image page") {
			found = true
			if c.StartLine != 1 || c.EndLine != 1 {
				t.Fatalf("page num = %d-%d, want 1-1", c.StartLine, c.EndLine)
			}
		}
	}
	if !found {
		t.Fatalf("no pdf_image chunk with OCR text; chunks=%v", chunks)
	}
	if ocr.callCount == 0 {
		t.Fatalf("expected at least one OCR call")
	}
}

func TestChunkPdfFlateImageOcr(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"png image text"}}]}`)
	}))
	defer srv.Close()

	path := buildPdfFromImages(t, [][]byte{writeTestPNG(t, 400, 300)}, "png")

	ocr := newOcrForTest(srv.URL)
	chunks, err := ChunkPathWithContext(context.Background(), path, ocr)
	if err != nil {
		t.Fatalf("ChunkPathWithContext: %v", err)
	}
	var found bool
	for _, c := range chunks {
		if c.ChunkType == "pdf_image" && strings.Contains(c.Content, "png image text") {
			found = true
		}
	}
	if !found {
		t.Fatalf("png image OCR chunk missing; chunks=%v", chunks)
	}
}

func TestChunkPdfSmallImageSkipped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"should not be called"}}]}`)
	}))
	defer srv.Close()

	path := buildPdfFromImages(t, [][]byte{writeTestJPEG(t, 50, 40)}, "jpg")

	ocr := newOcrForTest(srv.URL)
	chunks, err := ChunkPathWithContext(context.Background(), path, ocr)
	if err != nil {
		t.Fatalf("ChunkPathWithContext: %v", err)
	}
	if ocr.callCount != 0 {
		t.Fatalf("small icon should not trigger OCR, calls = %d", ocr.callCount)
	}
	for _, c := range chunks {
		if c.ChunkType == "pdf_image" {
			t.Fatalf("unexpected pdf_image chunk")
		}
	}
}

func TestChunkPdfOcrBudget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"x"}}]}`)
	}))
	defer srv.Close()

	var imgs [][]byte
	for i := 0; i < 25; i++ {
		imgs = append(imgs, writeTestJPEGVariant(t, 800, 600, i))
	}
	path := buildPdfFromImages(t, imgs, "jpg")

	ocr := newOcrForTest(srv.URL)
	if _, err := ChunkPathWithContext(context.Background(), path, ocr); err != nil {
		t.Fatalf("ChunkPathWithContext: %v", err)
	}
	if ocr.callCount > ocrMaxImagesPerFile {
		t.Fatalf("callCount = %d, want <= %d", ocr.callCount, ocrMaxImagesPerFile)
	}
	if len(ocr.skipReason) == 0 {
		t.Fatalf("expected budget-exceeded skip reason")
	}
	if report := ocr.Report(); !strings.Contains(report, "上限") {
		t.Fatalf("report = %q", report)
	}
}

func TestExtractRequestContainsPngDataUrl(t *testing.T) {
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := new(bytes.Buffer)
		buf.ReadFrom(r.Body)
		captured = buf.String()
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	}))
	defer srv.Close()

	ocr := newOcrForTest(srv.URL)
	pngData := []byte{0x89, 0x50, 0x4e, 0x47}
	if _, err := ocr.Extract(context.Background(), pngData, "image/png"); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngData)
	if !strings.Contains(captured, want) {
		t.Fatalf("request missing %s", want)
	}
}

func TestExtractPdfImagesRealPdf(t *testing.T) {
	dir := t.TempDir()
	jpgFile := filepath.Join(dir, "source.jpg")
	if err := os.WriteFile(jpgFile, writeTestJPEG(t, 640, 480), 0o644); err != nil {
		t.Fatal(err)
	}
	pngFile := filepath.Join(dir, "source.png")
	if err := os.WriteFile(pngFile, writeTestPNG(t, 320, 240), 0o644); err != nil {
		t.Fatal(err)
	}
	mixed := filepath.Join(dir, "mixed.pdf")
	if err := api.ImportImagesFile([]string{jpgFile, pngFile}, mixed, nil, nil); err != nil {
		t.Fatalf("ImportImagesFile: %v", err)
	}

	images, skips := extractPdfImages(mixed)
	if len(skips) > 0 {
		t.Fatalf("unexpected skips: %v", skips)
	}
	if len(images) < 2 {
		t.Fatalf("expected >= 2 images, got %d", len(images))
	}
	for _, img := range images {
		if img.pageNr < 1 {
			t.Fatalf("pageNr = %d", img.pageNr)
		}
		if len(img.data) == 0 {
			t.Fatalf("empty image data")
		}
		switch img.mime {
		case "image/png", "image/jpeg", "image/tiff":
		default:
			t.Fatalf("unexpected mime %q", img.mime)
		}
	}
}
