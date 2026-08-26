package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"mewcode/internal/config"
	"mewcode/internal/llm"
	"mewcode/internal/rag"
)

type RagIndexTool struct {
	Store    *rag.Store
	Embedder *rag.Embedder
	Ocr      *rag.OcrClient
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
	chunks, err := rag.ChunkPathWithContext(ctx, absPath, t.Ocr)
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
	if report := t.Ocr.Report(); report != "" {
		sb.WriteString(report + "\n")
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
	Store     *rag.Store
	Embedder  *rag.Embedder
	Reranker  *rag.Reranker
	Client    llm.Client // optional: when set, an LLM re-judges the candidates
	FinalTopK int
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
	finalTopK := t.FinalTopK
	if finalTopK <= 0 {
		finalTopK = 5
	}
	// User-facing top_k parameter; if not provided, default to finalTopK.
	topK := intArg(args, "top_k", finalTopK)
	if topK < 1 {
		topK = finalTopK
	}

	// Two-stage retrieval pipeline. Subagent A (coarse) and subagent B
	// (rerank) run as concurrent goroutines but B depends on A's output, so
	// they are connected by a channel. The final LLM judge runs after both
	// complete; on any failure it falls back to whatever stage succeeded.
	coarseTopK := 50
	if coarseTopK < topK {
		coarseTopK = topK
	}

	// Stage 1 (subagent A): coarse vector retrieval.
	type coarseResult struct {
		results []rag.SearchResult
		err     error
	}
	coarseCh := make(chan coarseResult, 1)
	go func() {
		defer close(coarseCh)
		r, err := rag.CoarseRetrieve(ctx, t.Store, t.Embedder, query, coarseTopK)
		coarseCh <- coarseResult{results: r, err: err}
	}()

	// Wait for coarse stage.
	cr := <-coarseCh
	if cr.err != nil {
		return ToolResult{Output: fmt.Sprintf("Error in coarse retrieval: %s", cr.err), IsError: true}
	}
	if len(cr.results) == 0 {
		return ToolResult{Output: "No results found. Use RagIndex to index files first."}
	}

	// Stage 2 (subagent B): cross-encoder rerank over coarse candidates.
	type rerankResult struct {
		results []rag.SearchResult
		err     error
	}
	rerankCh := make(chan rerankResult, 1)
	go func() {
		defer close(rerankCh)
		r, err := rag.RerankPass(ctx, t.Reranker, query, cr.results, topK)
		rerankCh <- rerankResult{results: r, err: err}
	}()

	rr := <-rerankCh
	candidates := rr.results
	// If rerank failed, fall back to coarse results truncated to topK.
	if rr.err != nil || len(candidates) == 0 {
		candidates = cr.results
		if len(candidates) > topK {
			candidates = candidates[:topK]
		}
	}

	// Stage 3 (LLM judge): let the model re-evaluate the candidates against
	// the query and pick the most relevant ones. Falls back to the
	// post-rerank candidates on any failure.
	results := candidates
	if t.Client != nil {
		judged, err := t.llmJudge(ctx, query, candidates, topK)
		if err == nil && len(judged) > 0 {
			results = judged
		}
		// On error: silent fallback to rerank/coarse results.
	}

	if len(results) > topK {
		results = results[:topK]
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

// llmJudge asks the LLM to re-evaluate candidates against the query and returns
// the LLM-selected most-relevant candidates, ordered by the LLM's score. The
// returned slice preserves the original SearchResult content but reorders
// candidates per the LLM verdict, dropping any the LLM marked irrelevant.
func (t *RagSearchTool) llmJudge(ctx context.Context, query string, candidates []rag.SearchResult, topK int) ([]rag.SearchResult, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	// Cap the candidate count sent to the LLM to keep the prompt bounded.
	// The rerank stage already narrowed to ~topK, but when reranker is nil
	// candidates may be as large as coarseTopK (50). Truncate to 15 for the
	// judge prompt.
	judgeCands := candidates
	if len(judgeCands) > 15 {
		judgeCands = judgeCands[:15]
	}

	jc := make([]llm.JudgeCandidate, len(judgeCands))
	for i, r := range judgeCands {
		jc[i] = llm.JudgeCandidate{
			Index:    i,
			FilePath: fmt.Sprintf("%s:%d-%d", r.FilePath, r.StartLine, r.EndLine),
			Content:  r.Content,
		}
	}

	verdicts, err := llm.JudgeRerankResults(ctx, t.Client, query, jc)
	if err != nil {
		return nil, err
	}

	// Map LLM verdicts back to SearchResults, keeping only relevant ones and
	// ordering by the LLM's score (JudgeRerankResults already sorts desc).
	out := make([]rag.SearchResult, 0, len(verdicts))
	for _, v := range verdicts {
		if !v.Relevant {
			continue
		}
		if v.Index < 0 || v.Index >= len(judgeCands) {
			continue
		}
		c := judgeCands[v.Index]
		c.Score = float32(v.Score)
		out = append(out, c)
	}
	if len(out) == 0 {
		// LLM marked nothing relevant - fall back to input order.
		return nil, fmt.Errorf("llm judge: no relevant candidates")
	}
	if len(out) > topK {
		out = out[:topK]
	}
	return out, nil
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

func NewRAGStore(baseDir string, providerCfg *config.ProviderConfig) (*rag.Store, *rag.Embedder, *rag.Reranker, *rag.OcrClient, error) {
	store, err := rag.NewStore(baseDir)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	if providerCfg == nil || providerCfg.EmbeddingModel == "" {
		return store, nil, nil, nil, nil
	}
	embedder, err := rag.NewEmbedder(providerCfg)
	if err != nil {
		store.Close()
		return nil, nil, nil, nil, err
	}
	var reranker *rag.Reranker
	if providerCfg.RerankModel != "" {
		apiKey := providerCfg.ResolveRerankAPIKey()
		if apiKey != "" {
			reranker, err = rag.NewReranker(apiKey, providerCfg.RerankModel, providerCfg.RerankURL)
			if err != nil {
				// Non-fatal: keep RAG usable even if reranker misconfigured.
				reranker = nil
			}
		}
	}
	var ocr *rag.OcrClient
	if res := rag.NewOcrClient(providerCfg); res.Err != nil {
		ocr = nil
	} else {
		ocr = res.Client
	}
	return store, embedder, reranker, ocr, nil
}
