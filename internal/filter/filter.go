// Package filter implements deterministic, fact-preserving output filters.
// Every filter is a pure conversion: no processes, no network, no config.
package filter

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Input is one completed invocation.
type Input struct {
	Argv     []string
	ExitCode int
	Stdout   []byte
	Profile  string
}

// Output is a filter candidate plus provenance.
type Output struct {
	Text         []byte
	ParserID     string
	Lossy        bool
	RemovedLines int
}

// Filter converts captured stdout.
type Filter interface {
	ID() string
	Apply(Input) (Output, error)
}

// Selection is the filter chosen for an argv shape.
type Selection struct {
	Filter Filter
	Family string
	// Test marks explicit test/build commands that may wait for the longer
	// capture window. It never enables automatic rewriting.
	Test bool
	// Lossless marks a transformation that removes no body content (today:
	// rg path grouping), so it does not require recovery storage.
	Lossless bool
}

// Select picks a filter from the v1 support matrix. Unknown commands and
// flag shapes return nil so the output is passed through unchanged.
func Select(argv []string, profile string) *Selection {
	if len(argv) == 0 {
		return nil
	}
	prog := strings.ToLower(strings.TrimSuffix(filepath.Base(argv[0]), ".exe"))
	switch prog {
	case "git":
		sub, rest, ok := splitGitArgs(argv)
		if !ok {
			return nil
		}
		switch sub {
		case "status":
			if len(rest) == 0 || (len(rest) == 1 && rest[0] == "--long") {
				return &Selection{Filter: gitStatusFilter{compact: profile == "aggressive"}, Family: "git status"}
			}
		case "log":
			if safeLogArgs(rest) {
				max := logLimit(rest)
				return &Selection{Filter: gitLogFilter{aggressive: profile == "aggressive", max: max}, Family: "git log"}
			}
		case "diff":
			if profile == "aggressive" && safeDiffArgs(rest) {
				return &Selection{Filter: gitDiffFilter{}, Family: "git diff"}
			}
		}
	case "cat", "tail", "head":
		if profile == "lossless" && safeTextCommandArgs(prog, argv[1:]) {
			return &Selection{Filter: logFilter{}, Family: "log repeats"}
		}
	case "rg":
		if safeRGArgs(argv[1:]) {
			aggressive := profile == "aggressive"
			return &Selection{Filter: rgFilter{aggressive: aggressive}, Family: "rg", Lossless: !aggressive}
		}
	case "go":
		if len(argv) >= 2 && argv[1] == "test" {
			if profile == "safe" || profile == "lossless" || hasArg(argv[2:], "-c") || hasArg(argv[2:], "--compile-only") {
				return nil
			}
			return &Selection{Filter: goTestFilter{json: hasArg(argv[2:], "-json"), aggressive: profile == "aggressive"}, Family: "go test", Test: true}
		}
	case "pytest", "py.test":
		if profile == "safe" || profile == "lossless" || !safePytestArgs(argv[1:]) {
			return nil
		}
		return &Selection{Filter: pytestFilter{aggressive: profile == "aggressive"}, Family: "pytest", Test: true}
	case "python", "python3":
		if profile == "safe" || profile == "lossless" || len(argv) < 3 || argv[1] != "-m" || argv[2] != "pytest" || !safePytestArgs(argv[3:]) {
			return nil
		}
		return &Selection{Filter: pytestFilter{aggressive: profile == "aggressive"}, Family: "pytest", Test: true}
	case "node":
		if profile == "safe" || profile == "lossless" || !nodeTapArgs(argv[1:]) {
			return nil
		}
		return &Selection{Filter: nodeTAPFilter{}, Family: "node --test", Test: true}
	}
	return nil
}

func splitGitArgs(argv []string) (string, []string, bool) {
	for i := 1; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "-C" || a == "-c" || a == "--git-dir" || a == "--work-tree":
			if i+1 >= len(argv) {
				return "", nil, false
			}
			i++
		case strings.HasPrefix(a, "--git-dir=") || strings.HasPrefix(a, "--work-tree="):
		case a == "--no-pager" || a == "--paginate":
		case a == "status" || a == "log" || a == "diff":
			return a, argv[i+1:], true
		default:
			return "", nil, false
		}
	}
	return "", nil, false
}

func safeLogArgs(args []string) bool {
	afterDash := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if afterDash {
			continue
		}
		switch {
		case a == "--":
			afterDash = true
		case a == "--all", a == "--first-parent", a == "--no-decorate", a == "--decorate",
			a == "--branches", a == "--tags", a == "--remotes", a == "--merges", a == "--no-merges":
		case strings.HasPrefix(a, "--decorate="), strings.HasPrefix(a, "--max-count="),
			strings.HasPrefix(a, "--author="), strings.HasPrefix(a, "--grep="),
			strings.HasPrefix(a, "--since="), strings.HasPrefix(a, "--until="):
		case a == "-n" || a == "--max-count" || a == "--author" || a == "--grep" || a == "--since" || a == "--until":
			if i+1 >= len(args) {
				return false
			}
			i++
		case strings.HasPrefix(a, "-n") && isDigits(a[2:]):
		case len(a) > 1 && a[0] == '-' && isDigits(a[1:]):
		default:
			// Any formatting, patch, graph, or unknown flag is unsafe.
			return false
		}
	}
	return true
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

var rgRejectExact = []string{
	"--json", "-0", "--null", "--count", "-c", "--files", "--files-with-matches", "-l",
	"--files-without-match", "--only-matching", "-o", "--passthru", "-p", "--multiline", "-U",
	"--replace", "-r", "--pre", "--pre-glob", "--context", "-A", "-B", "-C", "--after-context",
	"--before-context", "--max-columns", "--no-line-number", "-N", "--heading", "--pretty",
}

var rgRejectPrefix = []string{
	"--json", "--count", "--context=", "--after-context=", "--before-context=",
	"--max-columns=", "--replace=", "--pre=", "--pre-glob=", "-A", "-B", "-C",
}

func safeRGArgs(args []string) bool {
	hasPattern := false
	afterDash := false
	for _, a := range args {
		if afterDash {
			hasPattern = true
			continue
		}
		if a == "--" {
			afterDash = true
			continue
		}
		for _, bad := range rgRejectExact {
			if a == bad {
				return false
			}
		}
		for _, bad := range rgRejectPrefix {
			if strings.HasPrefix(a, bad) {
				return false
			}
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			hasPattern = true
		}
	}
	return hasPattern
}

func nodeTapArgs(args []string) bool {
	isTest := false
	reporters := 0
	tap := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--test":
			isTest = true
		case strings.HasPrefix(a, "--test-reporter-destination"), strings.HasPrefix(a, "--test-reporter-destinations"):
			return false
		case a == "--test-reporter":
			if i+1 >= len(args) {
				return false
			}
			i++
			reporters++
			tap = args[i] == "tap"
		case strings.HasPrefix(a, "--test-reporter="):
			reporters++
			tap = strings.TrimPrefix(a, "--test-reporter=") == "tap"
		}
	}
	return isTest && reporters == 1 && tap
}

func logLimit(args []string) int {
	limit := 10
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-n" || a == "--max-count":
			if i+1 < len(args) {
				if n, err := strconv.Atoi(args[i+1]); err == nil && n > 0 {
					limit = n
				}
				i++
			}
		case strings.HasPrefix(a, "-n"):
			if n, err := strconv.Atoi(a[2:]); err == nil && n > 0 {
				limit = n
			}
		case strings.HasPrefix(a, "--max-count="):
			if n, err := strconv.Atoi(strings.TrimPrefix(a, "--max-count=")); err == nil && n > 0 {
				limit = n
			}
		case len(a) > 1 && a[0] == '-' && isDigits(a[1:]):
			if n, err := strconv.Atoi(a[1:]); err == nil && n > 0 {
				limit = n
			}
		}
	}
	return limit
}

func safeDiffArgs(args []string) bool {
	afterDash := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if afterDash {
			continue
		}
		switch {
		case a == "--":
			afterDash = true
		case a == "--cached" || a == "--staged" || a == "--no-color" || a == "--color=never" || a == "-R":
		case strings.HasPrefix(a, "-U"):
		case a == "-U":
			i++
		case a == "--unified" || a == "--unified=":
			if a == "--unified" {
				i++
			}
		case a == "--stat" || a == "--name-only" || a == "--name-status" || a == "--raw" ||
			a == "--numstat" || a == "--shortstat" || a == "--word-diff" || a == "--word-diff-regex" ||
			a == "--binary" || strings.HasPrefix(a, "--color") && a != "--color=never":
			return false
		case strings.HasPrefix(a, "-"):
			return false
		}
	}
	return true
}

func safePytestArgs(args []string) bool {
	for _, a := range args {
		switch a {
		case "--collect-only", "--co", "--fixtures", "--setup-show", "--version", "--help", "-h":
			return false
		}
	}
	return true
}

func hasArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

// Run applies f with encoding/ANSI guards and a hard CPU deadline. Any panic,
// timeout, or malformed output yields a fallback reason and no candidate.
func Run(f Filter, in Input) (out Output, fallback string) {
	text, why := sanitize(in.Stdout)
	if why != "" {
		return Output{}, why
	}
	in.Stdout = text

	type result struct {
		out Output
		err error
	}
	ch := make(chan result, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				ch <- result{err: fmt.Errorf("parser panic: %v", r)}
			}
		}()
		o, err := f.Apply(in)
		ch <- result{out: o, err: err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return Output{}, "parser: " + r.err.Error()
		}
		if !utf8.Valid(r.out.Text) {
			return Output{}, "parser produced invalid UTF-8"
		}
		if len(r.out.Text) > 0 && bytes.IndexByte(r.out.Text, 0) >= 0 {
			return Output{}, "parser produced binary output"
		}
		r.out.ParserID = f.ID()
		return r.out, ""
	case <-time.After(250 * time.Millisecond):
		return Output{}, "filter CPU deadline exceeded"
	}
}

// sanitize strips only fully-verified SGR sequences. Any other escape, NUL,
// invalid UTF-8, or unusual control byte forces raw output.
func sanitize(b []byte) ([]byte, string) {
	if len(b) == 0 {
		return b, "empty output"
	}
	if bytes.IndexByte(b, 0) >= 0 {
		return nil, "binary output"
	}
	if !utf8.Valid(b) {
		return nil, "non-UTF-8 output"
	}
	b = bytes.TrimPrefix(b, []byte{0xEF, 0xBB, 0xBF})
	var out []byte
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c == 0x1b {
			if i+1 >= len(b) || b[i+1] != '[' {
				return nil, "unsupported escape sequence"
			}
			j := i + 2
			for j < len(b) && b[j] >= 0x30 && b[j] <= 0x3f {
				j++
			}
			for j < len(b) && b[j] >= 0x20 && b[j] <= 0x2f {
				j++
			}
			if j >= len(b) || b[j] != 'm' {
				return nil, "unsupported ANSI sequence"
			}
			i = j
			continue
		}
		if c == '\r' && (i+1 >= len(b) || b[i+1] != '\n') {
			return nil, "carriage-return progress output"
		}
		if c < 0x20 && c != '\n' && c != '\r' && c != '\t' {
			return nil, "control characters"
		}
		if c == 0x7f {
			return nil, "control characters"
		}
		out = append(out, c)
	}
	return out, ""
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func joinLines(lines []string) []byte {
	return []byte(strings.Join(lines, "\n") + "\n")
}

// BuildCandidate appends the recovery hint for lossy candidates and enforces
// the strict byte guard. It returns nil when raw output must be used.
func BuildCandidate(raw []byte, out Output, hint string) []byte {
	if out.Text == nil {
		return nil
	}
	candidate := append([]byte(nil), out.Text...)
	if out.Lossy {
		if hint == "" {
			return nil
		}
		candidate = append(candidate, hint...)
	}
	if len(candidate) == 0 || len(candidate) >= len(raw) {
		return nil
	}
	return candidate
}
