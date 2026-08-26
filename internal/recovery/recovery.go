// Package recovery contains the bounded retry policy shared by model-turn
// recovery. Keeping the policy independent makes it easy to evaluate and
// tune without changing the agent loop itself.
package recovery

import (
	"errors"
	"strconv"
	"time"

	"mewcode/internal/llm"
)

const (
	KindUnknown     = "unknown"
	KindRateLimit   = "ratelimit"
	KindNetwork     = "network"
	KindUnavailable = "unavailable"
	KindProtocol    = "protocol"
	KindToolArgs    = "tool_args"
	KindContext     = "context"
)

// Kind returns the recovery category for an error. Authentication and context
// errors intentionally remain non-retryable here: credentials need user action
// and context errors have a dedicated compaction path in Agent.
func Kind(err error) string {
	var e *llm.RateLimitError
	if errors.As(err, &e) {
		return KindRateLimit
	}
	var n *llm.NetworkError
	if errors.As(err, &n) {
		return KindNetwork
	}
	var u *llm.ServiceUnavailableError
	if errors.As(err, &u) {
		return KindUnavailable
	}
	var p *llm.ProtocolError
	if errors.As(err, &p) {
		return KindProtocol
	}
	var a *llm.InvalidToolArgumentsError
	if errors.As(err, &a) {
		return KindToolArgs
	}
	var c *llm.ContextTooLongError
	if errors.As(err, &c) {
		return KindContext
	}
	return KindUnknown
}

// ShouldRetry applies a bounded policy. attempt is zero-based (the first
// failure is attempt 0), and maxAttempts is the total number of retries.
func ShouldRetry(kind string, attempt, maxAttempts int) bool {
	if attempt < 0 || attempt >= maxAttempts || maxAttempts <= 0 {
		return false
	}
	switch kind {
	case KindRateLimit, KindNetwork, KindUnavailable, KindProtocol, KindToolArgs:
		return true
	default:
		return false
	}
}

// Delay returns an exponential backoff capped at maxDelay. Retry-After, when
// present on a rate-limit error, takes precedence and is also capped.
func Delay(kind string, attempt int, retryAfter string, base, maxDelay time.Duration) time.Duration {
	if base <= 0 {
		base = 250 * time.Millisecond
	}
	if maxDelay <= 0 {
		maxDelay = 8 * time.Second
	}
	if kind == KindRateLimit && retryAfter != "" {
		if secs, err := strconv.Atoi(retryAfter); err == nil && secs >= 0 {
			d := time.Duration(secs) * time.Second
			if d > maxDelay {
				return maxDelay
			}
			return d
		}
	}
	if attempt < 0 {
		attempt = 0
	}
	d := base
	for i := 0; i < attempt && d < maxDelay; i++ {
		d *= 2
	}
	if d > maxDelay {
		d = maxDelay
	}
	return d
}
