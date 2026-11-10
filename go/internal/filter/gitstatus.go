package filter

import (
	"fmt"
	"strings"
)

// gitStatusFilter compacts the default long-format status while preserving
// every path, rename pair, conflict marker, and branch/divergence fact.
type gitStatusFilter struct{}

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
	"added by us", "added by them", "unmerged",
}

func (gitStatusFilter) Apply(in Input) (Output, error) {
	lines := splitLines(string(in.Stdout))
	if len(lines) == 0 {
		return Output{}, fmt.Errorf("empty status")
	}
	var out []string
	section := ""
	removed := 0
	sawKnown := false
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t")
		if trimmed == "" {
			continue
		}
		if replacement, ok := statusSections[trimmed]; ok {
			section = replacement
			out = append(out, replacement)
			sawKnown = true
			continue
		}
		if strings.HasPrefix(line, "  (") {
			removed++
			continue
		}
		if strings.HasPrefix(line, "\t") {
			if section == "" {
				return Output{}, fmt.Errorf("status entry outside a known section")
			}
			if err := validateStatusEntry(line, section); err != nil {
				return Output{}, err
			}
			out = append(out, line)
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
			line == "You have unmerged paths.",
			line == "All conflicts fixed but you are still merging.",
			line == "No commits yet",
			strings.HasPrefix(line, "nothing to commit"),
			strings.HasPrefix(line, "nothing added to commit"),
			strings.HasPrefix(line, "no changes added to commit"):
			sawKnown = true
			if strings.Contains(line, " (use \"git ") {
				// Keep the fact but drop the suggested command.
				idx := strings.Index(line, " (use \"git ")
				out = append(out, strings.TrimRight(line[:idx], " "))
				removed++
				continue
			}
			out = append(out, line)
		default:
			return Output{}, fmt.Errorf("unrecognized status line")
		}
	}
	if !sawKnown || len(out) == 0 {
		return Output{}, fmt.Errorf("no recognizable status content")
	}
	return Output{Text: joinLines(out), Lossy: removed > 0, RemovedLines: removed}, nil
}

func validateStatusEntry(line, section string) error {
	rest := line[1:]
	if section == "untracked:" {
		if strings.TrimSpace(rest) == "" {
			return fmt.Errorf("empty untracked path")
		}
		return nil
	}
	for _, label := range statusEntryLabels {
		if strings.HasPrefix(rest, label+":") {
			path := strings.TrimSpace(strings.TrimPrefix(rest, label+":"))
			if path == "" {
				return fmt.Errorf("empty status path")
			}
			if (label == "renamed" || label == "copied") && !strings.Contains(path, " -> ") {
				return fmt.Errorf("rename entry without both paths")
			}
			return nil
		}
	}
	return fmt.Errorf("unknown status entry label")
}
