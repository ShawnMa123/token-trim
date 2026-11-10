// Package buildinfo exposes version data for the rtk-go binary.
package buildinfo

import "runtime"

// Version is the release version. It can be overridden at build time with
//
//	go build -ldflags "-X github.com/ShawnMa123/token-trim/go/internal/buildinfo.Version=1.2.3"
var Version = "0.1.0"

// Commit is the source revision, injected with -ldflags when available.
var Commit = "unknown"

// GoVersion returns the Go toolchain used to build the binary.
func GoVersion() string { return runtime.Version() }

// OS returns the build target operating system.
func OS() string { return runtime.GOOS }

// Arch returns the build target architecture.
func Arch() string { return runtime.GOARCH }
