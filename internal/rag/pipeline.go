package rag

import (
	"context"
	"fmt"
)

// CoarseRetrieve performs stage-1 vector-similarity retrieval. It embeds the
// query and asks the Store for up to candidateCount candidates, ordered by
// cosine similarity. This is the "粗排" stage, run by subagent A.
//
// Returns the candidate slice (already sorted by embedding score, highest
// first) or an error if embedding or store lookup fails.
func CoarseRetrieve(ctx context.Context, store *Store, embedder *Embedder, query string, candidateCount int) ([]SearchResult, error) {
	if store == nil || embedder == nil {
		return nil, fmt.Errorf("coarse retrieve: store and embedder are required")
	}
	if candidateCount < 1 {
		candidateCount = 50
	}
	queryVec, _, err := embedder.EmbedOne(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("coarse retrieve: embed query: %w", err)
	}
	results, err := store.Search(ctx, queryVec, candidateCount)
	if err != nil {
		return nil, fmt.Errorf("coarse retrieve: store search: %w", err)
	}
	return results, nil
}

// RerankPass performs stage-2 cross-encoder reranking. It takes the coarse
// candidates from stage 1 and asks the reranker to re-score them against the
// query, returning the top topN results ordered by reranker score. This is
// the "精排" stage, run by subagent B.
//
// If reranker is nil, the coarse candidates are returned unchanged (truncated
// to topN) - the caller (LLM judge stage) will then decide final relevance.
func RerankPass(ctx context.Context, reranker *Reranker, query string, coarse []SearchResult, topN int) ([]SearchResult, error) {
	if topN < 1 {
		topN = 5
	}
	if len(coarse) == 0 {
		return nil, nil
	}
	if reranker == nil {
		if len(coarse) > topN {
			coarse = coarse[:topN]
		}
		return coarse, nil
	}

	docs := make([]string, len(coarse))
	for i, r := range coarse {
		docs[i] = r.Content
	}
	rerankResults, err := reranker.Rerank(ctx, query, docs, topN)
	if err != nil {
		// Caller decides whether to fall back; we surface the error.
		return nil, fmt.Errorf("rerank pass: %w", err)
	}

	reranked := make([]SearchResult, 0, len(rerankResults))
	for _, rr := range rerankResults {
		if rr.Index < 0 || rr.Index >= len(coarse) {
			continue
		}
		candidate := coarse[rr.Index]
		candidate.Score = float32(rr.RelevanceScore)
		reranked = append(reranked, candidate)
	}
	if len(reranked) == 0 {
		// Reranker returned nothing usable - fall back to coarse order.
		if len(coarse) > topN {
			coarse = coarse[:topN]
		}
		return coarse, nil
	}
	return reranked, nil
}
