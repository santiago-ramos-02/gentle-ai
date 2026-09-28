package upgrade

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v3/internal/system"
	"github.com/gentleman-programming/gentle-ai/v3/internal/update"
)

const forkTestVersion = "3.7.0-t3.bbbbbbb"

func forkArchive(t *testing.T, binaryName string, content []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	gz := gzip.NewWriter(&buffer)
	tw := tar.NewWriter(gz)
	header := &tar.Header{Name: "gentle-ai_dir/" + binaryName, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(content); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// serveForkRelease points the fork upgrade at a fake release holding checksums and
// archive, and at executable as the running binary.
func serveForkRelease(t *testing.T, executable, checksums string, archive []byte) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Base(r.URL.Path) == "checksums.txt" {
			w.Write([]byte(checksums))
			return
		}
		w.Write(archive)
	}))
	t.Cleanup(server.Close)
	origExecutable, origURL := forkExecutableFn, forkReleaseURL
	t.Cleanup(func() { forkExecutableFn, forkReleaseURL = origExecutable, origURL })
	forkExecutableFn = func() (string, error) { return executable, nil }
	forkReleaseURL = func(owner, repo, version, asset string) string {
		return fmt.Sprintf("%s/%s/%s/v%s/%s", server.URL, owner, repo, version, asset)
	}
}

func forkUpgrade(goos string) error {
	result := update.UpdateResult{
		Tool:          update.ToolInfo{Name: "gentle-ai", Owner: "friend", Repo: "gentle-ai", InstallMethod: update.InstallForkRelease},
		LatestVersion: forkTestVersion,
	}
	_, err := runStrategy(context.Background(), result, system.PlatformProfile{OS: goos})
	return err
}

// A fork build replaces itself with the fork release's binary for its platform,
// including on Windows, where the running binary is moved aside first.
func TestForkReleaseUpgradeReplacesTheRunningBinary(t *testing.T) {
	for _, goos := range []string{"linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			binaryName := "gentle-ai"
			if goos == "windows" {
				binaryName = "gentle-ai.exe"
			}
			archiveName := fmt.Sprintf("gentle-ai_%s_%s_%s.tar.gz", forkTestVersion, goos, runtime.GOARCH)
			archive := forkArchive(t, binaryName, []byte("new build"))
			digest := sha256.Sum256(archive)
			executable := filepath.Join(t.TempDir(), binaryName)
			if err := os.WriteFile(executable, []byte("old build"), 0o755); err != nil {
				t.Fatal(err)
			}
			serveForkRelease(t, executable, fmt.Sprintf("%s  %s\n", hex.EncodeToString(digest[:]), archiveName), archive)

			if err := forkUpgrade(goos); err != nil {
				t.Fatalf("upgrade: %v", err)
			}
			if got, _ := os.ReadFile(executable); string(got) != "new build" {
				t.Fatalf("binary holds %q, want the new build", got)
			}
			if _, err := os.Stat(executable + ".old"); !os.IsNotExist(err) {
				t.Fatalf("the replaced binary was left behind: %v", err)
			}
		})
	}
}

// An archive that does not match the release's checksums is never installed.
func TestForkReleaseUpgradeRejectsATamperedArchive(t *testing.T) {
	archiveName := fmt.Sprintf("gentle-ai_%s_linux_%s.tar.gz", forkTestVersion, runtime.GOARCH)
	executable := filepath.Join(t.TempDir(), "gentle-ai")
	if err := os.WriteFile(executable, []byte("old build"), 0o755); err != nil {
		t.Fatal(err)
	}
	serveForkRelease(t, executable, fmt.Sprintf("%064d  %s\n", 0, archiveName), forkArchive(t, "gentle-ai", []byte("tampered")))

	if err := forkUpgrade("linux"); err == nil {
		t.Fatal("a tampered archive was accepted")
	}
	if got, _ := os.ReadFile(executable); string(got) != "old build" {
		t.Fatalf("binary holds %q, want the old build untouched", got)
	}
}
