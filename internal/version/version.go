// Package version holds the build version injected with -ldflags.
package version

// Version is overridden at build time:
//
//	go build -ldflags "-X github.com/nebulacsf/ncsf-sentinel/internal/version.Version=v1.2.3"
var Version = "0.1.0"
