package filter

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// rgFilter groups losslessly by path. In aggressive mode it also caps the
// number of files/matches and truncates very long match lines; hidden content
// is recoverable from the committed original.
type rgFilter struct{ aggressive bool }

const (
	rgMaxFiles     = 10
	rgMaxMatches   = 200
	rgMaxLineBytes = 40
)

func (rgFilter) ID() string { return "rg/v1" }

var (
	rgFour  = regexp.MustCompile(`^(.+?):([0-9]+):([0-9]+):(.*)$`)
	rgThree = regexp.MustCompile(`^(.+?):([0-9]+):(.*)$`)
)

type rgMatch struct {
	path string
	body string
}

func (f rgFilter) Apply(in Input) (Output, error) {
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
	if !f.aggressive {
		return renderRGGroups(matches, len(matches), len(matches)), nil
	}
	truncated := 0
	for i := range matches {
		if len(matches[i].body) > rgMaxLineBytes {
			matches[i].body = truncateUTF8(matches[i].body, rgMaxLineBytes) + "..."
			truncated++
		}
	}
	files := 0
	shown := 0
	lastPath := ""
	for _, m := range matches {
		if m.path != lastPath {
			if files >= rgMaxFiles {
				break
			}
			files++
			lastPath = m.path
		}
		if shown >= rgMaxMatches {
			break
		}
		shown++
	}
	out := renderRGGroups(matches[:shown], shown, len(matches))
	if shown < len(matches) {
		hiddenFiles := countDistinctPaths(matches[shown:])
		if shown > 0 && matches[shown-1].path == matches[shown].path {
			hiddenFiles--
		}
		out.Text = joinLines(append(splitLines(string(out.Text)), fmt.Sprintf("+%d more files, %d matches hidden", hiddenFiles, len(matches)-shown)))
		out.Lossy = true
		out.RemovedLines += len(matches) - shown
	}
	if truncated > 0 {
		out.Lossy = true
		out.RemovedLines += truncated
	}
	return out, nil
}

func renderRGGroups(matches []rgMatch, shown, total int) Output {
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
	// Grouping is lossless when every match is shown.
	return Output{Text: joinLines(out), Lossy: false}
}

func countDistinctPaths(matches []rgMatch) int {
	n := 0
	last := ""
	for _, m := range matches {
		if m.path != last {
			n++
			last = m.path
		}
	}
	return n
}

func truncateUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
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
