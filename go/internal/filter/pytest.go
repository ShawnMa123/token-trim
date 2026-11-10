package filter

import (
	"fmt"
	"regexp"
	"strings"
)

// pytestFilter keeps failures, errors, warnings, skip reasons, and the final
// summary. Platform banners and passing progress detail are folded. Any
// unrecognized preamble section falls back to raw output.
type pytestFilter struct{}

func (pytestFilter) ID() string { return "pytest/v1" }

var (
	pytestSummary  = regexp.MustCompile(`^=+ .* in [0-9.]+s(?: \([0-9:]+\))? =+$|^[0-9]+ (?:passed|failed|error|errors|skipped|warning|warnings|no tests ran)\b.* in [0-9.]+s(?: \([0-9:]+\))?$`)
	pytestSection  = regexp.MustCompile(`^=+ (FAILURES|ERRORS|warnings summary|short test summary info|slowest [0-9]+ durations|durations) =+$`)
	pytestProgress = regexp.MustCompile(`^(?:\S.*?)?[.sxXFE]+\s*\[\s*[0-9]+%\]\s*$`)
	pytestVerbose  = regexp.MustCompile(`^\S+::\S+\s+(PASSED|FAILED|ERROR|SKIPPED|XFAIL|XPASS)\b.*\[\s*[0-9]+%\]\s*$`)
)

func (pytestFilter) Apply(in Input) (Output, error) {
	lines := splitLines(string(in.Stdout))
	if len(lines) == 0 {
		return Output{}, fmt.Errorf("empty pytest output")
	}
	summaryIdx := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if pytestSummary.MatchString(strings.TrimRight(lines[i], " ")) {
			summaryIdx = i
			break
		}
	}
	if summaryIdx < 0 {
		return Output{}, fmt.Errorf("no final summary")
	}

	var out []string
	removed := 0
	keptSection := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimRight(line, " ")
		if keptSection {
			out = append(out, line)
			continue
		}
		if pytestSection.MatchString(trimmed) {
			keptSection = true
			out = append(out, line)
			continue
		}
		if trimmed == "" {
			continue
		}
		if i == summaryIdx {
			out = append(out, line)
			continue
		}
		if isPytestPreamble(trimmed) {
			if trimmed != "" {
				removed++
			}
			continue
		}
		if m := pytestVerbose.FindStringSubmatch(trimmed); m != nil {
			if m[1] == "FAILED" || m[1] == "ERROR" {
				out = append(out, line)
				continue
			}
			removed++
			continue
		}
		return Output{}, fmt.Errorf("unrecognized pytest preamble line")
	}
	if len(out) == 0 {
		return Output{}, fmt.Errorf("nothing retained")
	}
	return Output{Text: joinLines(out), Lossy: removed > 0, RemovedLines: removed}, nil
}

func isPytestPreamble(line string) bool {
	prefixes := []string{
		"platform ", "rootdir:", "configfile:", "plugins:", "collected ", "cachedir:",
	}
	for _, p := range prefixes {
		if strings.HasPrefix(line, p) {
			return true
		}
	}
	if strings.Contains(line, "test session starts") || strings.Contains(line, "Test session starts") {
		return true
	}
	if pytestProgress.MatchString(line) {
		return true
	}
	return false
}
