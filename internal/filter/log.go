package filter

import (
	"fmt"
	"strings"
)

// logFilter folds consecutive identical lines into one line plus an explicit
// repeat count. It does not drop line content: every run is represented by its
// text and total count, so the original can be reconstructed exactly. Runs that
// are too short to pay for the annotation are left untouched.
type logFilter struct{}

func (logFilter) ID() string { return "log-repeat/v1" }

func (f logFilter) Apply(in Input) (Output, error) {
	lines := splitLines(string(in.Stdout))
	if len(lines) < 2 {
		return Output{}, fmt.Errorf("nothing to fold")
	}
	for _, line := range lines {
		if strings.Contains(line, "rtk-go ×") {
			return Output{}, fmt.Errorf("ambiguous repeat marker")
		}
	}
	var out []string
	saved := 0
	i := 0
	for i < len(lines) {
		j := i + 1
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}
		n := j - i
		line := lines[i]
		if n == 1 {
			out = append(out, line)
			i = j
			continue
		}
		var annotation string
		if line == "" {
			annotation = fmt.Sprintf("[rtk-go: blank ×%d]", n)
		} else {
			annotation = fmt.Sprintf("%s [rtk-go ×%d]", line, n)
		}
		if len(annotation)+1 >= (len(line)+1)*n {
			for k := 0; k < n; k++ {
				out = append(out, line)
			}
			i = j
			continue
		}
		out = append(out, annotation)
		saved += n - 1
		i = j
	}
	if saved == 0 {
		return Output{}, fmt.Errorf("nothing to fold")
	}
	return Output{Text: joinLines(out), Lossy: true, RemovedLines: saved}, nil
}

// safeLogArgs accepts only non-interactive cat/tail/head shapes. The filter
// must never be attached to a follow/stream command or to an option that
// changes the meaning of the captured text.
func safeTextCommandArgs(tool string, args []string) bool {
	afterDash := false
	hasFile := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if afterDash {
			if a != "-" {
				hasFile = true
			}
			continue
		}
		if a == "--" {
			afterDash = true
			continue
		}
		if a == "-" {
			return false
		}
		if !strings.HasPrefix(a, "-") {
			hasFile = true
			continue
		}
		switch tool {
		case "cat":
			return false
		case "tail", "head":
			switch {
			case a == "-n" || a == "--lines":
				if i+1 >= len(args) || strings.HasPrefix(args[i+1], "-") {
					return false
				}
				i++
			case strings.HasPrefix(a, "-n") && len(a) > 2:
			case strings.HasPrefix(a, "--lines="):
			default:
				return false
			}
		default:
			return false
		}
	}
	return hasFile
}
