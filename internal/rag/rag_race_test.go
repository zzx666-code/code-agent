package rag

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

// TestStoreConcurrentInsertAndSearch: 并发写 + 并发读，-race 下不应报错
func TestStoreConcurrentInsertAndSearch(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()
	store.SetModel("race-test", 8)

	// 预填基础数据
	baseChunks := make([]Chunk, 50)
	for i := range baseChunks {
		vec := make([]float32, 8)
		vec[i%8] = 1
		baseChunks[i] = Chunk{
			FilePath: fmt.Sprintf("base_%d.go", i),
			StartLine: 1, EndLine: 10,
			Content: "base", Embedding: vec,
		}
	}
	store.InsertChunks(context.Background(), baseChunks)

	var wg sync.WaitGroup

	// 5 个 goroutine 并发插入
	for w := 0; w < 5; w++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				chunks := []Chunk{{
					FilePath: fmt.Sprintf("w%d_%d.go", id, i),
					StartLine: 1, EndLine: 5,
					Content: "concurrent write",
					Embedding: []float32{float32(id), float32(i), 0, 0, 0, 0, 0, 0},
				}}
				store.InsertChunks(context.Background(), chunks)
			}
		}(w)
	}

	// 10 个 goroutine 并发查询
	for r := 0; r < 10; r++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			queryVec := []float32{float32(id % 8), 0, 0, 0, 0, 0, 0, 0}
			for i := 0; i < 100; i++ {
				store.Search(context.Background(), queryVec, 5)
			}
		}(r)
	}

	wg.Wait()
	// 不 panic、不 race 即通过
}

// TestStoreConcurrentClearAndSearch: 并发 Clear + Search 不 crash
func TestStoreConcurrentClearAndSearch(t *testing.T) {
	store, _ := NewStore(t.TempDir())
	defer store.Close()
	store.SetModel("race-clear", 4)

	// 预填
	chunks := make([]Chunk, 100)
	for i := range chunks {
		chunks[i] = Chunk{
			FilePath: fmt.Sprintf("f%d.go", i),
			StartLine: 1, EndLine: 5,
			Content: "x", Embedding: []float32{float32(i % 4), 0, 0, 0},
		}
	}
	store.InsertChunks(context.Background(), chunks)

	var wg sync.WaitGroup

	// 1 个 goroutine 反复 Clear
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 20; i++ {
			store.Clear()
		}
	}()

	// 5 个 goroutine 反复 Search
	for r := 0; r < 5; r++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			q := []float32{1, 0, 0, 0}
			for i := 0; i < 100; i++ {
				store.Search(context.Background(), q, 5)
			}
		}()
	}

	wg.Wait()
}
