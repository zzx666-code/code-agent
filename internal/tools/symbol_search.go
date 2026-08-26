package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"mewcode/internal/retrieval"
)

// SymbolSearchTool exposes the offline AST/keyword hybrid index to the agent.
// It is read-only and deliberately independent from the optional embedding RAG.
type SymbolSearchTool struct {
	Root string
}

func (t *SymbolSearchTool) Name() string { return "SymbolSearch" }
func (t *SymbolSearchTool) Description() string {
	return "Search source symbols, directories, and stitched code context without embeddings"
}
func (t *SymbolSearchTool) Category() ToolCategory { return CategoryRead }

func (t *SymbolSearchTool) Schema() map[string]any {
	return map[string]any{
		"name": t.Name(), "description": t.Description(),
		"input_schema": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query": map[string]any{"type": "string"},
				"path":  map[string]any{"type": "string", "description": "Optional file or directory root"},
				"mode":  map[string]any{"type": "string", "enum": []string{"keyword", "symbol", "hybrid"}},
				"top_k": map[string]any{"type": "integer", "default": 5},
			},
			"required": []string{"query"},
		},
	}
}

func (t *SymbolSearchTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return ToolResult{Output: "Error: query is required", IsError: true}
	}
	root := t.Root
	if path, ok := args["path"].(string); ok && strings.TrimSpace(path) != "" {
		root = path
	}
	if root == "" {
		return ToolResult{Output: "Error: search root is not configured", IsError: true}
	}
	select {
	case <-ctx.Done():
		return ToolResult{Output: "Error: search cancelled", IsError: true}
	default:
	}
	idx, err := retrieval.Build(filepath.Clean(root))
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Error building symbol index: %s", err), IsError: true}
	}
	mode := retrieval.ModeHybrid
	if raw, ok := args["mode"].(string); ok {
		mode = retrieval.SearchMode(raw)
	}
	topK := intArg(args, "top_k", 5)
	if topK < 1 {
		topK = 5
	}
	results := idx.Search(query, retrieval.SearchOptions{Mode: mode, TopK: topK, ContextLines: 3})
	if len(results) == 0 {
		return ToolResult{Output: "No symbol or code results found."}
	}
	var out strings.Builder
	fmt.Fprintf(&out, "Found %d symbol result(s):\n\n", len(results))
	for n, result := range results {
		name := "file"
		if result.Symbol != nil {
			name = result.Symbol.Kind + " " + result.Symbol.Name
		}
		fmt.Fprintf(&out, "[%d] %s (%s:%d-%d, score %.3f)\n%s\n---\n", n+1, name, result.FilePath, result.StartLine, result.EndLine, result.Score, result.Context)
	}
	return ToolResult{Output: out.String()}
}
