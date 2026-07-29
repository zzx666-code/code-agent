package rag

import (
	"context"
	"fmt"
	"time"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"mewcode/internal/config"
)

type Embedder struct {
	client *openai.Client
	model  string
}

func NewEmbedder(cfg *config.ProviderConfig) (*Embedder, error) {
	if cfg.EmbeddingModel == "" {
		return nil, fmt.Errorf("embedding_model not configured; set it in .mewcode/config.yaml (e.g. embedding_model: doubao-embedding-vision)")
	}
	baseURL := cfg.EmbeddingURL
	if baseURL == "" {
		baseURL = cfg.BaseURL
	}
	apiKey := cfg.ResolveEmbeddingAPIKey()
	if apiKey == "" {
		return nil, fmt.Errorf("API key not found for embedding; set embedding_api_key or api_key in config")
	}
	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL(baseURL),
	)
	return &Embedder{client: &client, model: cfg.EmbeddingModel}, nil
}

func (e *Embedder) Model() string { return e.model }

func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, int, error) {
	if len(texts) == 0 {
		return nil, 0, nil
	}
	const batchSize = 10
	var all [][]float32
	var dim int
	for start := 0; start < len(texts); start += batchSize {
		end := start + batchSize
		if end > len(texts) {
			end = len(texts)
		}
		batch := texts[start:end]

		var resp *openai.CreateEmbeddingResponse
		var err error
		for attempt := 0; attempt < 3; attempt++ {
			resp, err = e.client.Embeddings.New(ctx, openai.EmbeddingNewParams{
				Model: openai.EmbeddingModel(e.model),
				Input: openai.EmbeddingNewParamsInputUnion{
					OfArrayOfStrings: batch,
				},
			})
			if err == nil {
				break
			}
			// 429 限流时退避重试
			if attempt < 2 {
				backoff := time.Duration(1500*(attempt+1)) * time.Millisecond
				select {
				case <-ctx.Done():
					return nil, 0, ctx.Err()
				case <-time.After(backoff):
				}
			}
		}
		if err != nil {
			return nil, 0, fmt.Errorf("embedding API call failed: %w", err)
		}
		for _, d := range resp.Data {
			vec := make([]float32, len(d.Embedding))
			for i, v := range d.Embedding {
				vec[i] = float32(v)
			}
			if dim == 0 {
				dim = len(vec)
			}
			all = append(all, vec)
		}
		// 批次间限速，避免触发 API 限流
		if end < len(texts) {
			select {
			case <-ctx.Done():
				return nil, 0, ctx.Err()
			case <-time.After(800 * time.Millisecond):
			}
		}
	}
	return all, dim, nil
}

func (e *Embedder) EmbedOne(ctx context.Context, text string) ([]float32, int, error) {
	vecs, dim, err := e.Embed(ctx, []string{text})
	if err != nil || len(vecs) == 0 {
		return nil, 0, err
	}
	return vecs[0], dim, nil
}
