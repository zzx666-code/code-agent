package tui

import (
	"fmt"
	"strings"
	"testing"
)

func TestRenderChatContentKeepsAllThinkingLines(t *testing.T) {
	lines := make([]string, 135)
	for i := range lines {
		lines[i] = fmt.Sprintf("thinking line %02d", i+1)
	}

	m := Model{
		thinkingBuf:  strings.Join(lines, "\n"),
		thinkingView: true,
	}

	rendered := m.renderChatContent()
	if !strings.Contains(rendered, lines[0]+" ") {
		t.Fatalf("rendered thinking should include first line %q, got %q", lines[0], rendered)
	}
	if !strings.Contains(rendered, lines[len(lines)-1]) {
		t.Fatalf("rendered thinking should include last line %q, got %q", lines[len(lines)-1], rendered)
	}
}
