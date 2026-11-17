// Package version reports the build version, taken from the latest Git tag.
package version

import "runtime/debug"

const devVersion = "dev"

// String returns the version: the generated Git tag, else the tag Go stamped
// into the build, else "dev".
func String() string {
	if gitVersion != "" && gitVersion != devVersion {
		return gitVersion
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.tag" && s.Value != "" {
				return s.Value
			}
		}
	}
	return devVersion
}
