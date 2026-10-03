package api

import (
	"context"
	"io"
	"os/exec"
	"slices"

	"github.com/gentleman-programming/gentle-ai/v4/internal/components/communitytool"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/service"
)

// toolAgentStatusPending is a tool set up for an agent that cannot confirm it is active.
const toolAgentStatusPending = "pending"

type toolAgentStatus struct {
	Agent      string `json:"agent"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Detected   bool   `json:"detected"`
	Configured bool   `json:"configured"`
	Path       string `json:"path,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type toolInfo struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	RepoURL      string            `json:"repoUrl"`
	CLIAvailable bool              `json:"cliAvailable"`
	CLIPath      string            `json:"cliPath,omitempty"`
	Agents       []toolAgentStatus `json:"agents"`
	FollowUps    []string          `json:"followUps"`
	Installed    bool              `json:"installed"`
}

type toolsResult struct {
	Tools []toolInfo `json:"tools"`
}

type toolsListParams struct {
	// Cwd is accepted for symmetry with tools.install; status is global.
	Cwd string `json:"cwd"`
}

// listTools reports each community tool's CLI and per-agent wiring status,
// and whether the installer selection records it.
func listTools(_ context.Context, env *env, params toolsListParams) (any, error) {
	if params.Cwd != "" {
		if err := requireCwd(params.Cwd); err != nil {
			return nil, err
		}
	}
	current, err := readState(env.deps.HomeDir)
	if err != nil {
		return nil, err
	}
	statuses := service.CommunityToolStatuses(func(id model.CommunityToolID) communitytool.Status {
		return env.deps.CommunityToolStatus(id, env.deps.HomeDir)
	})
	tools := make([]toolInfo, 0, len(statuses))
	for _, status := range statuses {
		definition, _ := communitytool.DefinitionFor(status.Tool)
		info := toolInfo{
			ID:           string(status.Tool),
			Name:         definition.Name,
			Description:  definition.Description,
			RepoURL:      definition.RepoURL,
			CLIAvailable: status.CLI == communitytool.AvailabilityAvailable,
			CLIPath:      status.CLIPath,
			Agents:       make([]toolAgentStatus, 0, len(status.Agents)),
			FollowUps:    orEmpty(status.FollowUps),
			Installed:    slices.Contains(current.CommunityTools, string(status.Tool)),
		}
		for _, agent := range status.Agents {
			entry := toolAgentStatus{
				Agent: string(agent.Agent), Name: agent.Name, Status: string(agent.Status),
				Detected: agent.Detected, Configured: agent.Configured, Path: agent.Path, Reason: agent.Reason,
			}
			// Upstream counts a set-up Pi integration as missing while Pi cannot confirm its
			// MCP adapter loaded it; reporting it as pending keeps hosts from offering a setup
			// that is already done.
			if agent.Status == communitytool.AgentStatusMissing && agent.Reason == communitytool.ErrPiCodeGraphAdapterHealthUnavailable.Error() {
				entry.Status = toolAgentStatusPending
				entry.Reason = "set up and its MCP server answers, but Pi cannot confirm its MCP adapter loaded it"
			}
			info.Agents = append(info.Agents, entry)
		}
		tools = append(tools, info)
	}
	return toolsResult{Tools: tools}, nil
}

type toolsInstallParams struct {
	IDs []string `json:"ids"`
	Cwd string   `json:"cwd"`
}

type toolInstallResult struct {
	ID            string   `json:"id"`
	CommandsRun   []string `json:"commandsRun"`
	ManualActions []string `json:"manualActions"`
}

type toolsInstallResult struct {
	Results []toolInstallResult `json:"results"`
}

// installTools installs community tools for the project in cwd, streaming
// their command output as log events, and records them in the installer
// selection so sync keeps their guidance current.
func installTools(_ context.Context, env *env, params toolsInstallParams) (any, error) {
	if err := requireCwd(params.Cwd); err != nil {
		return nil, err
	}
	if len(params.IDs) == 0 {
		return nil, invalidParams("ids must name at least one community tool")
	}
	ids, err := communityToolIDs(params.IDs)
	if err != nil {
		return nil, err
	}
	output := env.events.logWriter()
	runner := env.deps.CommunityToolRunner(params.Cwd, output)
	results, err := service.InstallCommunityTools(ids, func(id model.CommunityToolID) (communitytool.Result, error) {
		return env.deps.InstallCommunityTool(id, params.Cwd, env.deps.HomeDir, runner)
	})
	output.Flush()
	if err != nil {
		return nil, errorf(CodeFailed, "install community tools (%d of %d done): %v", len(results), len(ids), err)
	}
	if err := service.RecordCommunityTools(env.deps.HomeDir, ids); err != nil {
		return nil, errorf(CodeFailed, "community tools installed but not recorded: %v", err)
	}
	out := make([]toolInstallResult, 0, len(results))
	for _, result := range results {
		out = append(out, toolInstallResult{ID: string(result.Tool), CommandsRun: orEmpty(result.CommandsRun), ManualActions: orEmpty(result.ManualActions)})
	}
	return toolsInstallResult{Results: out}, nil
}

// commandRunner runs community tool commands in the project directory and
// streams their combined output.
type commandRunner struct {
	dir    string
	output io.Writer
}

func newCommandRunner(dir string, output io.Writer) communitytool.Runner {
	return commandRunner{dir: dir, output: output}
}

func (r commandRunner) Run(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = r.dir
	cmd.Stdout, cmd.Stderr = r.output, r.output
	return cmd.Run()
}
