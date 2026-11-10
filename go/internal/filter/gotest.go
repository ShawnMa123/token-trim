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
	json bool
}

func (f goTestFilter) ID() string {
	if f.json {
		return "gotest-json/v1"
	}
	return "gotest/v1"
}

func (f goTestFilter) Apply(in Input) (Output, error) {
	if f.json {
		return applyGoTestJSON(in)
	}
	return applyGoTestText(in)
}

func applyGoTestText(in Input) (Output, error) {
	if in.ExitCode != 0 {
		return Output{}, fmt.Errorf("nonzero exit")
	}
	text := string(in.Stdout)
	for _, marker := range []string{"--- FAIL", "panic:", "DATA RACE", "fatal error:", "build failed"} {
		if strings.Contains(text, marker) {
			return Output{}, fmt.Errorf("failure marker %q", marker)
		}
	}
	lines := splitLines(text)
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
	"pass": true, "skip": true, "output": true,
}

func applyGoTestJSON(in Input) (Output, error) {
	if in.ExitCode != 0 {
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
		if !seen[e.Package] {
			seen[e.Package] = true
			order = append(order, e.Package)
		}
		key := e.Package + "\x00" + e.Test
		switch e.Action {
		case "fail":
			return Output{}, fmt.Errorf("failed test event")
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

func formatSeconds(v float64) string {
	return strconv.FormatFloat(v, 'f', 3, 64) + "s"
}
