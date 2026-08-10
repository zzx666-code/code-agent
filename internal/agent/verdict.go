package agent

import (
	"regexp"
	"strings"
)

type Verdict int

const (
	VerdictUnknown Verdict = iota
	VerdictPass
	VerdictFail
	VerdictPartial
)

func (v Verdict) String() string {
	switch v {
	case VerdictPass:
		return "PASS"
	case VerdictFail:
		return "FAIL"
	case VerdictPartial:
		return "PARTIAL"
	default:
		return "UNKNOWN"
	}
}

var verdictLineRe = regexp.MustCompile(`(?im)^VERDICT:\s*(PASS|FAIL|PARTIAL)\s*$`)

func ParseVerdict(output string) (Verdict, string) {
	matches := verdictLineRe.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return VerdictUnknown, ""
	}
	last := matches[len(matches)-1]
	kind := strings.ToUpper(last[1])
	var v Verdict
	switch kind {
	case "PASS":
		v = VerdictPass
	case "FAIL":
		v = VerdictFail
	case "PARTIAL":
		v = VerdictPartial
	default:
		return VerdictUnknown, ""
	}
	return v, extractEvidence(output)
}

func extractEvidence(output string) string {
	lines := strings.Split(output, "\n")
	var evidence []string
	inCheckBlock := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "### Check:") {
			inCheckBlock = true
			evidence = append(evidence, line)
			continue
		}
		if inCheckBlock {
			if strings.HasPrefix(trim, "### Check:") {
				evidence = append(evidence, line)
				continue
			}
			if strings.HasPrefix(trim, "**Result:") || strings.HasPrefix(trim, "Result:") {
				evidence = append(evidence, line)
				inCheckBlock = false
				continue
			}
			if strings.HasPrefix(trim, "**Command run:") || strings.HasPrefix(trim, "**Output observed:") {
				evidence = append(evidence, line)
			}
		}
	}
	if len(evidence) == 0 {
		idx := verdictLineRe.FindStringIndex(output)
		if idx != nil && idx[0] > 0 {
			return strings.TrimSpace(output[max(0, idx[0]-500):idx[0]])
		}
		return truncStr(strings.TrimSpace(output), 500)
	}
	joined := strings.Join(evidence, "\n")
	return truncStr(joined, 800)
}

func truncStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
