package main

import (
	"context"
	"fmt"
	"os"

	"mewcode/internal/config"
	"mewcode/internal/tools"
)

func main() {
	pdfPath := os.Args[1]
	query := os.Args[2]
	baseDir := os.Args[3]

	cfg, err := config.LoadConfig(baseDir + "/.mewcode/config.yaml")
	if err != nil {
		fmt.Println("load config:", err)
		os.Exit(1)
	}
	var providerCfg *config.ProviderConfig
	if len(cfg.Providers) > 0 {
		providerCfg = &cfg.Providers[0]
	}

	store, embedder, reranker, ocr, err := tools.NewRAGStore(baseDir, providerCfg)
	if err != nil {
		fmt.Println("NewRAGStore:", err)
		os.Exit(1)
	}
	defer store.Close()

	if err := store.Clear(); err != nil {
		fmt.Println("clear store:", err)
		os.Exit(1)
	}

	indexTool := &tools.RagIndexTool{Store: store, Embedder: embedder, Ocr: ocr}
	progress := func(msg string) { fmt.Println("[progress]", msg) }

	fmt.Println("=== RagIndex ===")
	res := indexTool.ExecuteWithProgress(context.Background(), map[string]any{
		"path":      pdfPath,
		"recursive": true,
	}, progress)
	fmt.Println(res.Output)
	if res.IsError {
		os.Exit(1)
	}

	fmt.Println("\n=== RagSearch (纯向量检索, 无 LLM) ===")
	searchTool := &tools.RagSearchTool{Store: store, Embedder: embedder, Reranker: reranker, Client: nil, FinalTopK: 5}
	sres := searchTool.Execute(context.Background(), map[string]any{
		"query": query,
		"top_k": 5,
	})
	fmt.Println(sres.Output)
}
