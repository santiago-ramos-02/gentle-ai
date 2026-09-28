package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"testing"

	"github.com/gentleman-programming/gentle-ai/v3/internal/system"
)

// A fork build is never offered an upstream release, even a newer one.
func TestCheckAll_ForkBuildIsNotUpgradedToUpstream(t *testing.T) {
	mockNoHomebrew(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(githubRelease{TagName: "v9.0.0"})
	}))
	defer server.Close()

	origClient, origLookPath, origExecCommand, origTools := httpClient, lookPath, execCommand, Tools
	t.Cleanup(func() {
		httpClient, lookPath, execCommand, Tools = origClient, origLookPath, origExecCommand, origTools
	})
	httpClient = server.Client()
	httpClient.Transport = &testTransport{server: server}
	Tools = []ToolInfo{Tools[0]}
	lookPath = func(string) (string, error) { return "", fmt.Errorf("not found") }
	execCommand = func(name string, args ...string) *exec.Cmd { return mockCmd("false") }

	profile := system.PlatformProfile{OS: "windows", Supported: true}
	results := CheckAll(context.Background(), "3.7.0-t3.4816ea6", profile)
	if len(results) != 1 || results[0].Status != DevBuild {
		t.Fatalf("fork build results = %+v, want one DevBuild", results)
	}
	if IsForkBuild("3.7.0") || IsForkBuild("3.8.0-rc.1") {
		t.Fatal("upstream versions must not count as fork builds")
	}
}
