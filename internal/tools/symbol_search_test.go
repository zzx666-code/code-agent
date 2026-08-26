package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSymbolSearchToolReturnsContext(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "handler.go")
	if err := os.WriteFile(path, []byte("package demo\n\nfunc CreateOrder() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	result := (&SymbolSearchTool{Root: root}).Execute(context.Background(), map[string]any{"query": "CreateOrder"})
	if result.IsError || !strings.Contains(result.Output, "CreateOrder") || !strings.Contains(result.Output, "package demo") {
		t.Fatalf("unexpected symbol search result: %+v", result)
	}
}
