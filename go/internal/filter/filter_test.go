package filter

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
)

func TestSelect(t *testing.T) {
	cases := []struct {
		argv    []string
		profile string
		want    bool
	}{
		{[]string{"git", "status"}, "balanced", true},
		{[]string{"git", "status", "--long"}, "balanced", true},
		{[]string{"git", "status", "--porcelain=v2"}, "balanced", false},
		{[]string{"git", "-C", "dir", "status"}, "balanced", true},
		{[]string{"git", "log", "-n", "5"}, "balanced", true},
		{[]string{"git", "log", "--oneline"}, "balanced", false},
		{[]string{"git", "log", "--format=%H"}, "balanced", false},
		{[]string{"git", "diff"}, "balanced", false},
		{[]string{"rg", "needle"}, "balanced", true},
		{[]string{"rg", "-n", "needle", "src"}, "balanced", true},
		{[]string{"rg", "--json", "needle"}, "balanced", false},
		{[]string{"rg", "-A", "2", "needle"}, "balanced", false},
		{[]string{"rg", "--pre", "./hook", "needle"}, "balanced", false},
		{[]string{"go", "test", "./..."}, "balanced", true},
		{[]string{"go", "test", "./..."}, "safe", false},
		{[]string{"pytest", "tests"}, "balanced", true},
		{[]string{"python", "-m", "pytest"}, "balanced", true},
		{[]string{"node", "--test", "--test-reporter=tap", "x.test.js"}, "balanced", true},
		{[]string{"node", "--test", "x.test.js"}, "balanced", false},
		{[]string{"npm", "test"}, "balanced", false},
		{[]string{"pytest"}, "safe", false},
		{[]string{"pytest", "--collect-only"}, "balanced", false},
		{[]string{"go", "test", "-c", "./..."}, "balanced", false},
		{[]string{"python", "-m", "pytest", "--fixtures"}, "balanced", false},
	}
	for _, tc := range cases {
		got := Select(tc.argv, tc.profile) != nil
		if got != tc.want {
			t.Errorf("Select(%q, %s) = %v, want %v", tc.argv, tc.profile, got, tc.want)
		}
	}
}

func TestSanitizeLoneCarriageReturn(t *testing.T) {
	if _, why := sanitize([]byte("progress 10%\rprogress 20%")); why == "" {
		t.Fatal("lone carriage return must force raw output")
	}
	if _, why := sanitize([]byte("line\r\nnext\r\n")); why != "" {
		t.Fatalf("CRLF should be accepted: %s", why)
	}
}

func TestSanitize(t *testing.T) {
	out, why := sanitize([]byte("\x1b[32mok\x1b[0m\r\n"))
	if why != "" || string(out) != "ok\r\n" {
		t.Fatalf("SGR: %q %q", out, why)
	}
	for _, bad := range [][]byte{
		[]byte("\x1b]0;title\x07"),
		[]byte("\x1b[2J"),
		[]byte{0xff, 0xfe},
		[]byte("a\x00b"),
	} {
		if _, why := sanitize(bad); why == "" {
			t.Errorf("sanitize(%q) should fail", bad)
		}
	}
}

func TestBuildCandidateByteGuard(t *testing.T) {
	raw := []byte(strings.Repeat("x", 100))
	hint := "[rtk-go: hidden=3 lines; recall=rtk-go recall abc]\n"
	if got := BuildCandidate(raw, Output{Text: []byte("short\n"), Lossy: true}, hint); got == nil {
		t.Fatal("candidate should be adopted")
	}
	if got := BuildCandidate(raw, Output{Text: []byte(strings.Repeat("y", 99)), Lossy: true}, hint); got != nil {
		t.Fatal("hint must push the candidate over the guard")
	}
	if got := BuildCandidate(raw, Output{Text: raw}, ""); got != nil {
		t.Fatal("tie must be rejected")
	}
}

const statusFixture = `On branch main
Your branch is ahead of 'origin/main' by 2 commits.
  (use "git push" to publish your local commits)

Changes to be committed:
  (use "git restore --staged <file>..." to unstage)
	modified:   src/a b.go
	new file:   "中文 文件.txt"
	renamed:    old name.go -> new name.go

Changes not staged for commit:
  (use "git add <file>..." to update what will be committed)
  (use "git restore <file>..." to discard changes in working directory)
	modified:   doc/readme.md

Untracked files:
  (use "git add <file>..." to include in what will be committed)
	notes.txt

no changes added to commit (use "git add" and/or "git commit -a")
`

func TestGitStatusKeepsAllPaths(t *testing.T) {
	out, why := Run(gitStatusFilter{}, Input{Argv: []string{"git", "status"}, Stdout: []byte(statusFixture)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{
		"On branch main", "Your branch is ahead of 'origin/main' by 2 commits.",
		"modified:   src/a b.go", `"中文 文件.txt"`,
		"renamed:    old name.go -> new name.go",
		"modified:   doc/readme.md", "notes.txt",
		"no changes added to commit",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("candidate lost %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"git push", "git restore", "git add <file>"} {
		if strings.Contains(text, gone) {
			t.Errorf("candidate kept hint %q", gone)
		}
	}
	if !out.Lossy || out.RemovedLines == 0 {
		t.Fatalf("expected lossy output: %+v", out)
	}
}

func TestGitStatusConflictDetachedAndClean(t *testing.T) {
	conflict := `HEAD detached at abc123
You have unmerged paths.
  (fix conflicts and run "git commit")

Unmerged paths:
  (use "git add <file>..." to mark resolution)
	both modified:   src/conflict.go
	both added:      docs/new notes.txt
`
	out, why := Run(gitStatusFilter{}, Input{Stdout: []byte(conflict)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{"HEAD detached at abc123", "You have unmerged paths.", "both modified:   src/conflict.go", "both added:      docs/new notes.txt"} {
		if !strings.Contains(text, want) {
			t.Errorf("lost %q:\n%s", want, text)
		}
	}
	clean := "On branch main\nnothing to commit, working tree clean\n"
	out, why = Run(gitStatusFilter{}, Input{Stdout: []byte(clean)})
	if why != "" {
		t.Fatalf("clean fallback: %s", why)
	}
	if !strings.Contains(string(out.Text), "nothing to commit, working tree clean") {
		t.Fatalf("clean candidate = %q", out.Text)
	}
}

func TestGitStatusFallbacks(t *testing.T) {
	for _, body := range []string{
		"Auf Branch main\n",
		"M  a.go\n",
		"On branch main\n\tweird: x\n",
		"Changes to be committed:\n\tmade up: x\n",
	} {
		if _, why := Run(gitStatusFilter{}, Input{Stdout: []byte(body)}); why == "" {
			t.Errorf("expected fallback for %q", body)
		}
	}
}

func logFixture() string {
	h1 := strings.Repeat("1", 40)
	h2 := strings.Repeat("2", 40)
	return "commit " + h1 + " (HEAD -> main, origin/main)\n" +
		"Author: Alice <a@example.com>\n" +
		"Date:   Mon Nov 10 12:00:00 2025 +0800\n\n" +
		"    fix: first line\n\n" +
		"    second paragraph\n" +
		"\n" +
		"commit " + h2 + "\n" +
		"Merge: " + strings.Repeat("a", 40) + " " + strings.Repeat("b", 40) + "\n" +
		"Author: Bob <b@example.com>\n" +
		"Date:   Sun Nov 9 12:00:00 2025 +0800\n\n" +
		"    merge work\n"
}

func TestGitLogKeepsFacts(t *testing.T) {
	out, why := Run(gitLogFilter{}, Input{Stdout: []byte(logFixture())})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{
		strings.Repeat("1", 40), strings.Repeat("2", 40), "(HEAD -> main, origin/main)",
		"Merge: ", "Date:   Mon Nov 10 12:00:00 2025 +0800", "fix: first line",
		"second paragraph", "merge work",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("candidate lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Author:") {
		t.Error("author should be folded")
	}
	if strings.Index(text, strings.Repeat("1", 40)) > strings.Index(text, strings.Repeat("2", 40)) {
		t.Error("commit order changed")
	}
}

func TestGitLogFallback(t *testing.T) {
	bad := "commit deadbeef\nAuthor: x\nDate:   y\n\n    msg\n"
	if _, why := Run(gitLogFilter{}, Input{Stdout: []byte(bad)}); why == "" {
		t.Fatal("short hash should fall back")
	}
	sig := logFixture() + "gpg: Signature made Mon\n"
	if _, why := Run(gitLogFilter{}, Input{Stdout: []byte(sig)}); why == "" {
		t.Fatal("signature block should fall back")
	}
}

func TestRGGrouping(t *testing.T) {
	body := "src/main.go:12:3:func main() {\n" +
		"src/main.go:20:1:}\n" +
		"internal/x.go:5:1:package x\n"
	out, why := Run(rgFilter{}, Input{Stdout: []byte(body)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{"src/main.go (2)", "  12:3:func main() {", "  20:1:}", "internal/x.go (1)", "  5:1:package x"} {
		if !strings.Contains(text, want) {
			t.Errorf("candidate lost %q:\n%s", want, text)
		}
	}
	if out.Lossy {
		t.Fatal("path grouping is lossless")
	}
	if _, why := Run(rgFilter{}, Input{Stdout: []byte("C:\\repo\\a.go:12:3:x\n")}); why == "" {
		t.Fatal("drive-letter path must fall back")
	}
	if _, why := Run(rgFilter{}, Input{Stdout: []byte("src/a.go-12-x\n")}); why == "" {
		t.Fatal("context line must fall back")
	}
}

func TestGoTestTextFoldsSuccess(t *testing.T) {
	body := "=== RUN   TestA\n=== PAUSE TestA\n=== CONT  TestA\n--- PASS: TestA (0.00s)\n" +
		"=== RUN   TestB\n--- PASS: TestB (0.01s)\nPASS\nok  \texample.com/pkg\t0.012s\n"
	out, why := Run(goTestFilter{}, Input{ExitCode: 0, Stdout: []byte(body)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	if strings.Contains(text, "=== RUN") || strings.Contains(text, "--- PASS") {
		t.Fatalf("passing detail survived:\n%s", text)
	}
	for _, want := range []string{"PASS", "ok  \texample.com/pkg\t0.012s"} {
		if !strings.Contains(text, want) {
			t.Errorf("candidate lost %q", want)
		}
	}
	if _, why := Run(goTestFilter{}, Input{ExitCode: 1, Stdout: []byte("--- FAIL: TestA\n")}); why == "" {
		t.Fatal("nonzero exit must fall back")
	}
	if _, why := Run(goTestFilter{}, Input{ExitCode: 0, Stdout: []byte("--- FAIL: TestA\n")}); why == "" {
		t.Fatal("failure marker must fall back")
	}
}

func TestGoTestJSON(t *testing.T) {
	body := strings.Join([]string{
		`{"Action":"start","Package":"p"}`,
		`{"Action":"run","Package":"p","Test":"TestA"}`,
		`{"Action":"output","Package":"p","Test":"TestA","Output":"=== RUN   TestA\n"}`,
		`{"Action":"pass","Package":"p","Test":"TestA","Elapsed":0}`,
		`{"Action":"output","Package":"p","Output":"PASS\n"}`,
		`{"Action":"output","Package":"p","Output":"ok  \tp\t0.002s\n"}`,
		`{"Action":"pass","Package":"p","Elapsed":0.002}`,
	}, "\n") + "\n"
	out, why := Run(goTestFilter{json: true}, Input{ExitCode: 0, Stdout: []byte(body)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	if !strings.Contains(string(out.Text), "ok  p  0.002s") {
		t.Fatalf("candidate = %q", out.Text)
	}
	fail := body + `{"Action":"fail","Package":"p","Elapsed":0.002}` + "\n"
	if _, why := Run(goTestFilter{json: true}, Input{ExitCode: 0, Stdout: []byte(fail)}); why == "" {
		t.Fatal("fail event must fall back")
	}
	if _, why := Run(goTestFilter{json: true}, Input{ExitCode: 0, Stdout: []byte("{")}); why == "" {
		t.Fatal("malformed JSON must fall back")
	}
}

const pytestQuiet = `..F                                                                      [100%]
=================================== FAILURES ===================================
_________________________________ test_x ______________________________________

    def test_x():
>       assert 1 == 2
E       assert 1 == 2

test_a.py:3: AssertionError
=========================== short test summary info ============================
FAILED test_a.py::test_x - assert 1 == 2
========================= 1 failed, 2 passed in 0.12s =========================
`

func TestPytestKeepsFailureSections(t *testing.T) {
	out, why := Run(pytestFilter{}, Input{ExitCode: 1, Stdout: []byte(pytestQuiet)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{"FAILURES", "assert 1 == 2", "test_a.py:3: AssertionError", "FAILED test_a.py::test_x", "1 failed, 2 passed"} {
		if !strings.Contains(text, want) {
			t.Errorf("candidate lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "[100%]") {
		t.Error("progress detail survived")
	}
}

func TestPytestQuietSuccess(t *testing.T) {
	body := "..                                                                       [100%]\n2 passed in 0.01s\n"
	out, why := Run(pytestFilter{}, Input{ExitCode: 0, Stdout: []byte(body)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	if !strings.Contains(string(out.Text), "2 passed in 0.01s") {
		t.Fatalf("candidate = %q", out.Text)
	}
}

func TestPytestUnknownPreambleFallsBack(t *testing.T) {
	body := "custom plugin: something\n1 passed in 0.01s\n"
	if _, why := Run(pytestFilter{}, Input{Stdout: []byte(body)}); why == "" {
		t.Fatal("unknown preamble must fall back")
	}
	body = "platform linux\ncollected 1 item\n1 passed in 0.01s\n"
	if _, why := Run(pytestFilter{}, Input{Stdout: []byte(body)}); why != "" {
		t.Fatalf("known preamble should be accepted: %s", why)
	}
}

const tapFixture = `TAP version 14
# Subtest: passes
ok 1 - passes
  ---
  duration_ms: 0.5
  type: 'test'
  ...
# Subtest: fails
not ok 2 - fails
  ---
  duration_ms: 1.2
  type: 'test'
  error: 'boom'
  code: 'ERR_ASSERTION'
  ...
1..2
# tests 2
# suites 0
# pass 1
# fail 1
# cancelled 0
# skipped 0
# todo 0
# duration_ms 12.3
`

func TestNodeTAPKeepsFailuresAndSummary(t *testing.T) {
	out, why := Run(nodeTAPFilter{}, Input{ExitCode: 1, Stdout: []byte(tapFixture)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{"not ok 2 - fails", "error: 'boom'", "# tests 2", "# fail 1"} {
		if !strings.Contains(text, want) {
			t.Errorf("candidate lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ok 1 - passes") {
		t.Error("passing block survived")
	}
	if _, why := Run(nodeTAPFilter{}, Input{Stdout: []byte("TAP version 14\n# unknown thing\n1..1\n# tests 1\n")}); why == "" {
		t.Fatal("unknown TAP comment must fall back")
	}
}

func TestGoTestTextKeepsSkipsAndWarnings(t *testing.T) {
	body := "=== RUN   TestA\n--- PASS: TestA (0.00s)\n=== RUN   TestB\n--- SKIP: TestB (0.00s)\n    skip_reason.go:10: not today\nPASS\nok  \tpkg\t0.01s\n"
	out, why := Run(goTestFilter{}, Input{ExitCode: 0, Stdout: []byte(body)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{"--- SKIP: TestB", "not today", "ok  \tpkg"} {
		if !strings.Contains(text, want) {
			t.Errorf("lost %q:\n%s", want, text)
		}
	}
}

func TestGoTestJSONSkip(t *testing.T) {
	body := strings.Join([]string{
		`{"Action":"start","Package":"p"}`,
		`{"Action":"run","Package":"p","Test":"TestA"}`,
		`{"Action":"pass","Package":"p","Test":"TestA","Elapsed":0}`,
		`{"Action":"run","Package":"p","Test":"TestB"}`,
		`{"Action":"output","Package":"p","Test":"TestB","Output":"reason: not ready\n"}`,
		`{"Action":"skip","Package":"p","Test":"TestB","Elapsed":0}`,
		`{"Action":"pass","Package":"p","Elapsed":0.01}`,
	}, "\n") + "\n"
	out, why := Run(goTestFilter{json: true}, Input{ExitCode: 0, Stdout: []byte(body)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	if !strings.Contains(text, "skip p TestB") || !strings.Contains(text, "reason: not ready") {
		t.Fatalf("candidate = %q", text)
	}
}

func TestPytestWarningsSkipsAndCollectionError(t *testing.T) {
	body := `============================= test session starts ==============================
platform win32 -- Python 3.11.0
rootdir: C:\proj
collected 2 items

test_a.py .s                                                              [100%]

=============================== warnings summary ===============================
test_a.py:1
  PytestWarning: synthetic

-- Docs: https://docs.pytest.org/
=========================== short test summary info ============================
SKIPPED [1] test_a.py:5: not ready
======================= 1 passed, 1 skipped, 1 warning in 0.05s =======================
`
	out, why := Run(pytestFilter{}, Input{ExitCode: 0, Stdout: []byte(body)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{"warnings summary", "PytestWarning: synthetic", "SKIPPED [1] test_a.py:5: not ready", "1 passed, 1 skipped, 1 warning"} {
		if !strings.Contains(text, want) {
			t.Errorf("lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "platform win32") {
		t.Error("platform banner survived")
	}
}

func TestNodeTAPSkipTodo(t *testing.T) {
	body := `TAP version 14
ok 1 - normal
  ---
  duration_ms: 1
  ...
ok 2 - skipped # SKIP not ready
  ---
  duration_ms: 0
  ...
ok 3 - todo # TODO later
1..3
# tests 3
# pass 1
# fail 0
# skipped 1
# todo 1
# duration_ms 10
`
	out, why := Run(nodeTAPFilter{}, Input{ExitCode: 0, Stdout: []byte(body)})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{"ok 2 - skipped # SKIP not ready", "ok 3 - todo # TODO later", "# skipped 1", "# todo 1"} {
		if !strings.Contains(text, want) {
			t.Errorf("lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "ok 1 - normal") {
		t.Error("passing block survived")
	}
}

func TestSelectAggressiveDiffAndRG(t *testing.T) {
	if Select([]string{"git", "diff"}, "balanced") != nil {
		t.Fatal("balanced must not select git diff")
	}
	if s := Select([]string{"git", "diff", "--", "file"}, "aggressive"); s == nil {
		t.Fatal("aggressive should select git diff")
	}
	if Select([]string{"git", "diff", "--stat"}, "aggressive") != nil {
		t.Fatal("--stat must stay raw")
	}
	if Select([]string{"git", "diff", "--word-diff"}, "aggressive") != nil {
		t.Fatal("--word-diff must stay raw")
	}
	if s := Select([]string{"rg", "needle"}, "aggressive"); s == nil || s.Lossless {
		t.Fatalf("aggressive rg should be lossy-capable: %+v", s)
	}
}

func TestGitStatusCompact(t *testing.T) {
	out, why := Run(gitStatusFilter{compact: true}, Input{Stdout: []byte(statusFixture), Profile: "aggressive"})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{
		"## main ahead=2",
		"M  src/a b.go",
		"A  \"中文 文件.txt\"",
		"R  old name.go -> new name.go",
		" M doc/readme.md",
		"?? notes.txt",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"modified:", "git restore", "Changes to be committed"} {
		if strings.Contains(text, gone) {
			t.Errorf("verbose text survived: %q", gone)
		}
	}
	if !out.Lossy || len(text) >= len(statusFixture) {
		t.Fatalf("compact output should be smaller and lossy: %d vs %d", len(text), len(statusFixture))
	}
}

func TestRGElidesBeyondCaps(t *testing.T) {
	var body strings.Builder
	for f := 0; f < 12; f++ {
		for m := 0; m < 20; m++ {
			body.WriteString("src/dir")
			body.WriteString(strconv.Itoa(f))
			body.WriteString("/file.go:")
			body.WriteString(strconv.Itoa(m + 1))
			body.WriteString(":1:match payload ")
			body.WriteString(strings.Repeat("x", 200))
			body.WriteString("\n")
		}
	}
	out, why := Run(rgFilter{aggressive: true}, Input{Stdout: []byte(body.String()), Profile: "aggressive"})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	if !strings.Contains(text, "+2 more files, 40 matches hidden") {
		t.Fatalf("missing hidden summary:\n%s", text)
	}
	if !out.Lossy || out.RemovedLines == 0 {
		t.Fatalf("expected lossy output: %+v", out)
	}
	if strings.Count(text, "  ") < 200 {
		t.Fatalf("expected 200 displayed matches")
	}
	if !strings.Contains(text, "...") {
		t.Fatal("long match lines should be truncated")
	}
}

func TestGitDiffDropsContext(t *testing.T) {
	body := "diff --git a/x.txt b/x.txt\n" +
		"index 111..222 100644\n" +
		"--- a/x.txt\n" +
		"+++ b/x.txt\n" +
		"@@ -1,4 +1,4 @@\n" +
		" context one\n" +
		"-old line\n" +
		"+new line\n" +
		" context two\n"
	out, why := Run(gitDiffFilter{}, Input{Stdout: []byte(body), Profile: "aggressive"})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{"diff --git", "@@ -1,4 +1,4 @@", "-old line", "+new line"} {
		if !strings.Contains(text, want) {
			t.Errorf("lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "context one") || !out.Lossy {
		t.Fatalf("context not elided: %+v\n%s", out, text)
	}
}

func TestGoTestFailureOnly(t *testing.T) {
	body := "=== RUN   TestPass\n=== PAUSE TestPass\n=== CONT  TestPass\n--- PASS: TestPass (0.00s)\n" +
		"=== RUN   TestFail\n" +
		"    fail_test.go:10: expected 42, got 41\n" +
		"--- FAIL: TestFail (0.00s)\n" +
		"    fail_test.go:11: stack detail\n" +
		"FAIL\n" +
		"FAIL\texample.com/p\t0.01s\n" +
		"ok  \texample.com/ok\t0.01s\n"
	out, why := Run(goTestFilter{aggressive: true}, Input{ExitCode: 1, Stdout: []byte(body), Profile: "aggressive"})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	for _, want := range []string{"--- FAIL: TestFail", "expected 42, got 41", "stack detail", "FAIL\texample.com/p"} {
		if !strings.Contains(text, want) {
			t.Errorf("lost %q:\n%s", want, text)
		}
	}
	for _, gone := range []string{"TestPass", "=== RUN"} {
		if strings.Contains(text, gone) {
			t.Errorf("passing detail survived: %q", gone)
		}
	}
}

func TestGoTestJSONFailure(t *testing.T) {
	body := strings.Join([]string{
		`{"Action":"start","Package":"p"}`,
		`{"Action":"run","Package":"p","Test":"TestPass"}`,
		`{"Action":"pass","Package":"p","Test":"TestPass","Elapsed":0}`,
		`{"Action":"run","Package":"p","Test":"TestFail"}`,
		`{"Action":"output","Package":"p","Test":"TestFail","Output":"boom detail\n"}`,
		`{"Action":"fail","Package":"p","Test":"TestFail","Elapsed":0.01}`,
		`{"Action":"fail","Package":"p","Elapsed":0.02}`,
	}, "\n") + "\n"
	out, why := Run(goTestFilter{json: true, aggressive: true}, Input{ExitCode: 1, Stdout: []byte(body), Profile: "aggressive"})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	if !strings.Contains(text, "FAIL p TestFail") || !strings.Contains(text, "boom detail") {
		t.Fatalf("candidate = %q", text)
	}
	if strings.Contains(text, "TestPass") {
		t.Fatalf("passing test survived: %q", text)
	}
}

func TestGitDiffTruncatesChangedLines(t *testing.T) {
	var b strings.Builder
	b.WriteString("diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,150 +1,150 @@\n")
	for i := 0; i < 150; i++ {
		fmt.Fprintf(&b, "-old %03d\n", i)
	}
	for i := 0; i < 150; i++ {
		fmt.Fprintf(&b, "+new %03d\n", i)
	}
	out, why := Run(gitDiffFilter{}, Input{Stdout: []byte(b.String()), Profile: "aggressive"})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	if !strings.Contains(text, "... (+150 -50 truncated)") {
		t.Fatalf("missing truncation summary:\n%s", text[max(0, len(text)-120):])
	}
	if strings.Contains(text, "+new 100") || !out.Lossy {
		t.Fatalf("expected capped lossy diff: %+v", out)
	}
}

func TestGoTestAggressiveSuccessSummary(t *testing.T) {
	body := "=== RUN   TestA\n--- PASS: TestA (0.00s)\n=== RUN   TestB\n--- PASS: TestB (0.01s)\nPASS\nok  \texample.com/p\t0.02s\n"
	out, why := Run(goTestFilter{aggressive: true}, Input{ExitCode: 0, Stdout: []byte(body), Profile: "aggressive"})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	if string(out.Text) != "Go test: 2 passed in 1 packages\n" {
		t.Fatalf("candidate = %q", out.Text)
	}
}

func TestGitLogAggressiveCapsAndShortens(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 12; i++ {
		fmt.Fprintf(&b, "commit %040x\nAuthor: A <a@b>\nDate:   Mon Nov 10 12:00:0%d 2025 +0800\n\n    subject %d\n\n    body line that should be elided\n\n", i+1, i%10, i)
	}
	out, why := Run(gitLogFilter{aggressive: true, max: 10}, Input{Stdout: []byte(b.String()), Profile: "aggressive"})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	if strings.Count(text, "\n") > 10 {
		t.Fatalf("expected at most 10 lines:\n%s", text)
	}
	for _, gone := range []string{"Author:", "Date:   ", "body line"} {
		if strings.Contains(text, gone) {
			t.Errorf("elided text survived: %q", gone)
		}
	}
	if !strings.Contains(text, "subject 0") || !strings.Contains(text, "subject 9") {
		t.Errorf("subjects missing:\n%s", text)
	}
	if strings.Contains(text, "subject 10") {
		t.Error("commit beyond cap survived")
	}
	if !out.Lossy {
		t.Fatal("aggressive log must be lossy")
	}
	if got := logLimit([]string{"-n", "3"}); got != 3 {
		t.Fatalf("logLimit -n = %d", got)
	}
}

func TestPytestAggressiveSummary(t *testing.T) {
	out, why := Run(pytestFilter{aggressive: true}, Input{ExitCode: 1, Stdout: []byte(pytestQuiet), Profile: "aggressive"})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	text := string(out.Text)
	if !strings.Contains(text, "Pytest: 2 passed, 1 failed") {
		t.Fatalf("summary missing:\n%s", text)
	}
	for _, want := range []string{"[FAIL] test_x", "assert 1 == 2"} {
		if !strings.Contains(text, want) {
			t.Errorf("lost %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "def test_x") {
		t.Errorf("traceback body survived:\n%s", text)
	}
	if !out.Lossy {
		t.Fatal("aggressive pytest must be lossy")
	}
}

func TestPytestAggressiveSuccess(t *testing.T) {
	body := "..                                                                       [100%]\n2 passed in 0.01s\n"
	out, why := Run(pytestFilter{aggressive: true}, Input{ExitCode: 0, Stdout: []byte(body), Profile: "aggressive"})
	if why != "" {
		t.Fatalf("fallback: %s", why)
	}
	if strings.TrimSpace(string(out.Text)) != "Pytest: 2 passed, 0 failed in 0.01s" {
		t.Fatalf("candidate = %q", out.Text)
	}
}
