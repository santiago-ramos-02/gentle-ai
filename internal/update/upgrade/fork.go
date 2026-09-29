package upgrade

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
	"github.com/gentleman-programming/gentle-ai/v4/internal/update"
)

// forkExecutableFn resolves the binary a fork upgrade replaces: the running one,
// which is also the one hosts such as T3 Code call by path.
var forkExecutableFn = os.Executable

// forkReleaseURL is where a fork release's assets are downloaded from.
var forkReleaseURL = func(owner, repo, version, asset string) string {
	return fmt.Sprintf("https://github.com/%s/%s/releases/download/v%s/%s", owner, repo, version, asset)
}

// forkReleaseUpgrade replaces a fork build with the fork's latest release, on every
// platform. The archive must match the release's checksums.txt; both come from the
// fork's GitHub releases over HTTPS, which is what a fork build trusts.
func forkReleaseUpgrade(ctx context.Context, r update.UpdateResult, profile system.PlatformProfile) error {
	binaryName := "gentle-ai"
	if profile.OS == "windows" {
		binaryName = "gentle-ai.exe"
	}
	archiveName := fmt.Sprintf("gentle-ai_%s_%s_%s.tar.gz", r.LatestVersion, profile.OS, runtime.GOARCH)
	asset := func(name string) string { return forkReleaseURL(r.Tool.Owner, r.Tool.Repo, r.LatestVersion, name) }

	executable, err := forkExecutableFn()
	if err != nil {
		return fmt.Errorf("locate the running gentle-ai: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	// A previous Windows upgrade leaves the replaced binary behind while it still runs.
	_ = os.Remove(executable + ".old")

	checksums, err := fetchChecksums(ctx, asset("checksums.txt"))
	if err != nil {
		return fmt.Errorf("fetch checksums.txt: %w", err)
	}
	expectedDigest, err := expectedChecksumFor(checksums, archiveName)
	if err != nil {
		return fmt.Errorf("checksum verification failed: %w", err)
	}
	tmpDir, err := os.MkdirTemp("", "gentle-ai-upgrade-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)
	archivePath := filepath.Join(tmpDir, archiveName)
	actualDigest, err := downloadToFile(ctx, asset(archiveName), archivePath, maxReleaseArchiveBytes)
	if err != nil {
		return fmt.Errorf("download %s: %w", archiveName, err)
	}
	if actualDigest != expectedDigest {
		return fmt.Errorf("checksum mismatch for %s:\n  expected: %s\n  got:      %s", archiveName, expectedDigest, actualDigest)
	}

	staged := executable + ".new"
	defer os.Remove(staged)
	archive, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer archive.Close()
	if err := extractBinaryFromTarGz(archive, binaryName, staged); err != nil {
		return fmt.Errorf("extract gentle-ai: %w", err)
	}

	if profile.OS != "windows" {
		return atomicReplace(staged, executable)
	}
	// Windows cannot replace a running binary, but it can rename one, so the running
	// binary moves aside and the new one takes its name.
	old := executable + ".old"
	if err := renameFn(executable, old); err != nil {
		return fmt.Errorf("move the running gentle-ai aside: %w", err)
	}
	if err := atomicReplace(staged, executable); err != nil {
		_ = renameFn(old, executable)
		return err
	}
	_ = os.Remove(old)
	return nil
}
