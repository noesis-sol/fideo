package fideo

import "runtime/debug"

// versionString is what --version reports, read from the binary's build info
// (debug.ReadBuildInfo in production) so a release needs no -ldflags step. A
// plain `go build` in a git checkout stamps the main module's version: the tag
// at a tagged commit (v1.1.0), a pseudo-version past it, and a "+dirty" suffix
// when the tree had uncommitted changes. A build without VCS info reports
// "(devel)".
func versionString(info *debug.BuildInfo, ok bool) string {
	if !ok || info == nil {
		return "unknown"
	}
	if info.Main.Version == "" {
		return "(devel)"
	}
	return info.Main.Version
}
