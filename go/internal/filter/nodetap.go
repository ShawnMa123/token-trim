package filter

import (
	"fmt"
	"regexp"
	"strings"
)

// nodeTAPFilter folds successful TAP test blocks and keeps every failing,
// skipped, or todo block plus the summary counters.
type nodeTAPFilter struct{}

func (nodeTAPFilter) ID() string { return "node-tap/v1" }

var (
	tapTestLine = regexp.MustCompile(`^(ok|not ok) ([0-9]+)(?: - .*)?$`)
	tapPlan     = regexp.MustCompile(`^1\.\.([0-9]+)$`)
	tapCounter  = regexp.MustCompile(`^# (tests|suites|pass|fail|cancelled|skipped|todo|duration_ms) [0-9.]+$`)
)

func (nodeTAPFilter) Apply(in Input) (Output, error) {
	lines := splitLines(string(in.Stdout))
	if len(lines) == 0 {
		return Output{}, fmt.Errorf("empty TAP")
	}
	var out []string
	removed := 0
	plan := -1
	tests := 0
	keepBlock := false
	summaryTests := -1
	for _, line := range lines {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			if keepBlock {
				out = append(out, line)
			}
			continue
		}
		keepBlock = false
		if strings.HasPrefix(line, "TAP version ") || strings.HasPrefix(line, "# Subtest: ") {
			removed++
			continue
		}
		if m := tapPlan.FindStringSubmatch(line); m != nil {
			plan = atoi(m[1])
			continue
		}
		if m := tapTestLine.FindStringSubmatch(line); m != nil {
			tests++
			skipOrTodo := strings.Contains(line, "# SKIP") || strings.Contains(line, "# TODO")
			if m[1] == "not ok" || skipOrTodo {
				out = append(out, line)
				keepBlock = true
			} else {
				removed++
			}
			continue
		}
		if strings.HasPrefix(line, "# ") {
			if m := tapCounter.FindStringSubmatch(line); m != nil {
				if m[1] == "tests" {
					summaryTests = atoi(strings.TrimPrefix(line, "# tests "))
				}
				out = append(out, line)
				continue
			}
			return Output{}, fmt.Errorf("unknown TAP comment")
		}
		return Output{}, fmt.Errorf("unrecognized TAP line")
	}
	if plan <= 0 {
		return Output{}, fmt.Errorf("missing TAP plan")
	}
	if tests != plan {
		return Output{}, fmt.Errorf("TAP plan mismatch")
	}
	if summaryTests >= 0 && summaryTests != tests {
		return Output{}, fmt.Errorf("TAP summary mismatch")
	}
	if len(out) == 0 {
		return Output{}, fmt.Errorf("nothing retained")
	}
	return Output{Text: joinLines(out), Lossy: removed > 0, RemovedLines: removed}, nil
}

func atoi(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return -1
		}
		n = n*10 + int(r-'0')
	}
	return n
}
