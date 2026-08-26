package rag

import (
	"archive/zip"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pdf "github.com/ledongthuc/pdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

const (
	chunkTokenTarget  = 512
	chunkTokenOverlap = 50
	skipDirsName      = ".mewcode-skip"
)

var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "__pycache__": true,
	"dist": true, "build": true, "vendor": true, ".mewcode": true,
	".venv": true, "venv": true, ".tox": true, ".mypy_cache": true,
	".idea": true, ".vscode": true, "target": true, ".next": true,
}

var skipExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".bmp": true,
	".ico": true, ".webp": true, ".svg": true,
	".mp3": true, ".mp4": true, ".avi": true, ".mov": true,
	".zip": true, ".gz": true, ".tar": true, ".rar": true, ".7z": true,
	".doc": true, ".xls": true, ".xlsx": true,
	".exe": true, ".dll": true, ".so": true, ".dylib": true, ".o": true,
	".a": true, ".class": true, ".jar": true, ".pyc": true,
	".db": true, ".sqlite": true, ".lock": true, ".sum": true,
	".min.js": true, ".min.css": true,
}

type FileChunk struct {
	FilePath  string
	StartLine int
	EndLine   int
	ChunkType string
	Language  string
	Content   string
}

func ChunkPath(root string) ([]FileChunk, error) {
	return ChunkPathWithContext(context.Background(), root, nil)
}

func ChunkPathWithContext(ctx context.Context, root string, ocr *OcrClient) ([]FileChunk, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return chunkFile(ctx, root, ocr)
	}
	var all []FileChunk
	err = filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if info.IsDir() {
			if skipDirs[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(info.Name()) {
			return nil
		}
		chunks, err := chunkFile(ctx, path, ocr)
		if err != nil {
			return nil
		}
		all = append(all, chunks...)
		return nil
	})
	return all, err
}

func shouldSkipFile(name string) bool {
	if strings.HasPrefix(name, ".") && name != ".gitignore" && name != ".env.example" {
		return true
	}
	ext := strings.ToLower(filepath.Ext(name))
	if skipExts[ext] {
		return true
	}
	if strings.HasSuffix(strings.ToLower(name), ".min.js") ||
		strings.HasSuffix(strings.ToLower(name), ".min.css") {
		return true
	}
	if info, err := os.Stat(name); err == nil && info.Size() > 512*1024 {
		return true
	}
	return false
}

func detectLanguage(path string) (lang, chunkType string) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".go":
		return "go", "code"
	case ".py":
		return "python", "code"
	case ".js", ".jsx", ".mjs":
		return "javascript", "code"
	case ".ts", ".tsx":
		return "typescript", "code"
	case ".rs":
		return "rust", "code"
	case ".java":
		return "java", "code"
	case ".kt":
		return "kotlin", "code"
	case ".rb":
		return "ruby", "code"
	case ".cpp", ".cc", ".cxx":
		return "cpp", "code"
	case ".c", ".h":
		return "c", "code"
	case ".cs":
		return "csharp", "code"
	case ".php":
		return "php", "code"
	case ".swift":
		return "swift", "code"
	case ".scala":
		return "scala", "code"
	case ".sh", ".bash":
		return "shell", "code"
	case ".sql":
		return "sql", "code"
	case ".html", ".htm":
		return "html", "doc"
	case ".css", ".scss", ".less":
		return "css", "code"
	case ".yaml", ".yml":
		return "yaml", "config"
	case ".json":
		return "json", "config"
	case ".toml":
		return "toml", "config"
	case ".xml":
		return "xml", "config"
	case ".md", ".markdown":
		return "markdown", "doc"
	case ".txt", ".rst":
		return "text", "doc"
	case ".docx":
		return "docx", "doc"
	case ".pdf":
		return "pdf", "doc"
	default:
		return "text", "text"
	}
}

func chunkFile(ctx context.Context, path string, ocr *OcrClient) ([]FileChunk, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".docx" {
		return chunkDocxFile(path)
	}
	if ext == ".pdf" {
		return chunkPdfFile(ctx, path, ocr)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if isBinaryContent(data) {
		return nil, nil
	}
	content := string(data)
	if strings.TrimSpace(content) == "" {
		return nil, nil
	}
	lang, chunkType := detectLanguage(path)
	lines := strings.Split(content, "\n")
	if chunkType == "doc" {
		return chunkByParagraph(path, lines, lang, chunkType), nil
	}
	return chunkBySlidingWindow(path, lines, lang, chunkType), nil
}

// isBinaryContent heuristically detects binary files by checking for NUL bytes
// or a high ratio of non-printable/non-UTF8 bytes in the first 4KB.
func isBinaryContent(data []byte) bool {
	sample := data
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	if len(sample) == 0 {
		return false
	}
	nonPrintable := 0
	for _, b := range sample {
		if b == 0 {
			return true
		}
		if b < 0x09 || (b > 0x0d && b < 0x20) {
			nonPrintable++
		}
	}
	return float64(nonPrintable)/float64(len(sample)) > 0.30
}

func chunkBySlidingWindow(path string, lines []string, lang, chunkType string) []FileChunk {
	var chunks []FileChunk
	var buf []string
	var startLine int
	var tokenCount int
	flush := func(endLine int) {
		if len(buf) == 0 {
			return
		}
		chunks = append(chunks, FileChunk{
			FilePath:  path,
			StartLine: startLine + 1,
			EndLine:   endLine,
			ChunkType: chunkType,
			Language:  lang,
			Content:   strings.Join(buf, "\n"),
		})
	}
	for i, line := range lines {
		tokens := estimateTokens(line)
		if tokenCount > 0 && tokenCount+tokens > chunkTokenTarget {
			flush(i)
			overlap := chunkTokenOverlap
			if overlap > len(buf) {
				overlap = len(buf)
			}
			buf = append([]string{}, buf[len(buf)-overlap:]...)
			startLine = i - overlap
			tokenCount = 0
			for _, l := range buf {
				tokenCount += estimateTokens(l)
			}
		}
		if tokenCount == 0 {
			startLine = i
		}
		buf = append(buf, line)
		tokenCount += tokens
	}
	flush(len(lines))
	return chunks
}

func chunkByParagraph(path string, lines []string, lang, chunkType string) []FileChunk {
	var chunks []FileChunk
	var buf []string
	var startLine int
	var tokenCount int
	flush := func(endLine int) {
		if len(buf) == 0 {
			return
		}
		chunks = append(chunks, FileChunk{
			FilePath:  path,
			StartLine: startLine + 1,
			EndLine:   endLine,
			ChunkType: chunkType,
			Language:  lang,
			Content:   strings.Join(buf, "\n"),
		})
	}
	for i, line := range lines {
		tokens := estimateTokens(line)
		isHeading := strings.HasPrefix(strings.TrimSpace(line), "#")
		if (isHeading || (tokenCount > chunkTokenTarget && strings.TrimSpace(line) == "")) && len(buf) > 0 {
			flush(i)
			buf = nil
			tokenCount = 0
		}
		if tokenCount == 0 {
			startLine = i
		}
		buf = append(buf, line)
		tokenCount += tokens
	}
	flush(len(lines))
	return chunks
}

func estimateTokens(s string) int {
	return (len(s) + 3) / 4
}

func chunkDocxFile(path string) ([]FileChunk, error) {
	text, err := extractDocxText(path)
	if err != nil {
		return nil, fmt.Errorf("read docx: %w", err)
	}
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	return chunkByParagraph(path, lines, "docx", "doc"), nil
}

func chunkPdfFile(ctx context.Context, path string, ocr *OcrClient) ([]FileChunk, error) {
	text, err := extractPdfText(path)
	if err != nil {
		return nil, fmt.Errorf("read pdf: %w", err)
	}
	var chunks []FileChunk
	if strings.TrimSpace(text) != "" {
		lines := strings.Split(text, "\n")
		chunks = chunkByParagraph(path, lines, "pdf", "doc")
	}
	if !ocr.Available() {
		if len(chunks) == 0 {
			return nil, nil
		}
		return chunks, nil
	}
	imgChunks := chunkPdfImages(ctx, path, ocr)
	return append(chunks, imgChunks...), nil
}

type pdfExtractedImage struct {
	pageNr int
	data   []byte
	mime   string
	width  int
	height int
	isMask bool
}

// extractPdfImages extracts decoded embedded images with their page numbers
// via pdfcpu. Works with ObjStm-based (PDF 1.5+) files, supports DCT / Flate /
// CCITT / LZW / RunLength filters and SMask composition.
func extractPdfImages(path string) ([]pdfExtractedImage, []string) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer f.Close()

	conf := model.NewDefaultConfiguration()
	ctx, err := api.ReadValidateAndOptimize(f, conf)
	if err != nil {
		return nil, nil
	}

	var skips []string
	var result []pdfExtractedImage
	seen := map[int]bool{}

	for pageNr := 1; pageNr <= ctx.PageCount; pageNr++ {
		for objNr, img := range ctx.Optimize.ImageObjects {
			if seen[objNr] {
				continue
			}
			if _, ok := img.ResourceNames[pageNr-1]; !ok {
				continue
			}
			seen[objNr] = true

			sd := img.ImageDict
			w := sd.IntEntry("Width")
			h := sd.IntEntry("Height")
			if w == nil || h == nil {
				continue
			}
			var isMask bool
			if b := sd.BooleanEntry("ImageMask"); b != nil && *b {
				isMask = true
			}

			if err := sd.Decode(); err != nil {
				skips = append(skips, fmt.Sprintf("obj#%d: 解码失败: %v", objNr, err))
				continue
			}
			rendered, fileType, err := pdfcpu.RenderImage(ctx.XRefTable, sd, false, "", objNr)
			if err != nil {
				var unsupported *api.UnsupportedResourceError
				if !errors.As(err, &unsupported) {
					skips = append(skips, fmt.Sprintf("obj#%d: %v", objNr, err))
				}
				continue
			}
			if rendered == nil {
				continue
			}
			data, err := io.ReadAll(rendered)
			if err != nil {
				skips = append(skips, fmt.Sprintf("obj#%d: 读取失败: %v", objNr, err))
				continue
			}
			mime := "image/png"
			switch fileType {
			case "jpg":
				mime = "image/jpeg"
			case "tif":
				mime = "image/tiff"
			}
			result = append(result, pdfExtractedImage{
				pageNr: pageNr,
				data:   data,
				mime:   mime,
				width:  *w,
				height: *h,
				isMask: isMask,
			})
		}
	}
	return result, skips
}

func chunkPdfImages(ctx context.Context, path string, ocr *OcrClient) []FileChunk {
	images, skips := extractPdfImages(path)
	for _, s := range skips {
		ocr.RecordSkip(fmt.Sprintf("%s: %s", filepath.Base(path), s))
	}
	if len(images) == 0 {
		return nil
	}

	pageHasText := pdfPageTextMap(path)

	var chunks []FileChunk
	ocrBudget := ocrMaxImagesPerFile
	for _, img := range images {
		if ctx.Err() != nil {
			break
		}
		if img.isMask || ocrBudget <= 0 {
			if ocrBudget <= 0 {
				ocr.RecordSkip(fmt.Sprintf("%s: 图片数超过上限 %d, 余下跳过", filepath.Base(path), ocrMaxImagesPerFile))
				break
			}
			continue
		}
		if img.width < ocrMinImageDimension || img.height < ocrMinImageDimension {
			continue
		}
		// A full-page image on a page that already carries a text layer is a
		// decorative background; the text layer already covers retrieval.
		if pdfIsFullPageImage(img) && pageHasText[img.pageNr] {
			continue
		}
		ocrBudget--
		text, err := ocr.Extract(ctx, img.data, img.mime)
		if err != nil {
			ocr.RecordSkip(fmt.Sprintf("%s p%d 图片 OCR 失败: %v", filepath.Base(path), img.pageNr, err))
			continue
		}
		if strings.TrimSpace(text) == "" {
			continue
		}
		lines := strings.Split(text, "\n")
		pageChunks := chunkByParagraph(path, lines, "pdf", "pdf_image")
		for j := range pageChunks {
			pageChunks[j].StartLine = img.pageNr
			pageChunks[j].EndLine = img.pageNr
		}
		chunks = append(chunks, pageChunks...)
	}
	return chunks
}

// pdfIsFullPageImage reports whether the image dimensions match a typical
// full-page scan (A4/Letter at 72-200 dpi).
func pdfIsFullPageImage(img pdfExtractedImage) bool {
	if img.width < 600 || img.height < 600 {
		return false
	}
	ratio := float64(img.width) / float64(img.height)
	return ratio > 0.6 && ratio < 0.85
}

func pdfPageTextMap(path string) map[int]bool {
	textPath, cleanup, err := normalizePDFContentStreams(path)
	if err != nil {
		return nil
	}
	defer cleanup()
	f, r, err := pdf.Open(textPath)
	if err != nil {
		return nil
	}
	defer f.Close()
	result := map[int]bool{}
	totalPages := r.NumPage()
	for i := 1; i <= totalPages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		result[i] = strings.TrimSpace(text) != ""
	}
	return result
}

func extractPdfText(path string) (string, error) {
	textPath, cleanup, err := normalizePDFContentStreams(path)
	if err != nil {
		return "", fmt.Errorf("normalize pdf content streams: %w", err)
	}
	defer cleanup()
	f, r, err := pdf.Open(textPath)
	if err != nil {
		return "", fmt.Errorf("open normalized pdf: %w", err)
	}
	defer f.Close()
	var sb strings.Builder
	totalPages := r.NumPage()
	for i := 1; i <= totalPages; i++ {
		page := r.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, err := page.GetPlainText(nil)
		if err != nil {
			continue
		}
		sb.WriteString(text)
		if i < totalPages {
			sb.WriteString("\n\n")
		}
	}
	return sb.String(), nil
}

func normalizePDFContentStreams(path string) (string, func(), error) {
	input, err := os.Open(path)
	if err != nil {
		return "", func() {}, err
	}

	conf := model.NewDefaultConfiguration()
	ctx, err := api.ReadValidateAndOptimize(input, conf)
	if err != nil {
		input.Close()
		return "", func() {}, err
	}
	for pageNr := 1; pageNr <= ctx.PageCount; pageNr++ {
		pageDict, _, _, err := ctx.PageDict(pageNr, false)
		if err != nil {
			input.Close()
			return "", func() {}, fmt.Errorf("page %d: %w", pageNr, err)
		}
		content, err := ctx.PageContent(pageDict, pageNr)
		if err != nil {
			input.Close()
			return "", func() {}, fmt.Errorf("page %d content: %w", pageNr, err)
		}
		content = append(append([]byte(nil), content...), '\n')
		contentRef, err := ctx.XRefTable.StreamDictIndRef(content)
		if err != nil {
			input.Close()
			return "", func() {}, fmt.Errorf("page %d content stream: %w", pageNr, err)
		}
		pageDict["Contents"] = *contentRef
	}

	output, err := os.CreateTemp("", "mewcode-pdf-text-*.pdf")
	if err != nil {
		input.Close()
		return "", func() {}, err
	}
	outputPath := output.Name()
	writeErr := api.WriteContext(ctx, output)
	closeErr := output.Close()
	input.Close()
	if writeErr != nil {
		os.Remove(outputPath)
		return "", func() {}, writeErr
	}
	if closeErr != nil {
		os.Remove(outputPath)
		return "", func() {}, closeErr
	}
	return outputPath, func() { _ = os.Remove(outputPath) }, nil
}

func extractDocxText(path string) (string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer r.Close()
	var docFile *zip.File
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			docFile = f
			break
		}
	}
	if docFile == nil {
		return "", fmt.Errorf("word/document.xml not found in docx")
	}
	rc, err := docFile.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return "", err
	}
	return parseDocxXML(string(data)), nil
}

func parseDocxXML(xml string) string {
	var sb strings.Builder
	inT := false
	var textBuf strings.Builder
	for i := 0; i < len(xml); i++ {
		if xml[i] == '<' {
			if inT {
				sb.WriteString(textBuf.String())
				textBuf.Reset()
				inT = false
			}
			if i+3 < len(xml) && xml[i:i+4] == "<w:t" {
				gt := strings.IndexByte(xml[i:], '>')
				if gt < 0 {
					break
				}
				i += gt
				inT = true
			} else if i+4 < len(xml) && xml[i:i+5] == "<w:p " || (i+3 < len(xml) && xml[i:i+4] == "<w:p>") {
				sb.WriteString("\n")
			}
		} else if xml[i] == '>' && inT {
			continue
		} else if inT {
			textBuf.WriteByte(xml[i])
		}
	}
	if inT {
		sb.WriteString(textBuf.String())
	}
	return sb.String()
}

var _ = bufio.NewReader
