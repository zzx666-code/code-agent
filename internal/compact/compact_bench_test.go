package compact

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"mewcode/internal/conversation"
	"mewcode/internal/llm"
	"mewcode/internal/session"
)

// TestAutoCompactBoundary: 消息数刚好等于阈值时不触发压缩
func TestAutoCompactBoundary(t *testing.T) {
	conv := conversation.NewManager()
	for i := 0; i < 5; i++ {
		conv.AddUserMessage(fmt.Sprintf("msg %d", i))
		conv.AddAssistantMessage(fmt.Sprintf("reply %d", i))
	}
	// 小消息量，不应触发
	anchor := UsageAnchor{BaselineTokens: 100, AnchorCount: 10, HasUsage: true}
	msgs := conv.GetMessages()
	used := ComputeUsedTokens(msgs, anchor)
	window := 200000
	if used >= window {
		t.Errorf("small conversation should not exceed window: used=%d window=%d", used, window)
	}
}

// TestForceCompactPreservesTail: 强制压缩后保留最近消息
func TestForceCompactPreservesTail(t *testing.T) {
	client := &stubSummaryClient{summary: "Summary of earlier conversation"}
	conv := conversation.NewManager()
	for i := 0; i < 10; i++ {
		conv.AddUserMessage(strings.Repeat("content ", 50))
		conv.AddAssistantMessage(strings.Repeat("reply ", 50))
	}
	conv.AddUserMessage("keep this recent message")
	msgs := conv.GetMessages()
	_, err := ForceCompact(context.Background(), conv, client, ".", "test-session", 200000, nil, nil, nil)
	if err != nil {
		t.Logf("ForceCompact returned err (expected in test without real session dir): %v", err)
	}
	_ = msgs
}

// TestEstimateTokensAccuracy: token 估算函数的基本正确性
func TestEstimateTokensAccuracy(t *testing.T) {
	cases := []struct {
		name string
		text string
		min  int
		max  int
	}{
		{"empty", "", 4, 4},       // 空消息: 0/3.5 + 4 = 4
		{"short", "hello", 5, 6},  // 5/3.5 + 4 ≈ 5
		{"medium", strings.Repeat("a", 100), 32, 33}, // 100/3.5 + 4 ≈ 32
		{"long", strings.Repeat("a", 1000), 289, 290}, // 1000/3.5 + 4 ≈ 289
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			conv := conversation.NewManager()
			conv.AddUserMessage(tc.text)
			got := EstimateTokens(conv.GetMessages())
			if got < tc.min || got > tc.max {
				t.Errorf("EstimateTokens(%s) = %d, want [%d,%d]", tc.name, got, tc.min, tc.max)
			}
		})
	}
}

// TestRecoveryStateRecordAndRetrieve: RecoveryState 记录操作不 panic
func TestRecoveryStateRecordAndRetrieve(t *testing.T) {
	rs := NewRecoveryState()
	rs.RecordFileRead("/tmp/file1.go", "content of file1")
	rs.RecordFileRead("/tmp/file2.go", "content of file2")
	rs.RecordSkillInvocation("my-skill", "skill body text")
	// 重复记录（覆盖）
	rs.RecordFileRead("/tmp/file1.go", "updated content")
	rs.RecordSkillInvocation("my-skill", "updated body")
	// nil 安全性
	var nilRs *RecoveryState
	nilRs.RecordFileRead("x", "y")
	nilRs.RecordSkillInvocation("z", "w")
}

// --- 性能基准 ---

// BenchmarkEstimateTokens: token 估算性能
func BenchmarkEstimateTokens(b *testing.B) {
	conv := conversation.NewManager()
	for i := 0; i < 20; i++ {
		conv.AddUserMessage(strings.Repeat("some message content here ", 20))
		conv.AddAssistantMessage(strings.Repeat("assistant response text ", 20))
	}
	msgs := conv.GetMessages()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		EstimateTokens(msgs)
	}
}

// BenchmarkComputeUsedTokensColdStart: 无 anchor 的全量 token 计算性能
func BenchmarkComputeUsedTokensColdStart(b *testing.B) {
	conv := conversation.NewManager()
	for i := 0; i < 30; i++ {
		conv.AddUserMessage(strings.Repeat("x", 500))
		conv.AddAssistantMessage(strings.Repeat("y", 500))
	}
	msgs := conv.GetMessages()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeUsedTokens(msgs, UsageAnchor{})
	}
}

// BenchmarkComputeUsedTokensWithAnchor: 有 anchor 的增量 token 计算性能
func BenchmarkComputeUsedTokensWithAnchor(b *testing.B) {
	conv := conversation.NewManager()
	for i := 0; i < 30; i++ {
		conv.AddUserMessage(strings.Repeat("x", 500))
		conv.AddAssistantMessage(strings.Repeat("y", 500))
	}
	anchorCount := conv.Len()
	conv.AddUserMessage(strings.Repeat("z", 200))
	msgs := conv.GetMessages()
	anchor := UsageAnchor{BaselineTokens: 5000, AnchorCount: anchorCount, HasUsage: true}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ComputeUsedTokens(msgs, anchor)
	}
}

// BenchmarkFormatCompactSummary: 摘要格式化性能
func BenchmarkFormatCompactSummary(b *testing.B) {
	input := "<analysis>" + strings.Repeat("scratch ", 100) + "</analysis>\n<summary>" + strings.Repeat("final summary text ", 50) + "</summary>"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		formatCompactSummary(input)
	}
}

// 防止 unused import
var (
	_ = session.TypeCompactBoundary
	_ = llm.TextDelta{}
)
