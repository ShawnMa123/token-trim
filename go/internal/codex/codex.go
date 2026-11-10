// Package codex implements pure command rewrite analysis and the reversible
// instructions integration. Hooks are intentionally gated: without a verified
// Codex/platform permission regression, no hook installation or rewrite is
// enabled.
package codex

import (
	"path/filepath"
	"strings"

	"github.com/ShawnMa123/token-trim/go/internal/filter"
)

// RewriteResult is the pure analysis outcome. It never executes a command.
type RewriteResult struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
	Command  string `json:"command,omitempty"`
}

const (
	DecisionRewrite     = "rewrite"
	DecisionPassthrough = "passthrough"
)

// Analyze inspects one simple command for a known shell. It only wraps
// allowlisted read-only git/rg queries and never re-serializes the original
// command text.
func Analyze(shell, command, binary string, allowTests bool) RewriteResult {
	pass := func(reason string) RewriteResult {
		return RewriteResult{Decision: DecisionPassthrough, Reason: reason}
	}
	command = strings.TrimSpace(command)
	if command == "" {
		return pass("empty command")
	}
	if strings.ContainsAny(command, "\r\n") {
		return pass("multi-line command")
	}
	if alreadyWrapped(command) {
		return pass("already wrapped")
	}
	if binary == "" {
		return pass("wrapper path unknown")
	}
	tokens, ok := tokenize(command, shell)
	if !ok || len(tokens) == 0 {
		return pass("unparseable shell syntax")
	}
	for _, t := range tokens {
		if t == "" {
			continue
		}
	}
	prog := strings.ToLower(strings.TrimSuffix(filepath.Base(tokens[0]), ".exe"))
	if prog != "git" && prog != "rg" {
		return pass("unsupported program")
	}
	if prog == "git" {
		for _, a := range tokens[1:] {
			switch a {
			case "-c", "--exec-path", "--ext-diff":
				return pass("privileged git option")
			}
			if strings.HasPrefix(a, "--exec-path=") || strings.HasPrefix(a, "-c") {
				return pass("privileged git option")
			}
		}
	}
	if !rewriteSupported(tokens, allowTests) {
		return pass("unsupported query shape")
	}
	var prefix string
	switch shell {
	case "posix":
		prefix = "'" + strings.ReplaceAll(binary, "'", `'\''`) + "' exec -- "
	case "powershell":
		prefix = "& '" + strings.ReplaceAll(binary, "'", "''") + "' exec -- "
	default:
		return pass("unsupported shell")
	}
	return RewriteResult{Decision: DecisionRewrite, Reason: "simple supported query", Command: prefix + command}
}

func alreadyWrapped(command string) bool {
	lower := strings.ToLower(command)
	if strings.HasPrefix(lower, "rtk-go ") || strings.HasPrefix(lower, "rtk-go.exe ") {
		return true
	}
	for _, marker := range []string{"rtk-go' exec --", "rtk-go.exe' exec --", `rtk-go" exec --`, `rtk-go.exe" exec --`} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// tokenize validates a simple command and extracts argv. It rejects unquoted
// compound syntax, expansion, redirection, globbing, and nested shells.
func tokenize(command, shell string) ([]string, bool) {
	if shell != "posix" && shell != "powershell" {
		return nil, false
	}
	// Variable and command substitution are rejected everywhere; quoting rules
	// differ too much to prove safety.
	if strings.ContainsAny(command, "$`") {
		return nil, false
	}
	var (
		tokens   []string
		cur      strings.Builder
		inSingle bool
		inDouble bool
		quote    rune
	)
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	runes := []rune(command)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		switch {
		case inSingle:
			if c == '\'' {
				if shell == "powershell" && i+1 < len(runes) && runes[i+1] == '\'' {
					cur.WriteRune('\'')
					i++
					continue
				}
				inSingle = false
				continue
			}
			cur.WriteRune(c)
		case inDouble:
			if c == quote {
				inDouble = false
				continue
			}
			if (shell == "posix" && c == '\\') || (shell == "powershell" && c == '`') {
				if i+1 < len(runes) {
					i++
					cur.WriteRune(runes[i])
				}
				continue
			}
			if c == '\n' {
				return nil, false
			}
			cur.WriteRune(c)
		default:
			switch c {
			case '\'':
				inSingle = true
				quote = '\''
			case '"':
				inDouble = true
				quote = '"'
			case ' ', '\t':
				flush()
			case '\\':
				if shell == "posix" {
					if i+1 < len(runes) {
						i++
						cur.WriteRune(runes[i])
					}
				} else {
					return nil, false
				}
			case '`':
				return nil, false
			case ';', '|', '&', '<', '>', '(', ')', '{', '}', '[', ']', '#', '*', '?', '!':
				return nil, false
			case '\n', '\r':
				return nil, false
			default:
				cur.WriteRune(c)
			}
		}
	}
	if inSingle || inDouble {
		return nil, false
	}
	flush()
	return tokens, true
}

func rewriteSupported(tokens []string, allowTests bool) bool {
	// The safe profile selects only git status/log and rg; tests/builds stay
	// out. Test rewriting remains disabled in v1.
	s := filter.Select(tokens, "safe")
	if s == nil {
		return false
	}
	if s.Test && !allowTests {
		return false
	}
	return true
}
