package filter

import (
	"fmt"
	"strconv"
	"strings"
)

// gitStatusFilter compacts the default long-format status. In aggressive mode
// it emits porcelain-style XY codes; every path, rename pair, conflict marker,
// and branch/divergence fact survives.
type gitStatusFilter struct{ compact bool }

func (gitStatusFilter) ID() string { return "git-status/v1" }

var statusSections = map[string]string{
	"Changes to be committed:":       "staged:",
	"Changes not staged for commit:": "unstaged:",
	"Untracked files:":               "untracked:",
	"Unmerged paths:":                "conflicts:",
}

var statusEntryLabels = []string{
	"modified", "new file", "deleted", "renamed", "copied", "typechange",
	"both modified", "both added", "deleted by us", "deleted by them",
	"added by us", "added by them", "unmerged", "both deleted",
}

type statusEntry struct {
	section string
	label   string
	path    string
	raw     string
}

func (f gitStatusFilter) Apply(in Input) (Output, error) {
	lines := splitLines(string(in.Stdout))
	if len(lines) == 0 {
		return Output{}, fmt.Errorf("empty status")
	}
	var branch []string
	var entries []statusEntry
	var notes []string
	section := ""
	sawKnown := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" {
			continue
		}
		if replacement, ok := statusSections[trimmed]; ok {
			section = replacement
			sawKnown = true
			continue
		}
		if strings.HasPrefix(line, "  (") {
			if !f.compact {
				continue
			}
			continue
		}
		if strings.HasPrefix(line, "\t") {
			if section == "" {
				return Output{}, fmt.Errorf("status entry outside a known section")
			}
			label, path, ok := parseStatusEntry(line, section)
			if !ok {
				return Output{}, fmt.Errorf("unknown status entry")
			}
			entries = append(entries, statusEntry{section: section, label: label, path: path, raw: line})
			sawKnown = true
			continue
		}
		switch {
		case strings.HasPrefix(line, "On branch "),
			strings.HasPrefix(line, "HEAD detached at "),
			strings.HasPrefix(line, "HEAD detached from "),
			line == "Not currently on any branch.",
			strings.HasPrefix(line, "Your branch is "),
			strings.HasPrefix(line, "You are currently "),
			strings.HasPrefix(line, "You are in the middle of "),
			strings.HasPrefix(line, "interactive rebase in progress;"),
			strings.HasPrefix(line, "rebase in progress;"),
			line == "You have unmerged paths.",
			line == "All conflicts fixed but you are still merging.",
			line == "No commits yet",
			strings.HasPrefix(line, "nothing to commit"),
			strings.HasPrefix(line, "nothing added to commit"),
			strings.HasPrefix(line, "no changes added to commit"):
			sawKnown = true
			branch, notes = collectStatusFact(branch, notes, line, f.compact)
		default:
			return Output{}, fmt.Errorf("unrecognized status line")
		}
	}
	if !sawKnown || (len(entries) == 0 && len(branch) == 0 && len(notes) == 0) {
		return Output{}, fmt.Errorf("no recognizable status content")
	}
	if !f.compact {
		return f.renderVerbose(entries, branch, notes)
	}
	return f.renderCompact(entries, branch, notes)
}

func parseStatusEntry(line, section string) (string, string, bool) {
	rest := line[1:]
	if section == "untracked:" {
		if strings.TrimSpace(rest) == "" {
			return "", "", false
		}
		return "untracked", rest, true
	}
	for _, label := range statusEntryLabels {
		if strings.HasPrefix(rest, label+":") {
			path := strings.TrimSpace(strings.TrimPrefix(rest, label+":"))
			if path == "" {
				return "", "", false
			}
			if (label == "renamed" || label == "copied") && !strings.Contains(path, " -> ") {
				return "", "", false
			}
			return label, path, true
		}
	}
	return "", "", false
}

func collectStatusFact(branch, notes []string, line string, compact bool) ([]string, []string) {
	if !compact {
		switch {
		case strings.HasPrefix(line, "no changes added to commit"),
			strings.HasPrefix(line, "nothing added to commit"):
			if idx := strings.Index(line, " (use \"git "); idx >= 0 {
				line = strings.TrimRight(line[:idx], " ")
			}
		}
		return append(branch, line), notes
	}
	switch {
	case strings.HasPrefix(line, "On branch "):
		branch = append(branch, "## "+strings.TrimPrefix(line, "On branch "))
	case strings.HasPrefix(line, "HEAD detached at "), strings.HasPrefix(line, "HEAD detached from "):
		branch = append(branch, "## "+line)
	case line == "Not currently on any branch.":
		branch = append(branch, "## no branch")
	case strings.HasPrefix(line, "Your branch is up to date"):
		// no extra divergence token
	case strings.HasPrefix(line, "Your branch is ahead of "):
		if n, ok := divergenceCount(line, "by ", " commits"); ok {
			branch = addDivergence(branch, "ahead="+n)
		}
	case strings.HasPrefix(line, "Your branch is behind "):
		if n, ok := divergenceCount(line, "by ", " commits"); ok {
			branch = addDivergence(branch, "behind="+n)
		}
	case strings.Contains(line, "have diverged"):
		if n, ok := divergenceCount(line, "have ", " and "); ok {
			if m, ok2 := divergenceCount(line, " and ", " different"); ok2 {
				branch = addDivergence(branch, "diverged="+n+"/"+m)
			}
		}
	case line == "No commits yet":
		branch = append(branch, "## no commits")
	case strings.HasPrefix(line, "nothing to commit"):
		notes = append(notes, "clean")
	}
	return branch, notes
}

func addDivergence(branch []string, fact string) []string {
	if len(branch) == 0 {
		return append(branch, "## "+fact)
	}
	branch[len(branch)-1] += " " + fact
	return branch
}

func divergenceCount(s, before, after string) (string, bool) {
	i := strings.Index(s, before)
	if i < 0 {
		return "", false
	}
	rest := s[i+len(before):]
	j := strings.Index(rest, after)
	if j < 0 {
		return "", false
	}
	v := strings.TrimSpace(rest[:j])
	if _, err := strconv.Atoi(v); err != nil {
		return "", false
	}
	return v, true
}

func (gitStatusFilter) renderVerbose(entries []statusEntry, branch, notes []string) (Output, error) {
	var out []string
	out = append(out, branch...)
	out = append(out, notes...)
	section := ""
	for _, e := range entries {
		header := ""
		switch e.section {
		case "staged:":
			header = "staged:"
		case "unstaged:":
			header = "unstaged:"
		case "untracked:":
			header = "untracked:"
		case "conflicts:":
			header = "conflicts:"
		}
		if header != "" && header != section {
			out = append(out, header)
			section = header
		}
		out = append(out, e.raw)
	}
	return Output{Text: joinLines(out), Lossy: len(entries) > 0, RemovedLines: len(entries)}, nil
}

func (gitStatusFilter) renderCompact(entries []statusEntry, branch, notes []string) (Output, error) {
	type row struct {
		path string
		x, y byte
	}
	var rows []row
	index := map[string]int{}
	put := func(e statusEntry, x, y byte) {
		if i, ok := index[e.path]; ok {
			if x != ' ' && x != '?' {
				rows[i].x = x
			}
			if y != ' ' && y != '?' {
				rows[i].y = y
			}
			return
		}
		index[e.path] = len(rows)
		rows = append(rows, row{path: e.path, x: x, y: y})
	}
	for _, e := range entries {
		switch e.section {
		case "staged:":
			put(e, stagedCode(e.label), ' ')
		case "unstaged:":
			put(e, ' ', unstagedCode(e.label))
		case "untracked:":
			put(e, '?', '?')
		case "conflicts:":
			x, y := conflictCode(e.label)
			put(e, x, y)
		}
	}
	var out []string
	out = append(out, branch...)
	out = append(out, notes...)
	for _, r := range rows {
		out = append(out, string([]byte{r.x, r.y})+" "+r.path)
	}
	// Every retained byte is a fact; hint accounting is approximate but must be
	// non-zero so a recovery hint is emitted.
	removed := len(rows)
	if removed == 0 {
		removed = 1
	}
	return Output{Text: joinLines(out), Lossy: true, RemovedLines: removed}, nil
}

func stagedCode(label string) byte {
	switch label {
	case "modified":
		return 'M'
	case "new file":
		return 'A'
	case "deleted":
		return 'D'
	case "renamed":
		return 'R'
	case "copied":
		return 'C'
	case "typechange":
		return 'T'
	}
	return 'M'
}

func unstagedCode(label string) byte {
	switch label {
	case "modified":
		return 'M'
	case "deleted":
		return 'D'
	case "typechange":
		return 'T'
	}
	return 'M'
}

func conflictCode(label string) (byte, byte) {
	switch label {
	case "both modified", "unmerged":
		return 'U', 'U'
	case "both added":
		return 'A', 'A'
	case "both deleted":
		return 'D', 'D'
	case "deleted by us":
		return 'D', 'U'
	case "deleted by them":
		return 'U', 'D'
	case "added by us":
		return 'A', 'U'
	case "added by them":
		return 'U', 'A'
	}
	return 'U', 'U'
}
