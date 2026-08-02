package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mewcode/internal/config"
	"mewcode/internal/rag"
)

type RagIndexTool struct {
	Store    *rag.Store
	Embedder *rag.Embedder
}

func (t *RagIndexTool) Name() string           { return "RagIndex" }
func (t *RagIndexTool) Description() string    { return RagIndexDescription }
func (t *RagIndexTool) Category() ToolCategory { return CategoryRead }

func (t *RagIndexTool) Schema() map[string]any {
	return map[string]any{
		"name":        t.Name(),
		"description": t.Description(),
		"input_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{
					"type":        "string",
					"description": "Absolute or relative path to a file or directory to index",
				},
				"recursive": map[string]any{
					"type":        "boolean",
					"description": "Whether to recursively scan directories (default: true)",
					"default":     true,
				},
			},
			"required": []string{"path"},
		},
	}
}

func (t *RagIndexTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	return t.ExecuteWithProgress(ctx, args, nil)
}

func (t *RagIndexTool) ExecuteWithProgress(ctx context.Context, args map[string]any, progress func(msg string)) ToolResult {
	path, _ := args["path"].(string)
	if path == "" {
		return ToolResult{Output: "Error: path is required", IsError: true}
	}
	path = trimPathQuotes(path)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Error resolving path: %s", err), IsError: true}
	}
	if _, err := os.Stat(absPath); err != nil {
		return ToolResult{Output: fmt.Sprintf("Error: path not found: %s", absPath), IsError: true}
	}
	if t.Store == nil || t.Embedder == nil {
		return ToolResult{Output: "Error: RAG not initialized (embedding_model not configured)", IsError: true}
	}

	if progress != nil {
		progress(fmt.Sprintf("正在扫描并切分文件: %s", absPath))
	}
	chunks, err := rag.ChunkPath(absPath)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Error chunking: %s", err), IsError: true}
	}
	if len(chunks) == 0 {
		return ToolResult{Output: "No indexable content found (file may be binary or unsupported)"}
	}

	fileSet := map[string]struct{}{}
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Content
		fileSet[c.FilePath] = struct{}{}
	}

	if progress != nil {
		progress(fmt.Sprintf("扫描完成: %d 个 chunk, %d 个文件。正在生成 embedding...", len(chunks), len(fileSet)))
	}
	embeddings, dim, err := t.Embedder.Embed(ctx, texts)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Error generating embeddings: %s", err), IsError: true}
	}

	if progress != nil {
		progress("Embedding 完成，正在写入向量库...")
	}
	storeChunks := make([]rag.Chunk, len(chunks))
	for i, c := range chunks {
		storeChunks[i] = rag.Chunk{
			FilePath:  c.FilePath,
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
			ChunkType: c.ChunkType,
			Language:  c.Language,
			Content:   c.Content,
			Embedding: embeddings[i],
		}
	}
	if err := t.Store.SetModel(t.Embedder.Model(), dim); err != nil {
		return ToolResult{Output: fmt.Sprintf("Error setting model: %s", err), IsError: true}
	}
	if err := t.Store.InsertChunks(ctx, storeChunks); err != nil {
		return ToolResult{Output: fmt.Sprintf("Error storing chunks: %s", err), IsError: true}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("已索引 %d 个 chunk，来自 %d 个文件\n", len(chunks), len(fileSet)))
	for fp := range fileSet {
		sb.WriteString("  - " + fp + "\n")
	}
	return ToolResult{Output: sb.String()}
}

func trimPathQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

type RagSearchTool struct {
	Store    *rag.Store
	Embedder *rag.Embedder
}

func (t *RagSearchTool) Name() string           { return "RagSearch" }
func (t *RagSearchTool) Description() string    { return RagSearchDescription }
func (t *RagSearchTool) Category() ToolCategory { return CategoryRead }

func (t *RagSearchTool) Schema() map[string]any {
	return map[string]any{
		"name":        t.Name(),
		"description": t.Description(),
		"input_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "Natural language query to search for",
				},
				"top_k": map[string]any{
					"type":        "integer",
					"description": "Maximum number of results to return (default: 5)",
					"default":     5,
				},
			},
			"required": []string{"query"},
		},
	}
}

func (t *RagSearchTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	query, _ := args["query"].(string)
	if query == "" {
		return ToolResult{Output: "Error: query is required", IsError: true}
	}
	if t.Store == nil || t.Embedder == nil {
		return ToolResult{Output: "Error: RAG not initialized (embedding_model not configured)", IsError: true}
	}
	topK := intArg(args, "top_k", 5)
	if topK < 1 {
		topK = 5
	}

	queryVec, _, err := t.Embedder.EmbedOne(ctx, query)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Error embedding query: %s", err), IsError: true}
	}
	results, err := t.Store.Search(ctx, queryVec, topK)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Error searching: %s", err), IsError: true}
	}
	if len(results) == 0 {
		return ToolResult{Output: "No results found. Use RagIndex to index files first."}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d result(s):\n\n", len(results)))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("[%d] %s:%d-%d (score: %.4f)\n", i+1, r.FilePath, r.StartLine, r.EndLine, r.Score))
		sb.WriteString(r.Content)
		sb.WriteString("\n---\n")
	}
	return ToolResult{Output: sb.String()}
}

type RagClearTool struct {
	Store *rag.Store
}

func (t *RagClearTool) Name() string           { return "RagClear" }
func (t *RagClearTool) Description() string    { return RagClearDescription }
func (t *RagClearTool) Category() ToolCategory { return CategoryRead }

func (t *RagClearTool) Schema() map[string]any {
	return map[string]any{
		"name":        t.Name(),
		"description": t.Description(),
		"input_schema": map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func (t *RagClearTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	if t.Store == nil {
		return ToolResult{Output: "Error: RAG store not initialized", IsError: true}
	}
	if err := t.Store.Clear(); err != nil {
		return ToolResult{Output: fmt.Sprintf("Error clearing: %s", err), IsError: true}
	}
	return ToolResult{Output: "索引已清空"}
}

func NewRAGStore(baseDir string, providerCfg *config.ProviderConfig) (*rag.Store, *rag.Embedder, error) {
	store, err := rag.NewStore(baseDir)
	if err != nil {
		return nil, nil, err
	}
	if providerCfg == nil || providerCfg.EmbeddingModel == "" {
		return store, nil, nil
	}
	embedder, err := rag.NewEmbedder(providerCfg)
	if err != nil {
		store.Close()
		return nil, nil, err
	}
	return store, embedder, nil
}
