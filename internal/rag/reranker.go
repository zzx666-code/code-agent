package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// RerankResult is a single reranked document returned by a reranker API.
type RerankResult struct {
	Index          int     `json:"index"`
	Document       string  `json:"document,omitempty"`
	RelevanceScore float64 `json:"relevance_score"`
}

// Reranker calls an external cross-encoder reranker API (e.g. SiliconFlow,
// Voyage, Cohere, Jina) to refine the order of candidate chunks.
type Reranker struct {
	client    *http.Client
	baseURL   string
	apiKey    string
	model     string
	maxDocLen int
}

// NewReranker creates a Reranker. baseURL may be empty to use a sensible
// default when a known model prefix is detected.
func NewReranker(apiKey, model, baseURL string) (*Reranker, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("reranker api key is required")
	}
	if model == "" {
		return nil, fmt.Errorf("reranker model is required")
	}
	if baseURL == "" {
		baseURL = inferRerankBaseURL(model)
	}
	return &Reranker{
		client:    &http.Client{Timeout: 60 * time.Second},
		baseURL:   baseURL,
		apiKey:    apiKey,
		model:     model,
		maxDocLen: 8000,
	}, nil
}

func inferRerankBaseURL(model string) string {
	switch {
	// SiliconFlow hosts many reranker models including BAAI/bge-reranker-v2-m3
	// and Alibaba-NLP/gte-reranker-modernbert-base.
	case hasPrefix(model, "BAAI/"), hasPrefix(model, "Pro/BAAI/"),
		hasPrefix(model, "netease-youdao/"), hasPrefix(model, "Qwen/"),
		hasPrefix(model, "Alibaba-NLP/"):
		return "https://api.siliconflow.cn/v1"
	case hasPrefix(model, "rerank-"):
		// Voyage and Cohere both use the "rerank-" prefix. Default to Voyage;
		// users can override base_url explicitly if they mean Cohere.
		return "https://api.voyageai.com/v1"
	default:
		// OpenAI-compatible rerank endpoint, user can override via config.
		return "https://api.siliconflow.cn/v1"
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}

// Rerank scores the provided documents against query and returns the topN
// results ordered by relevance. Documents should already be the candidate set
// returned by the embedding-based first-stage retrieval.
func (r *Reranker) Rerank(ctx context.Context, query string, documents []string, topN int) ([]RerankResult, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	if topN < 1 {
		topN = 1
	}
	if topN > len(documents) {
		topN = len(documents)
	}

	docs := make([]string, len(documents))
	for i, d := range documents {
		docs[i] = truncate(d, r.maxDocLen)
	}

	body := map[string]any{
		"model":             r.model,
		"query":             query,
		"documents":         docs,
		"top_n":             len(documents), // ask API to score all candidates
		"return_documents":  false,
	}
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal rerank request: %w", err)
	}

	endpoint := r.baseURL + "/rerank"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("create rerank request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	var resp *http.Response
	for attempt := 0; attempt < 3; attempt++ {
		resp, err = r.client.Do(req)
		if err == nil {
			break
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if attempt < 2 {
			time.Sleep(time.Duration(500*(attempt+1)) * time.Millisecond)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("rerank request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read rerank response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("rerank API returned %d: %s", resp.StatusCode, string(respBody))
	}

	var parsed struct {
		Results []RerankResult `json:"results"`
		Data    []RerankResult `json:"data"`
	}
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode rerank response: %w", err)
	}

	results := parsed.Results
	if len(results) == 0 {
		results = parsed.Data
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("rerank API returned empty results")
	}

	// Some APIs may not honour top_n; truncate and sort just in case.
	if len(results) > len(documents) {
		results = results[:len(documents)]
	}
	sortRerankResults(results)

	if topN < len(results) {
		results = results[:topN]
	}
	return results, nil
}

func sortRerankResults(results []RerankResult) {
	for i := 0; i < len(results) && i < len(results); i++ {
		maxIdx := i
		for j := i + 1; j < len(results); j++ {
			if results[j].RelevanceScore > results[maxIdx].RelevanceScore {
				maxIdx = j
			}
		}
		results[i], results[maxIdx] = results[maxIdx], results[i]
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
