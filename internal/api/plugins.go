package api

import (
	"context"
	"slices"

	"github.com/gentleman-programming/gentle-ai/v4/internal/components/opencodeplugin"
	"github.com/gentleman-programming/gentle-ai/v4/internal/components/piplugin"
	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
	"github.com/gentleman-programming/gentle-ai/v4/internal/service"
)

type pluginInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RepoURL     string `json:"repoUrl,omitempty"`
	Installed   bool   `json:"installed"`
}

type pluginsResult struct {
	Plugins   []pluginInfo `json:"plugins"`
	Supported bool         `json:"supported"`
	Reason    string       `json:"reason,omitempty"`
}

const (
	openCodeMissingReason        = "OpenCode was not detected on this machine"
	openCodePluginsRetiredReason = "OpenCode community plugins are no longer offered; existing configuration is preserved"
	piMissingReason              = "Pi was not detected on this machine"
)

// pluginAgentParams names whose plugins a method is about: "opencode" (the
// default, for callers from before Pi had plugins) or "pi".
type pluginAgentParams struct {
	Agent string `json:"agent"`
}

func (p pluginAgentParams) pi() (bool, error) {
	switch p.Agent {
	case "", string(model.AgentOpenCode):
		return false, nil
	case string(model.AgentPi):
		return true, nil
	default:
		return false, invalidParams("plugins are offered for opencode and pi, not %q", p.Agent)
	}
}

// listPlugins lists existing OpenCode registrations only for removal, or the
// optional Pi packages and which of them Pi declares.
func listPlugins(ctx context.Context, env *env, params pluginAgentParams) (any, error) {
	isPi, err := params.pi()
	if err != nil {
		return nil, err
	}
	detection, err := env.deps.detect(ctx)
	if err != nil {
		return nil, err
	}
	if isPi {
		installed := piplugin.InstalledIDs(env.deps.HomeDir)
		plugins := []pluginInfo{}
		for _, def := range piplugin.Definitions() {
			plugins = append(plugins, pluginInfo{ID: string(def.ID), Name: def.Name, Description: def.Description, RepoURL: def.RepoURL, Installed: slices.Contains(installed, def.ID)})
		}
		result := pluginsResult{Plugins: plugins, Supported: service.AgentDetected(detection, model.AgentPi)}
		if !result.Supported {
			result.Reason = piMissingReason
		}
		return result, nil
	}
	installed := opencodeplugin.InstalledIDs(env.deps.HomeDir)
	plugins := []pluginInfo{}
	for _, id := range installed {
		info := pluginInfo{ID: string(id), Name: string(id), Installed: true}
		if def, ok := opencodeplugin.DefinitionFor(id); ok {
			info.Name, info.Description, info.RepoURL = def.Name, def.Description, def.RepoURL
		}
		plugins = append(plugins, info)
	}
	result := pluginsResult{Plugins: plugins, Supported: service.AgentDetected(detection, model.AgentOpenCode)}
	if !result.Supported {
		result.Reason = openCodeMissingReason
	}
	return result, nil
}

type pluginIDsParams struct {
	pluginAgentParams
	IDs []string `json:"ids"`
}

type pluginInstallResult struct {
	ID      string   `json:"id"`
	Changed bool     `json:"changed"`
	Files   []string `json:"files"`
}

type pluginsInstallResult struct {
	Results []pluginInstallResult `json:"results"`
}

func installPlugins(ctx context.Context, env *env, params pluginIDsParams) (any, error) {
	if len(params.IDs) == 0 {
		return nil, invalidParams("ids must name at least one plugin")
	}
	isPi, err := params.pi()
	if err != nil {
		return nil, err
	}
	if isPi {
		return installPiPlugins(ctx, env, params.IDs)
	}
	for _, raw := range params.IDs {
		id := model.OpenCodeCommunityPluginID(raw)
		if _, known := opencodeplugin.DefinitionFor(id); !known && id != model.OpenCodePluginGentleLogo {
			return nil, invalidParams("unknown OpenCode plugin %q", raw)
		}
	}
	return nil, errorf(CodeUnsupported, "%s", openCodePluginsRetiredReason)
}

type pluginIDParams struct {
	pluginAgentParams
	ID string `json:"id"`
}

type pluginUninstallResult struct {
	PluginID           string   `json:"pluginId"`
	ChangedTUI         bool     `json:"changedTui"`
	ChangedPackageJSON bool     `json:"changedPackageJson"`
	ChangedNodeModules bool     `json:"changedNodeModules"`
	CacheEntryRemoved  string   `json:"cacheEntryRemoved,omitempty"`
	NodeModulesPath    string   `json:"nodeModulesPath,omitempty"`
	TSXPath            string   `json:"tsxPath,omitempty"`
	CleanupPending     []string `json:"cleanupPending"`
}

// uninstallPlugin removes one installed plugin, offering only what its
// agent's settings register.
func uninstallPlugin(_ context.Context, env *env, params pluginIDParams) (any, error) {
	if params.ID == "" {
		return nil, invalidParams("id is required")
	}
	isPi, err := params.pi()
	if err != nil {
		return nil, err
	}
	if isPi {
		return uninstallPiPlugin(env, params.ID)
	}
	id := model.OpenCodeCommunityPluginID(params.ID)
	if !slices.Contains(opencodeplugin.InstalledIDs(env.deps.HomeDir), id) {
		return nil, errorf(CodeNotFound, "OpenCode plugin %q is not installed", params.ID)
	}
	result, err := env.deps.UninstallPlugin(env.deps.HomeDir, id)
	if err != nil {
		return nil, errorf(CodeFailed, "uninstall OpenCode plugin %s: %v", params.ID, err)
	}
	return pluginUninstallResult{
		PluginID:           string(result.PluginID),
		ChangedTUI:         result.ChangedTUI,
		ChangedPackageJSON: result.ChangedPackageJSON,
		ChangedNodeModules: result.ChangedNodeModules,
		CacheEntryRemoved:  result.CacheEntryRemoved,
		NodeModulesPath:    result.NodeModulesPath,
		TSXPath:            result.TSXPath,
		CleanupPending:     orEmpty(result.CleanupPending),
	}, nil
}

func installPiPlugins(ctx context.Context, env *env, raw []string) (any, error) {
	ids, err := piPluginIDs(raw)
	if err != nil {
		return nil, err
	}
	detection, err := env.deps.detect(ctx)
	if err != nil {
		return nil, err
	}
	if !service.AgentDetected(detection, model.AgentPi) {
		return nil, errorf(CodeUnsupported, "%s", piMissingReason)
	}
	before := piplugin.InstalledIDs(env.deps.HomeDir)
	if err := piplugin.Install(env.deps.RunCommand, ids); err != nil {
		return nil, errorf(CodeFailed, "install Pi plugins: %v", err)
	}
	out := make([]pluginInstallResult, 0, len(ids))
	for _, id := range ids {
		out = append(out, pluginInstallResult{ID: string(id), Changed: !slices.Contains(before, id), Files: []string{}})
	}
	return pluginsInstallResult{Results: out}, nil
}

type piPluginUninstallResult struct {
	PluginID string `json:"pluginId"`
}

func uninstallPiPlugin(env *env, raw string) (any, error) {
	id := model.PiPluginID(raw)
	if !slices.Contains(piplugin.InstalledIDs(env.deps.HomeDir), id) {
		return nil, errorf(CodeNotFound, "Pi plugin %q is not installed", raw)
	}
	if err := piplugin.Uninstall(env.deps.RunCommand, id); err != nil {
		return nil, errorf(CodeFailed, "uninstall Pi plugin %s: %v", raw, err)
	}
	return piPluginUninstallResult{PluginID: raw}, nil
}
