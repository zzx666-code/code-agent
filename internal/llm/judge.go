package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"mewcode/internal/conversation"
)

// JudgeCandidate is one candidate document passed to the LLM for re-judgement.
// Index must match the position in the input candidate slice so the caller can
// map the LLM's selection back to the original SearchResult.
type JudgeCandidate struct {
	Index    int    `json:"index"`
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// JudgeResult is the LLM's verdict on a single candidate.
type JudgeResult struct {
	Index    int     `json:"index"`
	Relevant bool    `json:"relevant"`
	Score    float64 `json:"score"`
	Reason   string  `json:"reason"`
}

// judgeSystemPrompt instructs the LLM to act as a relevance judge that
// re-evaluates retrieval candidates against the user's query.
const judgeSystemPrompt = `You are a relevance judge for a code search system.

Given a user query and a list of candidate code chunks (each with file_path and content), your job is to evaluate how relevant each candidate is to answering the query, then return a JSON verdict for EACH candidate.

Rules:
1. Evaluate every candidate - do not skip any.
2. "relevant": true if the chunk contains information that helps answer the query, false otherwise.
3. "score": a relevance score from 0.0 to 1.0 (higher = more relevant).
4. "reason": a short (<=15 words) explanation of your verdict.
5. Return ONLY a JSON object: {"results": [{"index": <int>, "relevant": <bool>, "score": <float>, "reason": "<string>"}, ...]}
6. Do not wrap the JSON in markdown fences or add any prose.`

// JudgeRerankResults asks the LLM to re-evaluate candidate documents against
// the user's query and return a relevance verdict per candidate. It uses the
// given Client directly (no tool loop, no agent) - just one model round-trip.
//
// The returned slice is ordered by the LLM's relevance score (highest first).
// If the LLM call fails or returns unparseable output, an error is returned so
// the caller can fall back to the pre-LLM ranking.
func JudgeRerankResults(ctx context.Context, client Client, query string, candidates []JudgeCandidate) ([]JudgeResult, error) {
	if client == nil {
		return nil, fmt.Errorf("llm judge: client is nil")
	}
	if len(candidates) == 0 {
		return nil, nil
	}

	// Save the original system prompt so we can restore it after the call.
	// We swap in the judge prompt for this single round-trip.
	prevPrompt := getSystemPrompt(client)
	client.SetSystemPrompt(judgeSystemPrompt)
	defer client.SetSystemPrompt(prevPrompt)

	// Build the user message: query + numbered candidate list.
	var sb strings.Builder
	sb.WriteString("User query:\n")
	sb.WriteString(query)
	sb.WriteString("\n\nCandidates:\n")
	for _, c := range candidates {
		fmt.Fprintf(&sb, "[index=%d] file_path=%s\n%s\n---\n", c.Index, c.FilePath, c.Content)
	}
	sb.WriteString("\nEvaluate each candidate against the query. Return JSON: {\"results\": [...]}")

	conv := conversation.NewManager()
	conv.AddUserMessage(sb.String())

	// No tools for this call - we want a plain text (JSON) response.
	events, errCh := client.Stream(ctx, conv, nil)

	var output strings.Builder
	for ev := range events {
		switch e := ev.(type) {
		case TextDelta:
			output.WriteString(e.Text)
		}
	}
	if err := <-errCh; err != nil {
		return nil, fmt.Errorf("llm judge stream: %w", err)
	}

	raw := output.String()
	if raw == "" {
		return nil, fmt.Errorf("llm judge: empty response")
	}

	// The LLM may wrap JSON in ``` fences despite the instruction; strip them.
	raw = stripCodeFences(raw)

	var parsed struct {
		Results []JudgeResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("llm judge: parse response: %w (raw: %s)", err, truncate(raw, 200))
	}
	if len(parsed.Results) == 0 {
		return nil, fmt.Errorf("llm judge: no results in response")
	}

	// Sort by score descending (stable selection sort - small N).
	results := parsed.Results
	for i := 0; i < len(results); i++ {
		maxIdx := i
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[maxIdx].Score {
				maxIdx = j
			}
		}
		results[i], results[maxIdx] = results[maxIdx], results[i]
	}
	return results, nil
}

// getSystemPrompt reads the current system prompt off a Client without needing
// a public accessor on the interface. It works by swapping in a sentinel,
// observing what SetSystemPrompt receives isn't possible (the interface only
// has a setter), so we keep a parallel prompt of our own in a tiny shim.
//
// Simpler: the judge caller doesn't care about restoring an exact prior prompt
// because it owns the client for the duration of the call. But to be safe and
// non-destructive we still restore. We track the prompt via a per-client
// goroutine-local map keyed by pointer. To avoid that complexity, we instead
// require callers that need preservation to pass a fresh client.
//
// In practice the RAG judge path passes the shared agent client; clobbering its
// system prompt would corrupt the main conversation. So we DO need to restore.
// The interface only exposes SetSystemPrompt, so we use a small helper type.
func getSystemPrompt(client Client) string {
	if s, ok := client.(systemPromptGetter); ok {
		return s.GetSystemPrompt()
	}
	return ""
}

type systemPromptGetter interface {
	GetSystemPrompt() string
}

func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Drop the opening fence (and optional language tag on the same line).
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		} else {
			s = strings.TrimPrefix(s, "```")
		}
		s = strings.TrimSpace(s)
	}
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
