package filter

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// goTestFilter folds successful test detail. Any failure signal, nonzero exit,
// panic, race, or unknown event stream falls back to the original output.
type goTestFilter struct {
	json       bool
	aggressive bool
}

func (f goTestFilter) ID() string {
	if f.json {
		return "gotest-json/v1"
	}
	return "gotest/v1"
}

func (f goTestFilter) Apply(in Input) (Output, error) {
	if f.json {
		return applyGoTestJSON(in, f.aggressive)
	}
	return applyGoTestText(in, f.aggressive)
}

func applyGoTestText(in Input, aggressive bool) (Output, error) {
	if in.ExitCode != 0 {
		if !aggressive {
			return Output{}, fmt.Errorf("nonzero exit")
		}
		return applyGoTestFailures(in)
	}
	text := string(in.Stdout)
	for _, marker := range []string{"--- FAIL", "panic:", "DATA RACE", "fatal error:", "build failed"} {
		if strings.Contains(text, marker) {
			return Output{}, fmt.Errorf("failure marker %q", marker)
		}
	}
	lines := splitLines(text)
	if aggressive {
		return renderGoTestSuccessSummary(lines)
	}
	var out []string
	removed := 0
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "=== RUN   "),
			strings.HasPrefix(line, "=== PAUSE "),
			strings.HasPrefix(line, "=== CONT  "),
			strings.HasPrefix(line, "=== NAME  "),
			strings.HasPrefix(line, "--- PASS: "):
			removed++
			continue
		default:
			out = append(out, line)
		}
	}
	if removed == 0 {
		return Output{}, fmt.Errorf("nothing to fold")
	}
	return Output{Text: joinLines(out), Lossy: true, RemovedLines: removed}, nil
}

type goTestEvent struct {
	Action  string  `json:"Action"`
	Package string  `json:"Package"`
	Test    string  `json:"Test"`
	Elapsed float64 `json:"Elapsed"`
	Output  string  `json:"Output"`
}

var knownGoActions = map[string]bool{
	"start": true, "run": true, "pause": true, "cont": true,
	"pass": true, "fail": true, "skip": true, "output": true,
}

func applyGoTestJSON(in Input, aggressive bool) (Output, error) {
	if in.ExitCode != 0 && !aggressive {
		return Output{}, fmt.Errorf("nonzero exit")
	}
	lines := splitLines(string(in.Stdout))
	if len(lines) == 0 {
		return Output{}, fmt.Errorf("empty event stream")
	}
	type skipInfo struct {
		pkg, test string
		output    []string
	}
	var order []string
	seen := map[string]bool{}
	finalPass := map[string]bool{}
	elapsed := map[string]float64{}
	noTests := map[string]bool{}
	testOutput := map[string][]string{}
	var skips []skipInfo
	skipSeen := map[string]bool{}
	hidden := 0
	var failed []goTestEvent

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var e goTestEvent
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			return Output{}, fmt.Errorf("invalid JSON event")
		}
		if e.Action == "" || e.Package == "" || !knownGoActions[e.Action] {
			return Output{}, fmt.Errorf("unknown event")
		}
		if e.Action == "fail" {
			failed = append(failed, e)
		}
		if !seen[e.Package] {
			seen[e.Package] = true
			order = append(order, e.Package)
		}
		key := e.Package + "\x00" + e.Test
		switch e.Action {
		case "fail":
			if !aggressive {
				return Output{}, fmt.Errorf("failed test event")
			}
		case "run":
			if e.Test != "" {
				hidden++
			}
		case "pause", "cont":
		case "pass":
			if e.Test == "" {
				finalPass[e.Package] = true
				elapsed[e.Package] = e.Elapsed
			} else {
				hidden++
			}
		case "skip":
			if e.Test == "" {
				noTests[e.Package] = true
				finalPass[e.Package] = true
				elapsed[e.Package] = e.Elapsed
			} else {
				if !skipSeen[key] {
					skipSeen[key] = true
					skips = append(skips, skipInfo{pkg: e.Package, test: e.Test})
				}
			}
		case "output":
			if e.Test == "" {
				trimmed := strings.TrimSpace(e.Output)
				if trimmed == "" || trimmed == "PASS" || strings.HasPrefix(trimmed, "ok  ") || strings.HasPrefix(trimmed, "?\t") {
					continue
				}
				return Output{}, fmt.Errorf("unexpected package output")
			}
			testOutput[key] = append(testOutput[key], strings.TrimRight(e.Output, "\r\n"))
			hidden++
		case "start":
		}
	}
	if len(order) == 0 {
		return Output{}, fmt.Errorf("no packages")
	}
	if len(failed) > 0 {
		return renderGoTestJSONFailures(order, failed, testOutput)
	}
	for _, pkg := range order {
		if !finalPass[pkg] {
			return Output{}, fmt.Errorf("missing terminal event for %s", pkg)
		}
	}
	var out []string
	for _, pkg := range order {
		if noTests[pkg] {
			out = append(out, "?   "+pkg+"\t[no test files]")
			continue
		}
		out = append(out, "ok  "+pkg+"  "+formatSeconds(elapsed[pkg]))
	}
	for _, s := range skips {
		out = append(out, "skip "+s.pkg+" "+s.test)
		for _, l := range testOutput[s.pkg+"\x00"+s.test] {
			if strings.TrimSpace(l) != "" {
				out = append(out, l)
			}
		}
	}
	if hidden == 0 {
		return Output{}, fmt.Errorf("nothing to fold")
	}
	return Output{Text: joinLines(out), Lossy: true, RemovedLines: hidden}, nil
}

func renderGoTestSuccessSummary(lines []string) (Output, error) {
	passed := 0
	packages := 0
	removed := 0
	var out []string
	keepIndented := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "--- PASS:"):
			passed++
			keepIndented = false
			removed++
		case strings.HasPrefix(line, "=== RUN"), strings.HasPrefix(line, "=== PAUSE"),
			strings.HasPrefix(line, "=== CONT"), strings.HasPrefix(line, "=== NAME"):
			keepIndented = false
			removed++
		case strings.HasPrefix(line, "ok  \t"):
			packages++
			removed++
		case line == "PASS":
			removed++
		case strings.HasPrefix(trimmed, "--- SKIP:"):
			keepIndented = true
			out = append(out, line)
		case strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t"):
			if keepIndented {
				out = append(out, line)
			} else {
				removed++
			}
		default:
			keepIndented = false
			out = append(out, line)
		}
	}
	if passed == 0 {
		return Output{}, fmt.Errorf("no passing test detail")
	}
	head := fmt.Sprintf("Go test: %d passed in %d packages", passed, packages)
	out = append([]string{head}, out...)
	return Output{Text: joinLines(out), Lossy: true, RemovedLines: removed}, nil
}

func applyGoTestFailures(in Input) (Output, error) {
	text := string(in.Stdout)
	if strings.Contains(text, "panic:") || strings.Contains(text, "DATA RACE") || strings.Contains(text, "fatal error:") {
		return Output{}, fmt.Errorf("panic or race")
	}
	lines := splitLines(text)
	type block struct{ start, end int }
	var blocks []block
	start := -1
	flush := func(end int) {
		if start >= 0 {
			blocks = append(blocks, block{start, end})
			start = -1
		}
	}
	for i, line := range lines {
		if strings.HasPrefix(line, "=== RUN") {
			flush(i)
			start = i
		} else if isGoPackageSummary(line) {
			flush(i)
		}
	}
	flush(len(lines))

	used := make([]bool, len(lines))
	var out []string
	removed := 0
	failures := 0
	for _, b := range blocks {
		failed := false
		for i := b.start; i < b.end; i++ {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "--- FAIL:") {
				failed = true
			}
		}
		for i := b.start; i < b.end; i++ {
			used[i] = true
			line := lines[i]
			trimmed := strings.TrimSpace(line)
			switch {
			case strings.HasPrefix(line, "=== RUN"), strings.HasPrefix(line, "=== PAUSE"),
				strings.HasPrefix(line, "=== CONT"), strings.HasPrefix(line, "=== NAME"):
				removed++
			case strings.HasPrefix(trimmed, "--- PASS:"):
				removed++
			case !failed:
				removed++
			default:
				if strings.HasPrefix(trimmed, "--- FAIL:") {
					failures++
				}
				out = append(out, line)
			}
		}
	}
	for i, line := range lines {
		if used[i] {
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case isGoPackageSummary(line):
			out = append(out, line)
		case strings.HasPrefix(trimmed, "--- FAIL:"):
			failures++
			out = append(out, line)
		case strings.HasPrefix(line, "=== RUN"), strings.HasPrefix(line, "=== PAUSE"),
			strings.HasPrefix(line, "=== CONT"), strings.HasPrefix(line, "=== NAME"):
			removed++
		case strings.HasPrefix(trimmed, "--- PASS:"):
			removed++
		default:
			out = append(out, line)
		}
	}
	if failures == 0 {
		return Output{}, fmt.Errorf("no failure block found")
	}
	return Output{Text: joinLines(out), Lossy: true, RemovedLines: removed}, nil
}

func isGoPackageSummary(line string) bool {
	return strings.HasPrefix(line, "ok  \t") || strings.HasPrefix(line, "FAIL\t") ||
		strings.HasPrefix(line, "?   \t") || line == "PASS" || line == "FAIL"
}

func renderGoTestJSONFailures(order []string, failed []goTestEvent, outputs map[string][]string) (Output, error) {
	failedPackages := map[string]bool{}
	var out []string
	seen := map[string]bool{}
	for _, e := range failed {
		failedPackages[e.Package] = true
		if e.Test == "" {
			if !seen[e.Package] {
				seen[e.Package] = true
				out = append(out, "FAIL "+e.Package)
			}
			continue
		}
		key := e.Package + "\x00" + e.Test
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, "FAIL "+e.Package+" "+e.Test)
		for _, l := range outputs[key] {
			if strings.TrimSpace(l) != "" {
				out = append(out, l)
			}
		}
	}
	if len(out) == 0 {
		return Output{}, fmt.Errorf("no failed test")
	}
	for _, pkg := range order {
		if !failedPackages[pkg] {
			out = append(out, "ok  "+pkg)
		}
	}
	return Output{Text: joinLines(out), Lossy: true, RemovedLines: len(failed)}, nil
}

func formatSeconds(v float64) string {
	return strconv.FormatFloat(v, 'f', 3, 64) + "s"
}
