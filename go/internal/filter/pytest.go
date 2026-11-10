package filter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// pytestFilter keeps failures, errors, warnings, skip reasons, and the final
// summary. Platform banners and passing progress detail are folded. Any
// unrecognized preamble section falls back to raw output.
type pytestFilter struct{ aggressive bool }

func (pytestFilter) ID() string { return "pytest/v1" }

var (
	pytestSummary  = regexp.MustCompile(`^=+ .* in [0-9.]+s(?: \([0-9:]+\))? =+$|^[0-9]+ (?:passed|failed|error|errors|skipped|warning|warnings|no tests ran)\b.* in [0-9.]+s(?: \([0-9:]+\))?$`)
	pytestSection  = regexp.MustCompile(`^=+ (FAILURES|ERRORS|warnings summary|short test summary info|slowest [0-9]+ durations|durations) =+$`)
	pytestProgress = regexp.MustCompile(`^(?:\S.*?)?[.sxXFE]+\s*\[\s*[0-9]+%\]\s*$`)
	pytestVerbose  = regexp.MustCompile(`^\S+::\S+\s+(PASSED|FAILED|ERROR|SKIPPED|XFAIL|XPASS)\b.*\[\s*[0-9]+%\]\s*$`)
)

func (f pytestFilter) Apply(in Input) (Output, error) {
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
	if f.aggressive {
		return renderPytestAggressive(lines, summaryIdx)
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

var pytestCountRE = regexp.MustCompile(`([0-9]+) (passed|failed|error|errors|skipped|warning|warnings|xfailed|xpassed)`)

func renderPytestAggressive(lines []string, summaryIdx int) (Output, error) {
	counts := map[string]int{}
	duration := ""
	summaryRaw := strings.TrimRight(lines[summaryIdx], " ")
	for _, m := range pytestCountRE.FindAllStringSubmatch(summaryRaw, -1) {
		n, _ := strconv.Atoi(m[1])
		key := m[2]
		if key == "errors" {
			key = "error"
		}
		if key == "warnings" {
			key = "warning"
		}
		counts[key] = n
	}
	if i := strings.LastIndex(summaryRaw, " in "); i >= 0 {
		duration = strings.TrimRight(strings.TrimSpace(summaryRaw[i+4:]), "= ")
	}
	summary := fmt.Sprintf("Pytest: %d passed, %d failed", counts["passed"], counts["failed"])
	for _, key := range []string{"skipped", "xfailed", "xpassed", "error", "warning"} {
		if counts[key] > 0 {
			summary += fmt.Sprintf(", %d %s", counts[key], key)
		}
	}
	if duration != "" {
		summary += " in " + duration
	}

	var blocks []string
	var failures []string
	inFailures := false
	current := -1
	for i := 0; i < summaryIdx; i++ {
		line := lines[i]
		trimmed := strings.TrimRight(line, " ")
		if pytestSection.MatchString(trimmed) {
			if current >= 0 {
				blocks = append(blocks, strings.Join(failures, "\n"))
				current = -1
			}
			inFailures = strings.Contains(trimmed, "FAILURES") || strings.Contains(trimmed, "ERRORS")
			continue
		}
		if !inFailures {
			continue
		}
		if isPytestBlockHeader(trimmed) {
			if current >= 0 {
				blocks = append(blocks, strings.Join(failures, "\n"))
			}
			failures = []string{trimmed}
			current = len(failures) - 1
			continue
		}
		if current >= 0 && strings.TrimSpace(line) != "" {
			failures = append(failures, line)
		}
	}
	if current >= 0 {
		blocks = append(blocks, strings.Join(failures, "\n"))
	}

	var out []string
	out = append(out, summary)
	if len(blocks) > 0 {
		out = append(out, "Failures:")
		for i, block := range blocks {
			if i >= 20 {
				out = append(out, fmt.Sprintf("... +%d more failures", len(blocks)-20))
				break
			}
			lines := strings.Split(block, "\n")
			name := strings.Trim(strings.TrimSpace(lines[0]), "_ ")
			label := "FAIL"
			if strings.Contains(strings.ToUpper(name), "ERROR") {
				label = "ERROR"
			}
			out = append(out, fmt.Sprintf("%d. [%s] %s", i+1, label, truncateString(name, 120)))
			var loc, errs, misc []string
			for _, l := range lines[1:] {
				t := strings.TrimSpace(l)
				switch {
				case t == "":
				case strings.Contains(t, ".py:"):
					loc = append(loc, t)
				case strings.HasPrefix(t, "E "):
					errs = append(errs, t)
				case strings.HasPrefix(t, ">") || strings.Contains(strings.ToLower(t), "assert"):
					misc = append(misc, t)
				}
			}
			kept := 0
			add := func(vals []string, max int) {
				for _, t := range vals {
					if kept >= 3 || max <= 0 {
						break
					}
					out = append(out, "   "+truncateString(t, 120))
					kept++
					max--
				}
			}
			add(loc, 1)
			add(errs, 2)
			add(misc, 3-kept)
		}
	}
	// If failures were parsed, the short summary duplicates them; keep it only
	// when there was no failure block (collection errors and the like).
	if len(blocks) == 0 {
		inShort := false
		for i := 0; i < summaryIdx; i++ {
			trimmed := strings.TrimRight(lines[i], " ")
			if pytestSection.MatchString(trimmed) {
				inShort = strings.Contains(trimmed, "short test summary info")
				continue
			}
			if inShort {
				t := strings.TrimSpace(trimmed)
				if strings.HasPrefix(t, "FAILED") || strings.HasPrefix(t, "ERROR") ||
					strings.HasPrefix(t, "SKIPPED") || strings.HasPrefix(t, "XFAIL") || strings.HasPrefix(t, "XPASS") {
					out = append(out, truncateString(t, 160))
				}
			}
		}
	}
	// Warnings summary, capped.
	inWarn := false
	keptWarn := 0
	for i := 0; i < summaryIdx; i++ {
		trimmed := strings.TrimRight(lines[i], " ")
		if pytestSection.MatchString(trimmed) {
			inWarn = strings.Contains(trimmed, "warnings summary")
			continue
		}
		if inWarn && strings.TrimSpace(trimmed) != "" && keptWarn < 8 {
			out = append(out, truncateString(strings.TrimSpace(trimmed), 140))
			keptWarn++
		}
	}
	if counts["failed"]+counts["error"] > 0 && len(blocks) == 0 {
		return Output{}, fmt.Errorf("failure counts without failure blocks")
	}
	if len(out) == 1 && counts["failed"]+counts["error"] == 0 && counts["warning"] == 0 && counts["skipped"] == 0 {
		// Summary-only success.
	}
	return Output{Text: joinLines(out), Lossy: true, RemovedLines: len(lines) - len(out)}, nil
}

func isPytestBlockHeader(line string) bool {
	t := strings.TrimSpace(line)
	if !strings.HasPrefix(t, "_") || !strings.HasSuffix(t, "_") {
		return false
	}
	core := strings.Trim(t, "_ ")
	return core != "" && !strings.HasPrefix(core, "=")
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
