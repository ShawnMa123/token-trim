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
