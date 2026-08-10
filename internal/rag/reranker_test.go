package rag

import (
	"testing"
)

func TestInferRerankBaseURL(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		{"BAAI/bge-reranker-v2-m3", "https://api.siliconflow.cn/v1"},
		{"Pro/BAAI/bge-reranker-v2-m3", "https://api.siliconflow.cn/v1"},
		{"netease-youdao/bce-reranker-base_v1", "https://api.siliconflow.cn/v1"},
		{"Qwen/Qwen3-Reranker-8B", "https://api.siliconflow.cn/v1"},
		{"Alibaba-NLP/gte-reranker-modernbert-base", "https://api.siliconflow.cn/v1"},
		{"rerank-2.5", "https://api.voyageai.com/v1"},
		{"custom-model", "https://api.siliconflow.cn/v1"},
	}
	for _, tc := range tests {
		got := inferRerankBaseURL(tc.model)
		if got != tc.want {
			t.Errorf("inferRerankBaseURL(%q) = %q, want %q", tc.model, got, tc.want)
		}
	}
}

func TestSortRerankResults(t *testing.T) {
	results := []RerankResult{
		{Index: 0, RelevanceScore: 0.1},
		{Index: 1, RelevanceScore: 0.9},
		{Index: 2, RelevanceScore: 0.5},
	}
	sortRerankResults(results)
	if results[0].Index != 1 || results[1].Index != 2 || results[2].Index != 0 {
		t.Fatalf("unexpected order: %+v", results)
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Fatalf("truncate(hello,10) = %q", got)
	}
	if got := truncate("hello world", 5); got != "hello" {
		t.Fatalf("truncate(hello world,5) = %q", got)
	}
}
