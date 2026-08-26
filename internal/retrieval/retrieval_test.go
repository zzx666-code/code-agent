package retrieval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureIndex(t *testing.T) *Index {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"internal/orders/service.go":    "package orders\n\nimport \"context\"\n\ntype Service struct{}\n\nfunc (s *Service) CreateOrder(ctx context.Context, id string) error { return nil }\n\nfunc (s *Service) CancelOrder(id string) error { return nil }\n",
		"internal/orders/repository.go": "package orders\n\ntype Repository struct{}\n\nfunc (r *Repository) FindOrder(id string) (string, error) { return id, nil }\n",
		"internal/users/handler.go":     "package users\n\n// HandleLogin authenticates a user.\nfunc HandleLogin(token string) error { return nil }\n",
		"README.md":                     "# Fixture\nThis documents order handling.\n",
	}
	for name, content := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	idx, err := Build(root)
	if err != nil {
		t.Fatal(err)
	}
	return idx
}

func TestBuildExtractsSymbolsAndDirectoryTree(t *testing.T) {
	idx := fixtureIndex(t)
	if len(idx.Symbols()) < 5 {
		t.Fatalf("expected symbols, got %d", len(idx.Symbols()))
	}
	dirs := idx.Directories()
	if len(dirs) == 0 {
		t.Fatal("expected directory entries")
	}
	foundOrders := false
	for _, d := range dirs {
		if strings.Contains(filepath.ToSlash(d.Path), "internal/orders") {
			foundOrders = true
		}
	}
	if !foundOrders {
		t.Fatalf("directory tree missing orders: %#v", dirs)
	}
	results := idx.SearchDirectories("orders", 3)
	if len(results) == 0 || !strings.Contains(results[0].Directory.Path, "orders") {
		t.Fatalf("directory search miss: %#v", results)
	}
}

func TestSearchSymbolAndContextStitching(t *testing.T) {
	idx := fixtureIndex(t)
	got := idx.Search("CreateOrder", SearchOptions{Mode: ModeSymbol, TopK: 5, ContextLines: 2})
	if len(got) == 0 || got[0].Symbol == nil || got[0].Symbol.Name != "CreateOrder" {
		t.Fatalf("symbol search miss: %#v", got)
	}
	if !strings.Contains(got[0].Context, "package orders") || !strings.Contains(got[0].Context, "CreateOrder") {
		t.Fatalf("context not stitched: %q", got[0].Context)
	}
}

func TestKeywordAndHybridSearch(t *testing.T) {
	idx := fixtureIndex(t)
	keyword := idx.Search("order handling", SearchOptions{Mode: ModeKeyword, TopK: 5})
	hybrid := idx.Search("cancel order", SearchOptions{Mode: ModeHybrid, TopK: 5})
	if len(keyword) == 0 || len(hybrid) == 0 {
		t.Fatalf("expected search results")
	}
	if hybrid[0].Symbol == nil || hybrid[0].Symbol.Name != "CancelOrder" {
		t.Fatalf("hybrid ranking miss: %#v", hybrid[0])
	}
}

func TestOfflineEvaluationHasThirtyQueriesAndComparison(t *testing.T) {
	idx := fixtureIndex(t)
	queries := DefaultEvalQueries(idx.Symbols())
	if len(queries) < 30 {
		t.Fatalf("expected at least 30 queries, got %d", len(queries))
	}
	report := Evaluate(idx, queries, 5)
	t.Logf("offline retrieval report: keyword=%.1f%% symbol=%.1f%% hybrid=%.1f%% latency(ms): %.3f/%.3f/%.3f", report.Keyword.Top5Accuracy, report.Symbol.Top5Accuracy, report.Hybrid.Top5Accuracy, report.Keyword.AvgLatencyMs, report.Symbol.AvgLatencyMs, report.Hybrid.AvgLatencyMs)
	if report.Queries != len(queries) || report.Keyword.Queries != len(queries) || report.Symbol.Queries != len(queries) {
		t.Fatalf("invalid report: %#v", report)
	}
	if report.Symbol.Top5Accuracy <= 0 {
		t.Fatalf("symbol baseline should hit fixture symbols: %#v", report)
	}
}
