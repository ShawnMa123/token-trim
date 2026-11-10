package filter

import (
	"fmt"
	"strings"
)

// gitLogFilter compresses default-format log redundancy while keeping the
// complete hash, date, decorations, merge parents, message, and order.
type gitLogFilter struct{}

func (gitLogFilter) ID() string { return "git-log/v1" }

func (gitLogFilter) Apply(in Input) (Output, error) {
	lines := splitLines(string(in.Stdout))
	if len(lines) == 0 {
		return Output{}, fmt.Errorf("empty log")
	}
	var out []string
	removed := 0
	commits := 0
	i := 0
	for i < len(lines) {
		for i < len(lines) && lines[i] == "" {
			i++
		}
		if i >= len(lines) {
			break
		}
		if !isCommitLine(lines[i]) {
			return Output{}, fmt.Errorf("expected commit header")
		}
		commits++
		out = append(out, lines[i])
		i++
		if i < len(lines) && strings.HasPrefix(lines[i], "Merge: ") {
			out = append(out, lines[i])
			i++
		}
		if i >= len(lines) || !strings.HasPrefix(lines[i], "Author: ") {
			return Output{}, fmt.Errorf("missing author")
		}
		removed++
		i++
		if i >= len(lines) || !strings.HasPrefix(lines[i], "Date:   ") {
			return Output{}, fmt.Errorf("missing date")
		}
		out = append(out, lines[i])
		i++
		if i < len(lines) && lines[i] != "" {
			return Output{}, fmt.Errorf("missing blank line after date")
		}
		for i < len(lines) && lines[i] == "" {
			i++
		}
		var msg []string
		for i < len(lines) && !isCommitLine(lines[i]) {
			msg = append(msg, lines[i])
			i++
		}
		for len(msg) > 0 && msg[len(msg)-1] == "" {
			msg = msg[:len(msg)-1]
		}
		for _, l := range msg {
			if l == "" {
				out = append(out, "")
				continue
			}
			if !strings.HasPrefix(l, "    ") {
				return Output{}, fmt.Errorf("unexpected log line")
			}
			out = append(out, l[4:])
		}
	}
	if commits == 0 {
		return Output{}, fmt.Errorf("no commits")
	}
	return Output{Text: joinLines(out), Lossy: removed > 0, RemovedLines: removed}, nil
}

func isCommitLine(line string) bool {
	if !strings.HasPrefix(line, "commit ") {
		return false
	}
	rest := strings.TrimPrefix(line, "commit ")
	if idx := strings.IndexByte(rest, ' '); idx >= 0 {
		rest = rest[:idx]
	}
	if len(rest) != 40 && len(rest) != 64 {
		return false
	}
	for _, r := range rest {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			return false
		}
	}
	return true
}
