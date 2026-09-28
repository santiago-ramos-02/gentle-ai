package update

import (
	"context"
	"fmt"
	"strings"
)

// InstallForkRelease upgrades a fork build from the fork's own GitHub releases.
const InstallForkRelease InstallMethod = "fork-release"

// forkRepository is the "owner/repo" whose releases a fork build updates from. The
// fork's release workflow sets it:
//
//	-X github.com/gentleman-programming/gentle-ai/v3/internal/update.forkRepository=owner/repo
//
// Fork builds made anywhere else leave it empty and never update themselves.
var forkRepository = ""

// IsForkBuild reports whether version names a T3 fork build, such as 3.7.0-t3.4816ea6.
// A fork build updates only from the fork's releases, never from upstream's, which
// would drop the fork's additions.
func IsForkBuild(version string) bool {
	return strings.Contains(version, "-t3.")
}

// ForkRepository returns the repository a fork build updates from, if it has one.
func ForkRepository() (owner, repo string, ok bool) {
	owner, repo, ok = strings.Cut(forkRepository, "/")
	return owner, repo, ok && owner != "" && repo != ""
}

// checkForkBuild answers the gentle-ai check for a fork build from the fork's latest
// release: any other version there is newer, since each release is a later commit.
// It does not apply to other tools, upstream builds, or fork builds without a
// repository, which the regular check reports as dev builds.
func checkForkBuild(ctx context.Context, tool ToolInfo, currentVersion string) (UpdateResult, bool) {
	owner, repo, ok := ForkRepository()
	if tool.Name != "gentle-ai" || !IsForkBuild(currentVersion) || !ok {
		return UpdateResult{}, false
	}
	tool.Owner, tool.Repo, tool.InstallMethod, tool.GoImportPath = owner, repo, InstallForkRelease, ""
	result := UpdateResult{
		Tool:             tool,
		InstalledVersion: currentVersion,
		UpdateHint:       fmt.Sprintf("Download it from https://github.com/%s/%s/releases/latest", owner, repo),
	}
	release, err := fetchLatestRelease(ctx, owner, repo)
	if err != nil {
		result.Status, result.Err = CheckFailed, err
		return result, true
	}
	result.LatestVersion = strings.TrimPrefix(release.TagName, "v")
	result.ReleaseURL = release.HTMLURL
	if result.LatestVersion == currentVersion {
		result.Status = UpToDate
	} else {
		result.Status = UpdateAvailable
	}
	return result, true
}
