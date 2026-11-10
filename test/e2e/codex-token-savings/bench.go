// Command bench measures wrapper overhead, peak RSS, and output bytes for the
// same commands under three arms: direct, rtk-go, and original rtk.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"syscall"
	"time"
)

type arm struct {
	name string
	argv []string
}

type benchCase struct {
	name string
	dir  string
	arms []arm
}

type armResult struct {
	Arm         string  `json:"arm"`
	P50MS       float64 `json:"p50_ms"`
	P95MS       float64 `json:"p95_ms"`
	MeanMS      float64 `json:"mean_ms"`
	MaxRSSKB    int64   `json:"max_rss_kb"`
	StdoutBytes int     `json:"stdout_bytes"`
	Failures    int     `json:"failures"`
	Runs        int     `json:"runs"`
}

type caseResult struct {
	Case string      `json:"case"`
	Arms []armResult `json:"arms"`
}

const warmup = 3
const samples = 25

func main() {
	cases := []benchCase{
		{
			name: "git-status",
			dir:  "/work",
			arms: []arm{
				{"direct", []string{"git", "status"}},
				{"rtk-go", []string{"rtk-go", "exec", "--", "git", "status"}},
				{"rtk", []string{"rtk", "git", "status"}},
			},
		},
		{
			name: "rg",
			dir:  "/work",
			arms: []arm{
				{"direct", []string{"rg", "-n", "TODO_MARKER", "src"}},
				{"rtk-go", []string{"rtk-go", "exec", "--", "rg", "-n", "TODO_MARKER", "src"}},
				{"rtk", []string{"rtk", "rg", "-n", "TODO_MARKER", "src"}},
			},
		},
		{
			name: "go-test",
			dir:  "/work",
			arms: []arm{
				{"direct", []string{"go", "test", "-count=1", "-v", "./gen"}},
				{"rtk-go", []string{"rtk-go", "exec", "--", "go", "test", "-count=1", "-v", "./gen"}},
				{"rtk", []string{"rtk", "go", "test", "-count=1", "-v", "./gen"}},
			},
		},
		{
			name: "tiny-git",
			dir:  "/tiny",
			arms: []arm{
				{"direct", []string{"git", "status"}},
				{"rtk-go", []string{"rtk-go", "exec", "--", "git", "status"}},
				{"rtk", []string{"rtk", "git", "status"}},
			},
		},
	}

	// The failure fixture has no search corpus; keep only applicable cases.
	if _, err := os.Stat("/work/src/search0"); err != nil {
		filtered := cases[:0]
		for _, c := range cases {
			if c.name != "rg" {
				filtered = append(filtered, c)
			}
		}
		cases = filtered
	}
	for i := range cases {
		if cases[i].name == "go-test" {
			if _, err := os.Stat("/work/gen"); err != nil {
				for j := range cases[i].arms {
					for k := range cases[i].arms[j].argv {
						if cases[i].arms[j].argv[k] == "./gen" {
							cases[i].arms[j].argv[k] = "./genok"
						}
					}
				}
			}
		}
	}
	if _, err := os.Stat("/work/genfail"); err == nil {
		cases = append(cases,
			benchCase{
				name: "git-diff",
				dir:  "/work",
				arms: []arm{
					{"direct", []string{"git", "diff", "--", "bigdiff.txt"}},
					{"rtk-go", []string{"rtk-go", "exec", "--", "git", "diff", "--", "bigdiff.txt"}},
					{"rtk", []string{"rtk", "git", "diff", "--", "bigdiff.txt"}},
				},
			},
			benchCase{
				name: "go-test-fail",
				dir:  "/work",
				arms: []arm{
					{"direct", []string{"go", "test", "-count=1", "-v", "./genfail"}},
					{"rtk-go", []string{"rtk-go", "exec", "--", "go", "test", "-count=1", "-v", "./genfail"}},
					{"rtk", []string{"rtk", "go", "test", "-count=1", "-v", "./genfail"}},
				},
			},
		)
	}

	out := make([]caseResult, 0, len(cases))
	for _, c := range cases {
		cr := caseResult{Case: c.name}
		for _, a := range c.arms {
			cr.Arms = append(cr.Arms, measure(c.dir, a))
		}
		out = append(out, cr)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func measure(dir string, a arm) armResult {
	env := append(os.Environ(),
		"HOME=/tmp/bench-home",
		"XDG_CACHE_HOME=/tmp/bench-cache",
		"XDG_CONFIG_HOME=/tmp/bench-config",
		"XDG_DATA_HOME=/tmp/bench-data",
		"RTK_GO_ACTIVE=",
	)
	runOne := func() (time.Duration, int64, int, bool) {
		var stdout, stderr bytes.Buffer
		cmd := exec.Command(a.argv[0], a.argv[1:]...)
		cmd.Dir = dir
		cmd.Env = env
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		start := time.Now()
		err := cmd.Run()
		d := time.Since(start)
		var rss int64
		if cmd.ProcessState != nil {
			if ru, ok := cmd.ProcessState.SysUsage().(*syscall.Rusage); ok {
				rss = int64(ru.Maxrss)
			}
		}
		return d, rss, stdout.Len(), err == nil
	}
	for range warmup {
		runOne()
	}
	res := armResult{Arm: a.name, Runs: samples}
	durations := make([]float64, 0, samples)
	var rssMax int64
	outSizes := make([]int, 0, samples)
	for range samples {
		d, rss, n, ok := runOne()
		if !ok {
			res.Failures++
		}
		ms := float64(d.Microseconds()) / 1000.0
		durations = append(durations, ms)
		if rss > rssMax {
			rssMax = rss
		}
		outSizes = append(outSizes, n)
	}
	sort.Float64s(durations)
	res.MeanMS = mean(durations)
	res.P50MS = percentile(durations, 0.50)
	res.P95MS = percentile(durations, 0.95)
	res.MaxRSSKB = rssMax
	res.StdoutBytes = medianInt(outSizes)
	return res
}

func mean(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(p * float64(len(sorted)-1))
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func medianInt(xs []int) int {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]int(nil), xs...)
	sort.Ints(cp)
	return cp[len(cp)/2]
}
