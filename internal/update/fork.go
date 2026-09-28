package update

import "strings"

// IsForkBuild reports whether version names a T3 fork build, such as 3.7.0-t3.4816ea6.
// A fork build updates through the fork's own releases, so gentle-ai treats it like a
// source build and never replaces it with an upstream release, which would drop the
// fork's additions.
func IsForkBuild(version string) bool {
	return strings.Contains(version, "-t3.")
}
