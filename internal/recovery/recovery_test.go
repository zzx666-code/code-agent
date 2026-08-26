package recovery

import (
	"testing"
	"time"

	"mewcode/internal/llm"
)

func TestShouldRetryIsBounded(t *testing.T) {
	if !ShouldRetry(KindNetwork, 0, 3) || !ShouldRetry(KindProtocol, 2, 3) {
		t.Fatal("expected transient failures to be retryable")
	}
	if ShouldRetry(KindNetwork, 3, 3) || ShouldRetry(KindUnknown, 0, 3) || ShouldRetry("auth", 0, 3) {
		t.Fatal("non-retryable or exhausted failures must stop")
	}
}

func TestDelayUsesRetryAfterAndCaps(t *testing.T) {
	if got := Delay(KindRateLimit, 0, "3", time.Second, 2*time.Second); got != 2*time.Second {
		t.Fatalf("retry-after cap = %s", got)
	}
	if got := Delay(KindNetwork, 2, "", 100*time.Millisecond, time.Second); got != 400*time.Millisecond {
		t.Fatalf("backoff = %s", got)
	}
}

func TestKind(t *testing.T) {
	if got := Kind(&llm.ServiceUnavailableError{Message: "down"}); got != KindUnavailable {
		t.Fatalf("kind = %q", got)
	}
}
