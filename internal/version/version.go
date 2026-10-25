// Package version exposes build metadata. Values can be overridden with
// -ldflags at build time.
package version

var (
	Version = "0.1.0"
	Commit  = "dev"
	Date    = "unknown"
)
