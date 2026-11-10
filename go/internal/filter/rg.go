package filter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// rgFilter groups losslessly by path: every match line, line/column number,
// and order is preserved.
type rgFilter struct{}

func (rgFilter) ID() string { return "rg/v1" }

var (
	rgFour  = regexp.MustCompile(`^(.+?):([0-9]+):([0-9]+):(.*)$`)
	rgThree = regexp.MustCompile(`^(.+?):([0-9]+):(.*)$`)
)

type rgMatch struct {
	path string
	body string
}

func (rgFilter) Apply(in Input) (Output, error) {
	lines := splitLines(string(in.Stdout))
	if len(lines) == 0 {
		return Output{}, fmt.Errorf("no matches")
	}
	matches := make([]rgMatch, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			return Output{}, fmt.Errorf("blank line in rg output")
		}
		m, ok := parseRGLine(line)
		if !ok {
			return Output{}, fmt.Errorf("unrecognized rg line")
		}
		matches = append(matches, m)
	}
	var out []string
	i := 0
	for i < len(matches) {
		j := i
		for j < len(matches) && matches[j].path == matches[i].path {
			j++
		}
		out = append(out, fmt.Sprintf("%s (%d)", matches[i].path, j-i))
		for k := i; k < j; k++ {
			out = append(out, "  "+matches[k].body)
		}
		i = j
	}
	// Grouping is lossless: each path is recoverable from its heading, and
	// every match body, line/column, and order is preserved. No recovery hint.
	return Output{Text: joinLines(out)}, nil
}

func parseRGLine(line string) (rgMatch, bool) {
	if m := rgFour.FindStringSubmatch(line); m != nil {
		if strings.Contains(m[1], ":") {
			return rgMatch{}, false
		}
		if n, err := strconv.Atoi(m[2]); err != nil || n < 1 {
			return rgMatch{}, false
		}
		if n, err := strconv.Atoi(m[3]); err != nil || n < 1 {
			return rgMatch{}, false
		}
		return rgMatch{path: m[1], body: m[2] + ":" + m[3] + ":" + m[4]}, true
	}
	if m := rgThree.FindStringSubmatch(line); m != nil {
		if strings.Contains(m[1], ":") {
			return rgMatch{}, false
		}
		if n, err := strconv.Atoi(m[2]); err != nil || n < 1 {
			return rgMatch{}, false
		}
		return rgMatch{path: m[1], body: m[2] + ":" + m[3]}, true
	}
	return rgMatch{}, false
}
