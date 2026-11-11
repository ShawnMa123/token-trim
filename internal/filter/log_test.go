package filter

import (
	"strings"
	"testing"
)

func TestLogFilterFoldsConsecutiveLongLines(t *testing.T) {
	line := "2026-11-11T00:00:00Z INFO worker heartbeat queue=default state=idle attempts=0 latency=12ms"
	in := Input{Stdout: []byte(line + "\n" + line + "\n" + line + "\n"), Profile: "lossless"}
	out, err := logFilter{}.Apply(in)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out.Text)
	if strings.Count(got, line) != 1 || !strings.Contains(got, "[rtk-go ×3]") {
		t.Fatalf("unexpected output:\n%s", got)
	}
	if !out.Lossy || out.RemovedLines != 2 {
		t.Fatalf("lossy=%v removed=%d", out.Lossy, out.RemovedLines)
	}
}

func TestLogFilterKeepsUniqueAndShortRuns(t *testing.T) {
	in := Input{Stdout: []byte("a\nb\nb\nc\n"), Profile: "lossless"}
	if _, err := (logFilter{}).Apply(in); err == nil {
		t.Fatal("short runs must fall back to raw")
	}
}

func TestLogFilterFoldsBlankRuns(t *testing.T) {
	in := Input{Stdout: []byte(strings.Repeat("\n", 40)), Profile: "lossless"}
	out, err := logFilter{}.Apply(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out.Text), "[rtk-go: blank ×40]") {
		t.Fatalf("unexpected output: %q", out.Text)
	}
}

func TestLogFilterRejectsAmbiguousMarkers(t *testing.T) {
	in := Input{Stdout: []byte("x rtk-go ×2\nx rtk-go ×2\n"), Profile: "lossless"}
	if _, err := (logFilter{}).Apply(in); err == nil {
		t.Fatal("marker collision must force raw")
	}
}

func TestLosslessLogSelection(t *testing.T) {
	ok := []struct {
		argv []string
	}{
		{[]string{"cat", "app.log"}},
		{[]string{"tail", "-n", "100", "app.log"}},
		{[]string{"tail", "--lines=100", "app.log"}},
		{[]string{"head", "-n100", "app.log"}},
		{[]string{"cat", "--", "app.log"}},
		{[]string{"tail", "-n", "10", "--", "app.log"}},
	}
	for _, tc := range ok {
		if Select(tc.argv, "lossless") == nil {
			t.Errorf("Select(%q, lossless) = nil", tc.argv)
		}
		if Select(tc.argv, "safe") != nil {
			t.Errorf("Select(%q, safe) should be nil", tc.argv)
		}
	}
	bad := [][]string{
		{"tail", "-f", "app.log"},
		{"head", "-f", "app.log"},
		{"cat", "-n", "app.log"},
		{"tail", "-n", "100"},
		{"cat", "--", "-"},
	}
	for _, argv := range bad {
		if Select(argv, "lossless") != nil {
			t.Errorf("Select(%q, lossless) should be nil", argv)
		}
	}
}
