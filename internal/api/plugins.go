package api

import (
	"context"
	"slices"

	"github.com/gentleman-programming/gentle-ai/v3/internal/components/opencodeplugin"
	"github.com/gentleman-programming/gentle-ai/v3/internal/model"
	"github.com/gentleman-programming/gentle-ai/v3/internal/service"
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

const openCodeMissingReason = "OpenCode was not detected on this machine"

// listPlugins offers the installable OpenCode community plugins plus any
// installed plugin that is no longer offered, so it can still be removed.
func listPlugins(ctx context.Context, env *env, _ noParams) (any, error) {
	detection, err := env.deps.detect(ctx)
	if err != nil {
		return nil, err
	}
	installed := opencodeplugin.InstalledIDs(env.deps.HomeDir)
	plugins := []pluginInfo{}
	offered := map[model.OpenCodeCommunityPluginID]bool{}
	for _, def := range opencodeplugin.Definitions() {
		offered[def.ID] = true
		plugins = append(plugins, pluginInfo{ID: string(def.ID), Name: def.Name, Description: def.Description, RepoURL: def.RepoURL, Installed: slices.Contains(installed, def.ID)})
	}
	for _, id := range installed {
		if offered[id] {
			continue
		}
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

func requireOpenCode(ctx context.Context, env *env) error {
	detection, err := env.deps.detect(ctx)
	if err != nil {
		return err
	}
	if !service.AgentDetected(detection, model.AgentOpenCode) {
		return errorf(CodeUnsupported, "%s", openCodeMissingReason)
	}
	return nil
}

func installPlugins(ctx context.Context, env *env, params pluginIDsParams) (any, error) {
	if len(params.IDs) == 0 {
		return nil, invalidParams("ids must name at least one plugin")
	}
	ids := make([]model.OpenCodeCommunityPluginID, 0, len(params.IDs))
	for _, raw := range params.IDs {
		id := model.OpenCodeCommunityPluginID(raw)
		if !slices.ContainsFunc(opencodeplugin.Definitions(), func(def opencodeplugin.Definition) bool { return def.ID == id }) {
			return nil, invalidParams("unknown OpenCode plugin %q", raw)
		}
		ids = append(ids, id)
	}
	if err := requireOpenCode(ctx, env); err != nil {
		return nil, err
	}
	results, err := env.deps.InstallPlugins(env.deps.HomeDir, ids)
	if err != nil {
		return nil, errorf(CodeFailed, "install OpenCode plugins (%d of %d registered): %v", len(results), len(ids), err)
	}
	out := make([]pluginInstallResult, 0, len(results))
	for i, result := range results {
		out = append(out, pluginInstallResult{ID: string(ids[i]), Changed: result.Changed, Files: orEmpty(result.Files)})
	}
	return pluginsInstallResult{Results: out}, nil
}

type pluginIDParams struct {
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

// uninstallPlugin removes one installed plugin, like the TUI's uninstall
// shortcut, which only offers what tui.json registers.
func uninstallPlugin(_ context.Context, env *env, params pluginIDParams) (any, error) {
	if params.ID == "" {
		return nil, invalidParams("id is required")
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
