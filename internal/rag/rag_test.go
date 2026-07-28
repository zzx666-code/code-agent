package rag

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStoreInsertAndSearch(t *testing.T) {
	tmp := t.TempDir()
	store, err := NewStore(tmp)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	if err := store.SetModel("test-model", 3); err != nil {
		t.Fatalf("SetModel: %v", err)
	}

	chunks := []Chunk{
		{FilePath: "a.go", StartLine: 1, EndLine: 10, Content: "hello world", Embedding: []float32{1, 0, 0}},
		{FilePath: "b.go", StartLine: 1, EndLine: 5, Content: "goodbye world", Embedding: []float32{0, 1, 0}},
		{FilePath: "c.go", StartLine: 1, EndLine: 5, Content: "hello there", Embedding: []float32{0.9, 0.1, 0}},
	}
	if err := store.InsertChunks(t.Context(), chunks); err != nil {
		t.Fatalf("InsertChunks: %v", err)
	}

	model, dim := store.GetModel()
	if model != "test-model" || dim != 3 {
		t.Fatalf("GetModel = %s/%d, want test-model/3", model, dim)
	}

	results, err := store.Search(t.Context(), []float32{1, 0, 0}, 2)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if results[0].FilePath != "a.go" {
		t.Fatalf("top result = %s, want a.go (score %.4f)", results[0].FilePath, results[0].Score)
	}
	if results[1].FilePath != "c.go" {
		t.Fatalf("second result = %s, want c.go", results[1].FilePath)
	}

	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.ChunkCount != 3 || stats.FileCount != 3 {
		t.Fatalf("Stats = %d chunks/%d files, want 3/3", stats.ChunkCount, stats.FileCount)
	}

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	stats, _ = store.Stats()
	if stats.ChunkCount != 0 {
		t.Fatalf("after Clear, chunk count = %d, want 0", stats.ChunkCount)
	}
}

func TestChunkerSlidingWindow(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.go")
	content := "package main\n\nfunc a() {\n"
	for i := 0; i < 1000; i++ {
		content += "\tx := someFunctionCall(" + string(rune('a'+i%26)) + ", anotherArg, yetAnotherArg)\n"
	}
	content += "}\n"
	os.WriteFile(path, []byte(content), 0o644)

	chunks, err := ChunkPath(path)
	if err != nil {
		t.Fatalf("ChunkPath: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks for large file, got %d", len(chunks))
	}
	if chunks[0].Language != "go" || chunks[0].ChunkType != "code" {
		t.Fatalf("language/type = %s/%s, want go/code", chunks[0].Language, chunks[0].ChunkType)
	}
	if chunks[0].StartLine != 1 {
		t.Fatalf("first chunk start = %d, want 1", chunks[0].StartLine)
	}
	for _, c := range chunks {
		if c.EndLine < c.StartLine {
			t.Fatalf("chunk %s has end < start: %d < %d", c.FilePath, c.EndLine, c.StartLine)
		}
	}
}

func TestChunkerSkipDirs(t *testing.T) {
	tmp := t.TempDir()
	os.MkdirAll(filepath.Join(tmp, ".git"), 0o755)
	os.MkdirAll(filepath.Join(tmp, "src"), 0o755)
	os.WriteFile(filepath.Join(tmp, ".git", "config"), []byte("git config"), 0o644)
	os.WriteFile(filepath.Join(tmp, "src", "main.go"), []byte("package main\n"), 0o644)

	chunks, err := ChunkPath(tmp)
	if err != nil {
		t.Fatalf("ChunkPath: %v", err)
	}
	for _, c := range chunks {
		if filepath.Dir(c.FilePath) == filepath.Join(tmp, ".git") {
			t.Fatalf(".git dir was not skipped: %s", c.FilePath)
		}
	}
	if len(chunks) == 0 {
		t.Fatalf("expected at least one chunk from src/")
	}
}

func TestEncodeDecodeFloats(t *testing.T) {
	original := []float32{1.0, -1.0, 0.5, -0.5, 0, 3.14159}
	encoded := encodeFloats(original)
	decoded, err := decodeFloats(encoded)
	if err != nil {
		t.Fatalf("decodeFloats: %v", err)
	}
	if len(decoded) != len(original) {
		t.Fatalf("length mismatch: %d vs %d", len(decoded), len(original))
	}
	for i := range original {
		if decoded[i] != original[i] {
			t.Fatalf("at index %d: got %v, want %v", i, decoded[i], original[i])
		}
	}
}

func TestCosineSim(t *testing.T) {
	tests := []struct {
		a, b []float32
		want float32
	}{
		{[]float32{1, 0, 0}, []float32{1, 0, 0}, 1.0},
		{[]float32{1, 0, 0}, []float32{0, 1, 0}, 0.0},
		{[]float32{1, 1, 0}, []float32{1, 1, 0}, 1.0},
		{[]float32{1, 0, 0}, []float32{-1, 0, 0}, -1.0},
		{[]float32{}, []float32{1}, 0.0},
	}
	for _, tc := range tests {
		got := cosineSim(tc.a, tc.b)
		if abs32(got-tc.want) > 1e-6 {
			t.Fatalf("cosineSim(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func abs32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}
