package agent

import (
	"strings"
	"testing"
)

func TestParseVerdict_Pass(t *testing.T) {
	out := "### Check: build\n**Command run:**\n  go build ./...\n**Output observed:**\n  ok\n**Result: PASS**\n\nVERDICT: PASS"
	v, ev := ParseVerdict(out)
	if v != VerdictPass {
		t.Fatalf("verdict = %v, want PASS", v)
	}
	if !strings.Contains(ev, "build") {
		t.Errorf("evidence missing check title: %q", ev)
	}
}

func TestParseVerdict_Fail(t *testing.T) {
	out := "### Check: tests\n**Command run:**\n  go test ./...\n**Output observed:**\n  FAIL\n**Result: FAIL**\n\nVERDICT: FAIL"
	v, _ := ParseVerdict(out)
	if v != VerdictFail {
		t.Fatalf("verdict = %v, want FAIL", v)
	}
}

func TestParseVerdict_Partial(t *testing.T) {
	out := "VERDICT: PARTIAL"
	v, _ := ParseVerdict(out)
	if v != VerdictPartial {
		t.Fatalf("verdict = %v, want PARTIAL", v)
	}
}

func TestParseVerdict_None(t *testing.T) {
	v, ev := ParseVerdict("no verdict here")
	if v != VerdictUnknown {
		t.Fatalf("verdict = %v, want UNKNOWN", v)
	}
	if ev != "" {
		t.Errorf("evidence should be empty, got %q", ev)
	}
}

func TestParseVerdict_MultipleTakesLast(t *testing.T) {
	out := "VERDICT: PASS\nsome text\nVERDICT: FAIL\n"
	v, _ := ParseVerdict(out)
	if v != VerdictFail {
		t.Fatalf("verdict = %v, want FAIL (last wins)", v)
	}
}

func TestParseVerdict_CaseInsensitive(t *testing.T) {
	out := "verdict: pass"
	v, _ := ParseVerdict(out)
	if v != VerdictPass {
		t.Fatalf("verdict = %v, want PASS", v)
	}
}

func TestParseVerdict_TrailingSpaces(t *testing.T) {
	out := "VERDICT: PASS   \n"
	v, _ := ParseVerdict(out)
	if v != VerdictPass {
		t.Fatalf("verdict = %v, want PASS", v)
	}
}

func TestVerdictString(t *testing.T) {
	cases := map[Verdict]string{
		VerdictPass:    "PASS",
		VerdictFail:    "FAIL",
		VerdictPartial: "PARTIAL",
		VerdictUnknown: "UNKNOWN",
	}
	for v, want := range cases {
		if got := v.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", v, got, want)
		}
	}
}

func TestParseVerdict_EvidenceFallback(t *testing.T) {
	out := "some preamble\na line with detail\nVERDICT: FAIL"
	v, ev := ParseVerdict(out)
	if v != VerdictFail {
		t.Fatalf("verdict = %v, want FAIL", v)
	}
	if ev == "" {
		t.Error("evidence should be non-empty via fallback")
	}
}
