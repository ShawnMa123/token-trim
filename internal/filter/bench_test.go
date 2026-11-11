package filter

import (
	"strings"
	"testing"
)

func benchStatusInput(size int) []byte {
	var b strings.Builder
	b.WriteString("On branch main\n")
	b.WriteString("Changes not staged for commit:\n")
	b.WriteString("  (use \"git add <file>...\" to update what will be committed)\n")
	line := "modified:   src/some/deep/path/file_name.go\n"
	for b.Len() < size {
		b.WriteString("\t")
		b.WriteString(line)
	}
	return []byte(b.String())
}

func BenchmarkGitStatus1MiB(b *testing.B) {
	in := Input{Argv: []string{"git", "status"}, Stdout: benchStatusInput(1 << 20)}
	b.SetBytes(int64(len(in.Stdout)))
	b.ResetTimer()
	for range b.N {
		if _, err := (gitStatusFilter{}).Apply(in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRG1MiB(b *testing.B) {
	var sb strings.Builder
	for sb.Len() < 1<<20 {
		sb.WriteString("src/deep/path/file_name.go:120:7:some matched source text here\n")
	}
	in := Input{Stdout: []byte(sb.String())}
	b.SetBytes(int64(len(in.Stdout)))
	b.ResetTimer()
	for range b.N {
		if _, err := (rgFilter{}).Apply(in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGitStatusAggressive1MiB(b *testing.B) {
	in := Input{Argv: []string{"git", "status"}, Stdout: benchStatusInput(1 << 20), Profile: "aggressive"}
	b.SetBytes(int64(len(in.Stdout)))
	b.ResetTimer()
	for range b.N {
		if _, err := (gitStatusFilter{compact: true}).Apply(in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRGAggressive1MiB(b *testing.B) {
	var sb strings.Builder
	for sb.Len() < 1<<20 {
		sb.WriteString("src/deep/path/file_name.go:120:7:some matched source text here that is long enough to truncate\n")
	}
	in := Input{Stdout: []byte(sb.String()), Profile: "aggressive"}
	b.SetBytes(int64(len(in.Stdout)))
	b.ResetTimer()
	for range b.N {
		if _, err := (rgFilter{aggressive: true}).Apply(in); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGitDiffAggressive1MiB(b *testing.B) {
	var sb strings.Builder
	sb.WriteString("diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1,99999 +1,99999 @@\n")
	i := 0
	for sb.Len() < 1<<20 {
		if i%2 == 0 {
			sb.WriteString("-old line with some payload\n")
		} else {
			sb.WriteString("+new line with some payload\n")
		}
		i++
	}
	in := Input{Stdout: []byte(sb.String()), Profile: "aggressive"}
	b.SetBytes(int64(len(in.Stdout)))
	b.ResetTimer()
	for range b.N {
		if _, err := (gitDiffFilter{}).Apply(in); err != nil {
			b.Fatal(err)
		}
	}
}
