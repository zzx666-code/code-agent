package rag

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- RAG 检索质量测试（不依赖 API，用构造向量） ---

// TestSearchPrecisionByTopic: 同主题向量应聚在一起，查询应召回同主题
func TestSearchPrecisionByTopic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()
	store.SetModel("test", 8)

	// 5 个主题，每个主题 5 个 chunk，同主题向量相近（基向量 + 小噪声）
	topics := [][]float32{
		{1, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 0, 0, 0, 0, 0, 0},
		{0, 0, 1, 0, 0, 0, 0, 0},
		{0, 0, 0, 1, 0, 0, 0, 0},
		{0, 0, 0, 0, 1, 0, 0, 0},
	}
	topicNames := []string{"auth", "db", "log", "net", "cache"}
	var chunks []Chunk
	for ti, base := range topics {
		for i := 0; i < 5; i++ {
			vec := make([]float32, 8)
			copy(vec, base)
			vec[ti] += 0.1 * rand.Float32()
			vec[(ti+1)%8] += 0.01 * rand.Float32()
			chunks = append(chunks, Chunk{
				FilePath:  fmt.Sprintf("%s_%d.go", topicNames[ti], i),
				StartLine: 1, EndLine: 10,
				Content:   fmt.Sprintf("%s content %d", topicNames[ti], i),
				Embedding: vec,
			})
		}
	}
	store.InsertChunks(context.Background(), chunks)

	for ti, queryVec := range topics {
		results, err := store.Search(context.Background(), queryVec, 5)
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		hits := 0
		for _, r := range results {
			if strings.Contains(r.FilePath, topicNames[ti]) {
				hits++
			}
		}
		if hits < 4 {
			t.Errorf("topic %s: expected >=4 hits in top5, got %d", topicNames[ti], hits)
		}
	}
}

// TestSearchRankOrder: 结果应按 score 降序
func TestSearchRankOrder(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	defer store.Close()
	store.SetModel("test", 4)
	chunks := []Chunk{
		{FilePath: "a", Content: "a", Embedding: []float32{1, 0, 0, 0}},
		{FilePath: "b", Content: "b", Embedding: []float32{0.9, 0.1, 0, 0}},
		{FilePath: "c", Content: "c", Embedding: []float32{0.5, 0.5, 0, 0}},
		{FilePath: "d", Content: "d", Embedding: []float32{0, 1, 0, 0}},
	}
	store.InsertChunks(context.Background(), chunks)
	results, _ := store.Search(context.Background(), []float32{1, 0, 0, 0}, 4)
	for i := 1; i < len(results); i++ {
		if results[i].Score > results[i-1].Score {
			t.Errorf("results not sorted desc: [%d]=%.4f > [%d]=%.4f", i, results[i].Score, i-1, results[i-1].Score)
		}
	}
}

// TestSearchEmptyStore: 空库查询不报错
func TestSearchEmptyStore(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	defer store.Close()
	store.SetModel("test", 4)
	results, err := store.Search(context.Background(), []float32{1, 0, 0, 0}, 5)
	if err != nil {
		t.Errorf("empty store search should not error: %v", err)
	}
	if results != nil && len(results) != 0 {
		t.Errorf("empty store should return nil/empty, got %d", len(results))
	}
}

// TestStorePersistAndReload: 索引持久化后重新打开数据仍在
func TestStorePersistAndReload(t *testing.T) {
	dir := t.TempDir()
	store1, _ := NewStore(dir)
	store1.SetModel("test-model", 4)
	store1.InsertChunks(context.Background(), []Chunk{
		{FilePath: "a.go", StartLine: 1, EndLine: 5, Content: "hello", Embedding: []float32{1, 0, 0, 0}},
	})
	stats1, _ := store1.Stats()
	if stats1.ChunkCount != 1 {
		t.Fatalf("before close: chunk count = %d", stats1.ChunkCount)
	}
	store1.Close()

	store2, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer store2.Close()
	stats2, _ := store2.Stats()
	if stats2.ChunkCount != 1 {
		t.Errorf("after reload: chunk count = %d, want 1", stats2.ChunkCount)
	}
	model, dim := store2.GetModel()
	if model != "test-model" || dim != 4 {
		t.Errorf("after reload: model=%s dim=%d, want test-model/4", model, dim)
	}
	results, _ := store2.Search(context.Background(), []float32{1, 0, 0, 0}, 1)
	if len(results) != 1 || results[0].FilePath != "a.go" {
		t.Errorf("after reload: search result = %+v", results)
	}
}

// TestChunkerDocxUnsupported: 不存在的 docx 不 crash
func TestChunkerDocxUnsupported(t *testing.T) {
	chunks, err := chunkDocxFile("/nonexistent/path.docx")
	if err == nil && len(chunks) > 0 {
		t.Error("expected error or empty for nonexistent docx")
	}
}

// TestChunkerBinaryDetection: 二进制文件被跳过
func TestChunkerBinaryDetection(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "binary.bin")
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i % 256)
	}
	os.WriteFile(path, data, 0o644)
	chunks, err := ChunkPath(path)
	if err != nil {
		t.Errorf("binary file should not error: %v", err)
	}
	if len(chunks) != 0 {
		t.Errorf("binary file should produce 0 chunks, got %d", len(chunks))
	}
}

// TestChunkerMarkdownParagraph: markdown 按段落切分
func TestChunkerMarkdownParagraph(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "test.md")
	content := "# Title\n\nFirst paragraph here.\n\n## Section\n\nSecond paragraph with more text.\n\n# Another Title\n\nThird section."
	os.WriteFile(path, []byte(content), 0o644)
	chunks, err := ChunkPath(path)
	if err != nil {
		t.Fatalf("ChunkPath: %v", err)
	}
	if len(chunks) < 2 {
		t.Fatalf("expected markdown to split into >=2 chunks, got %d", len(chunks))
	}
	if chunks[0].Language != "markdown" || chunks[0].ChunkType != "doc" {
		t.Errorf("language/type = %s/%s, want markdown/doc", chunks[0].Language, chunks[0].ChunkType)
	}
}

// --- RAG 性能基准 ---

// BenchmarkStoreInsert: 批量写入性能
func BenchmarkStoreInsert(b *testing.B) {
	for _, n := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				store, _ := NewStore(b.TempDir())
				store.SetModel("bench", 128)
				chunks := make([]Chunk, n)
				for j := 0; j < n; j++ {
					vec := make([]float32, 128)
					for k := range vec {
						vec[k] = rand.Float32()
					}
					chunks[j] = Chunk{
						FilePath:  fmt.Sprintf("file%d.go", j),
						StartLine: 1, EndLine: 10,
						Content:   fmt.Sprintf("content %d", j),
						Embedding: vec,
					}
				}
				b.StartTimer()
				store.InsertChunks(context.Background(), chunks)
				b.StopTimer()
				store.Close()
			}
		})
	}
}

// BenchmarkStoreSearch: 向量检索性能（不同规模）
func BenchmarkStoreSearch(b *testing.B) {
	for _, n := range []int{100, 500, 1000, 5000} {
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			store, _ := NewStore(b.TempDir())
			defer store.Close()
			store.SetModel("bench", 128)
			chunks := make([]Chunk, n)
			for j := 0; j < n; j++ {
				vec := make([]float32, 128)
				for k := range vec {
					vec[k] = rand.Float32()
				}
				chunks[j] = Chunk{
					FilePath:  fmt.Sprintf("file%d.go", j),
					StartLine: 1, EndLine: 10,
					Content:   fmt.Sprintf("content %d", j),
					Embedding: vec,
				}
			}
			store.InsertChunks(context.Background(), chunks)
			queryVec := make([]float32, 128)
			for k := range queryVec {
				queryVec[k] = rand.Float32()
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				store.Search(context.Background(), queryVec, 5)
			}
		})
	}
}

// BenchmarkEncodeFloats: 向量序列化性能
func BenchmarkEncodeFloats(b *testing.B) {
	vec := make([]float32, 2048)
	for i := range vec {
		vec[i] = rand.Float32()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		encodeFloats(vec)
	}
}

// BenchmarkDecodeFloats: 向量反序列化性能
func BenchmarkDecodeFloats(b *testing.B) {
	vec := make([]float32, 2048)
	for i := range vec {
		vec[i] = rand.Float32()
	}
	buf := encodeFloats(vec)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		decodeFloats(buf)
	}
}

// BenchmarkCosineSim: 余弦相似度计算性能
func BenchmarkCosineSim(b *testing.B) {
	a := make([]float32, 2048)
	bb := make([]float32, 2048)
	for i := range a {
		a[i] = rand.Float32()
		bb[i] = rand.Float32()
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cosineSim(a, bb)
	}
}

// BenchmarkChunkFile: 文件切分性能
func BenchmarkChunkFile(b *testing.B) {
	tmp := b.TempDir()
	path := filepath.Join(tmp, "large.go")
	content := "package main\n\nfunc main() {\n"
	for i := 0; i < 2000; i++ {
		content += fmt.Sprintf("\tx := functionCall%d(arg1, arg2)\n", i)
	}
	content += "}\n"
	os.WriteFile(path, []byte(content), 0o644)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ChunkPath(path)
	}
}
