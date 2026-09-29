package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v4/internal/system"
)

// checkForkRelease runs the gentle-ai check for version against a fake GitHub whose
// latest release is tag, with the build's fork repository set to repository.
func checkForkRelease(t *testing.T, repository, version, tag string) UpdateResult {
	t.Helper()
	mockNoHomebrew(t)
	var requested string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(githubRelease{TagName: tag, HTMLURL: "https://example.test/release"})
	}))
	t.Cleanup(server.Close)

	origClient, origLookPath, origExecCommand, origTools, origRepository := httpClient, lookPath, execCommand, Tools, forkRepository
	t.Cleanup(func() {
		httpClient, lookPath, execCommand, Tools, forkRepository = origClient, origLookPath, origExecCommand, origTools, origRepository
	})
	httpClient = server.Client()
	httpClient.Transport = &testTransport{server: server}
	Tools = []ToolInfo{Tools[0]}
	forkRepository = repository
	lookPath = func(string) (string, error) { return "", fmt.Errorf("not found") }
	execCommand = func(name string, args ...string) *exec.Cmd { return mockCmd("false") }

	results := CheckAll(context.Background(), version, system.PlatformProfile{OS: "windows", Supported: true})
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if repository != "" && requested != "/repos/"+repository+"/releases/latest" {
		t.Fatalf("checked %q, want the fork's latest release", requested)
	}
	return results[0]
}

func TestCheckAll_ForkBuildUpdatesFromTheForksReleases(t *testing.T) {
	result := checkForkRelease(t, "friend/gentle-ai", "3.7.0-t3.aaaaaaa", "v3.7.0-t3.bbbbbbb")
	if result.Status != UpdateAvailable || result.LatestVersion != "3.7.0-t3.bbbbbbb" {
		t.Fatalf("result = %+v, want the fork release available", result)
	}
	if result.Tool.InstallMethod != InstallForkRelease || result.Tool.Owner != "friend" || result.Tool.Repo != "gentle-ai" {
		t.Fatalf("tool = %+v, want a fork-release upgrade from friend/gentle-ai", result.Tool)
	}

	current := checkForkRelease(t, "friend/gentle-ai", "3.7.0-t3.bbbbbbb", "v3.7.0-t3.bbbbbbb")
	if current.Status != UpToDate {
		t.Fatalf("status = %q, want up to date on the latest fork release", current.Status)
	}
}

// A fork build made outside the fork's release workflow has nowhere to update from,
// and is never offered an upstream release, even a newer one.
func TestCheckAll_ForkBuildWithoutRepositoryIsADevBuild(t *testing.T) {
	result := checkForkRelease(t, "", "3.7.0-t3.aaaaaaa", "v9.0.0")
	if result.Status != DevBuild {
		t.Fatalf("status = %q, want %q", result.Status, DevBuild)
	}
	if IsForkBuild("3.7.0") || IsForkBuild("3.8.0-rc.1") {
		t.Fatal("upstream versions must not count as fork builds")
	}
}
