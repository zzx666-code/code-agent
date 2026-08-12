package llm

import (
	"context"
	"errors"
	"time"

	"mewcode/internal/conversation"
	"mewcode/internal/metrics"
)

type MeteredClient struct {
	inner    Client
	metrics  *metrics.Metrics
	provider string
	model    string
}

func NewMeteredClient(inner Client, m *metrics.Metrics, provider, model string) *MeteredClient {
	if m == nil {
		m = metrics.Noop
	}
	return &MeteredClient{inner: inner, metrics: m, provider: provider, model: model}
}

func (c *MeteredClient) SetSystemPrompt(prompt string) {
	c.inner.SetSystemPrompt(prompt)
}

func (c *MeteredClient) Stream(ctx context.Context, conv *conversation.Manager, tools []map[string]any) (<-chan StreamEvent, <-chan error) {
	start := time.Now()
	events, errs := c.inner.Stream(ctx, conv, tools)

	wrappedEvents := make(chan StreamEvent, 32)
	wrappedErrs := make(chan error, 1)

	go func() {
		defer close(wrappedEvents)
		defer close(wrappedErrs)

		var usage UsageInfo
		for ev := range events {
			if se, ok := ev.(StreamEnd); ok {
				usage = se.Usage
			}
			wrappedEvents <- ev
		}

		select {
		case err := <-errs:
			if err != nil {
				status := ClassifyErrorStatus(err)
				if status != "ok" {
					c.metrics.RecordLLMRetry(status)
					c.metrics.RecordLLMError(status)
				}
				c.metrics.RecordLLMCall(c.provider, c.model, status)
				c.metrics.ObserveLLMLatency(c.provider, c.model, time.Since(start))
				wrappedErrs <- err
				return
			}
		default:
		}

		c.metrics.RecordLLMCall(c.provider, c.model, "ok")
		c.metrics.ObserveLLMLatency(c.provider, c.model, time.Since(start))
		c.metrics.RecordTokenUsage(c.provider, c.model,
			usage.InputTokens, usage.OutputTokens,
			usage.CacheReadTokens, usage.CacheCreationTokens)
		cost := computeCost(c.provider, usage)
		c.metrics.RecordLLMCost(c.provider, c.model, cost)
	}()

	return wrappedEvents, wrappedErrs
}

type tokenPricing struct {
	inputPerMTok       float64
	outputPerMTok      float64
	cacheReadPerMTok   float64
	cacheCreatePerMTok float64
}

var defaultPricing = map[string]tokenPricing{
	"anthropic":     {3.0, 15.0, 0.30, 3.75},
	"openai":        {2.50, 10.0, 1.25, 0},
	"openai-compat": {1.0, 2.0, 0, 0},
}

func computeCost(provider string, usage UsageInfo) float64 {
	p, ok := defaultPricing[provider]
	if !ok {
		p = defaultPricing["openai-compat"]
	}
	mtok := 1_000_000.0
	cost := float64(usage.InputTokens)/mtok*p.inputPerMTok +
		float64(usage.OutputTokens)/mtok*p.outputPerMTok +
		float64(usage.CacheReadTokens)/mtok*p.cacheReadPerMTok +
		float64(usage.CacheCreationTokens)/mtok*p.cacheCreatePerMTok
	return cost
}

func ClassifyErrorStatus(err error) string {
	var authErr *AuthenticationError
	if errors.As(err, &authErr) {
		return "auth"
	}
	var rlErr *RateLimitError
	if errors.As(err, &rlErr) {
		return "ratelimit"
	}
	var netErr *NetworkError
	if errors.As(err, &netErr) {
		return "network"
	}
	var ctxErr *ContextTooLongError
	if errors.As(err, &ctxErr) {
		return "context"
	}
	var llmErr *LLMError
	if errors.As(err, &llmErr) {
		return "server"
	}
	return "unknown"
}
