package compact

import "testing"

func TestParseCompactionStats(t *testing.T) {
	before, after, ok := ParseCompactionStats("Compacted: 12000 → 4500 estimated tokens")
	if !ok || before != 12000 || after != 4500 {
		t.Fatalf("stats = %d, %d, %v", before, after, ok)
	}
	if _, _, ok := ParseCompactionStats("no compaction"); ok {
		t.Fatal("non-stats message should not parse")
	}
}
