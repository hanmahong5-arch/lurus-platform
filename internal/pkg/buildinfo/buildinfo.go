// Package buildinfo exposes the build-time provenance of the running binary
// (short commit SHA, build timestamp) so operators can correlate a crash
// / request / trace with an exact ghcr.io image tag.
//
// The CI pipeline injects values via ldflags:
//
//	go build -ldflags "-X github.com/hanmahong5-arch/lurus-platform/internal/pkg/buildinfo.sha=<sha7> \
//	                   -X github.com/hanmahong5-arch/lurus-platform/internal/pkg/buildinfo.built=<rfc3339>"
//
// When the ldflags are absent (local `go run .`, `go test`) the package
// falls back to runtime/debug.ReadBuildInfo for the VCS revision Git
// embedded into the binary, which gives a usable SHA for developer builds
// without any build-system plumbing.
package buildinfo

import (
	"runtime/debug"
	"sync"
)

// Default values for builds without ldflags. The CI workflow overrides `sha`
// with the short commit hash; `built` with the ISO 8601 build timestamp.
var (
	sha   = ""
	built = ""
)

// placeholder when neither ldflags nor VCS info are available. Distinct
// from empty string so logs / /metrics can distinguish "unknown build" from
// "binary only contains a space" etc.
const unknown = "unknown"

var (
	once   sync.Once
	cached Info
)

// Info is a small value type so callers can pass it around without
// touching package-level state.
type Info struct {
	// SHA is the short (7-char) commit hash the binary was built from.
	// Matches the :main-<sha7> immutable image tag pushed by CI.
	SHA string
	// BuiltAt is the RFC 3339 UTC timestamp of the build, or "unknown".
	BuiltAt string
}

// Get returns the build provenance, computed once on first call.
func Get() Info {
	once.Do(func() {
		cached = resolve()
	})
	return cached
}

func resolve() Info {
	out := Info{SHA: sha, BuiltAt: built}

	// If ldflags did not inject a SHA, fall back to VCS info that Go
	// automatically embeds for git-managed builds.
	if out.SHA == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				switch s.Key {
				case "vcs.revision":
					if len(s.Value) >= 7 {
						out.SHA = s.Value[:7]
					} else {
						out.SHA = s.Value
					}
				case "vcs.time":
					if out.BuiltAt == "" {
						out.BuiltAt = s.Value
					}
				}
			}
		}
	}

	if out.SHA == "" {
		out.SHA = unknown
	}
	if out.BuiltAt == "" {
		out.BuiltAt = unknown
	}
	return out
}
