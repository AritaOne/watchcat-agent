// Package buildinfo carries version metadata stamped in at link time.
package buildinfo

import (
	"fmt"
	"runtime"
)

// Overridden via -ldflags "-X github.com/aritaone/watchcat-agent/internal/buildinfo.Version=..."
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

// String returns a single-line human readable build description.
func String() string {
	return fmt.Sprintf("watchcat-agent %s (commit %s, built %s, %s, %s/%s)",
		Version, Commit, BuildDate, runtime.Version(), runtime.GOOS, runtime.GOARCH)
}
