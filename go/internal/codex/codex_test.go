package codex

import (
	"strings"
	"testing"
)

func TestAnalyzeRewritesSimpleQueries(t *testing.T) {
	r := Analyze("powershell", "git status", `C:\tools\rtk-go.exe`, false)
	if r.Decision != DecisionRewrite {
		t.Fatalf("decision = %+v", r)
	}
	if !strings.Contains(r.Command, "exec -- git status") || !strings.HasPrefix(r.Command, "& '") {
		t.Fatalf("command = %q", r.Command)
	}
	r = Analyze("posix", "rg -n 'a b' src", "/opt/rtk go/rtk-go", false)
	if r.Decision != DecisionRewrite || !strings.HasPrefix(r.Command, `'/opt/rtk go/rtk-go' exec -- rg -n 'a b' src`) {
		t.Fatalf("command = %q", r.Command)
	}
	// Quoted glob/regex metacharacters are data, not shell syntax.
	r = Analyze("posix", `rg "foo.*" src`, "/bin/rtk-go", false)
	if r.Decision != DecisionRewrite {
		t.Fatalf("quoted pattern: %+v", r)
	}
}

func TestAnalyzePassthrough(t *testing.T) {
	cases := []string{
		"git status | cat",
		"rg foo > out.txt",
		"git status && echo done",
		"git -c core.pager=cat status",
		"git log --oneline",
		"go test ./...",
		"rg --pre ./hook x",
		"rg foo.*",
		"rg '$HOME' x",
		"rtk-go exec -- git status",
		"Get-Content x",
		"",
		"git status; rm -rf /",
	}
	for _, cmd := range cases {
		r := Analyze("posix", cmd, "/bin/rtk-go", false)
		if r.Decision != DecisionPassthrough {
			t.Errorf("Analyze(%q) = %+v, want passthrough", cmd, r)
		}
	}
}

func TestAnalyzeShellQuoting(t *testing.T) {
	r := Analyze("powershell", "git status", `C:\Program Files\rtk-go.exe`, false)
	if !strings.Contains(r.Command, `& 'C:\Program Files\rtk-go.exe' exec -- `) {
		t.Fatalf("command = %q", r.Command)
	}
	r = Analyze("posix", "git status", `/opt/it's/rtk-go`, false)
	if !strings.Contains(r.Command, `'/opt/it'\''s/rtk-go'`) {
		t.Fatalf("command = %q", r.Command)
	}
}

func TestTokenize(t *testing.T) {
	got, ok := tokenize(`git status "a b" 'c d'`, "posix")
	if !ok || len(got) != 4 || got[2] != "a b" || got[3] != "c d" {
		t.Fatalf("tokens = %q ok=%v", got, ok)
	}
	if _, ok := tokenize(`git status "unclosed`, "posix"); ok {
		t.Fatal("unclosed quote accepted")
	}
	if _, ok := tokenize(`git status $(whoami)`, "posix"); ok {
		t.Fatal("substitution accepted")
	}
}
