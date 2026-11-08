// Package buildinfo names the running build and its corresponding source.
package buildinfo

import "runtime/debug"

// sourceURL is set explicitly by the release build. A version alone cannot
// identify a fork's source, so local builds do not guess an upstream URL.
var sourceURL string

// SourceURL is the download location of this build's corresponding source.
// Custom distributions must set it to their own complete source archive.
func SourceURL() string { return sourceURL }

// Version names a build: the release, such as "v0.1.0 (1cfab60)", when the
// build set version and commit with -ldflags "-X main.version=… -X
// main.commit=…"; otherwise the module version, or "dev-" and the commit
// of a build from a checkout.
func Version(version, commit string) string {
	if version != "" {
		if len(commit) >= 7 {
			return version + " (" + commit[:7] + ")"
		}
		return version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 12 {
			return "dev-" + s.Value[:12]
		}
	}
	return "dev"
}
