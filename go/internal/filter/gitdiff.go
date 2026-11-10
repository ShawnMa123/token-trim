package filter

import (
	"fmt"
	"strings"
)

// gitDiffFilter keeps file/hunk headers and changed lines, dropping unchanged
// context. Every removed context line is recoverable from the committed
// original. It never applies to --stat/--name-only/--raw/--word-diff forms.
type gitDiffFilter struct{}

func (gitDiffFilter) ID() string { return "git-diff/v1" }

const gitDiffMaxChangedLines = 100

func (gitDiffFilter) Apply(in Input) (Output, error) {
	lines := splitLines(string(in.Stdout))
	if len(lines) == 0 {
		return Output{}, fmt.Errorf("empty diff")
	}
	var out []string
	inHunk := false
	hunks := 0
	removed := 0
	keptChanged := 0
	hiddenAdd, hiddenDel := 0, 0
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "diff --git "),
			strings.HasPrefix(line, "diff --cc "),
			strings.HasPrefix(line, "diff --combined "):
			inHunk = false
			out = append(out, line)
		case strings.HasPrefix(line, "index "),
			strings.HasPrefix(line, "old mode "),
			strings.HasPrefix(line, "new mode "),
			strings.HasPrefix(line, "deleted file mode "),
			strings.HasPrefix(line, "new file mode "),
			strings.HasPrefix(line, "similarity index "),
			strings.HasPrefix(line, "rename from "),
			strings.HasPrefix(line, "rename to "),
			strings.HasPrefix(line, "Binary files "),
			strings.HasPrefix(line, "GIT binary patch"):
			out = append(out, line)
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "):
			out = append(out, line)
		case strings.HasPrefix(line, "@@"):
			inHunk = true
			hunks++
			out = append(out, line)
		case inHunk:
			switch {
			case strings.HasPrefix(line, "+"), strings.HasPrefix(line, "-"), strings.HasPrefix(line, "\\"):
				if keptChanged < gitDiffMaxChangedLines {
					out = append(out, line)
					keptChanged++
				} else {
					if strings.HasPrefix(line, "+") {
						hiddenAdd++
					} else if strings.HasPrefix(line, "-") {
						hiddenDel++
					}
					removed++
				}
			case strings.HasPrefix(line, " "):
				removed++
			case line == "":
				// A completely empty line is not valid unified-diff context;
				// treat it as separator noise.
			default:
				return Output{}, fmt.Errorf("unexpected diff line")
			}
		case line == "":
			continue
		default:
			return Output{}, fmt.Errorf("unrecognized diff header")
		}
	}
	if hunks == 0 || len(out) == 0 {
		return Output{}, fmt.Errorf("no hunks")
	}
	if removed == 0 {
		return Output{}, fmt.Errorf("nothing to elide")
	}
	if hiddenAdd > 0 || hiddenDel > 0 {
		out = append(out, fmt.Sprintf("... (+%d -%d truncated)", hiddenAdd, hiddenDel))
	}
	return Output{Text: joinLines(out), Lossy: true, RemovedLines: removed}, nil
}
