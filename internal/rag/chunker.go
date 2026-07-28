package rag

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	chunkTokenTarget = 512
	chunkTokenOverlap = 50
	skipDirsName = ".mewcode-skip"
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
	".pdf": true, ".doc": true, ".xls": true, ".xlsx": true,
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
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", root, err)
	}
	if !info.IsDir() {
		return chunkFile(root)
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
		chunks, err := chunkFile(path)
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
	default:
		return "text", "text"
	}
}

func chunkFile(path string) ([]FileChunk, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".docx" {
		return chunkDocxFile(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
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
