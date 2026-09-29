package uninstall

import (
	"os"
	"path/filepath"

	"github.com/gentleman-programming/gentle-ai/v4/internal/agents"
	"github.com/gentleman-programming/gentle-ai/v4/internal/components/reviewassets"
)

// agentOnlySections are the instruction sections Gentle AI writes for an agent
// itself rather than for a component: the orchestrator and routing guidance
// every configured agent receives, the retired SDD orchestrator, and the
// CodeGraph guidance. Only removing Gentle AI from the agent entirely takes
// them out.
var agentOnlySections = []string{"orchestrator", "agent-routing", "sdd-orchestrator", "codegraph-guidance"}

// fullAgentOperations undoes what installing Gentle AI into an agent adds
// outside its components: the guidance sections in its instructions file and
// the native review and Judgment Day agents. Native agents the user changed
// are kept, as the installer keeps them.
func fullAgentOperations(adapter agents.Adapter, homeDir string) ([]operation, []string) {
	ops := []operation{}
	targets := []string{}
	if adapter.SupportsSystemPrompt() {
		path := adapter.SystemPromptFile(homeDir)
		targets = append(targets, path)
		ops = append(ops, rewriteMarkdownFile(path, func(content string) (string, bool) {
			return removeMarkdownSections(content, agentOnlySections...)
		}))
	}
	if dir := adapter.SubAgentsDir(homeDir); dir != "" && reviewassets.NativeAgentsSupported(adapter.Agent()) {
		targets = append(targets, reviewassets.OwnershipLedgerPath(dir))
		for _, name := range reviewassets.NativeAgentFileNames(adapter.Agent()) {
			path := filepath.Join(dir, name)
			targets = append(targets, path)
			ops = append(ops, removeOwnedNativeAgent(adapter, path))
		}
	}
	return ops, targets
}

// removeOwnedNativeAgent removes a native agent file only while it still holds
// the bytes Gentle AI installed, reading the ledger next to the file.
func removeOwnedNativeAgent(adapter agents.Adapter, path string) operation {
	agent := adapter.Agent()
	return operation{
		typeID: opRemoveFile,
		path:   path,
		apply: func(path string) (bool, bool, error) {
			owned, err := reviewassets.NativeAgentOwned(agent, path)
			if err != nil || !owned {
				return false, false, err
			}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				return false, false, err
			}
			return true, true, nil
		},
	}
}
