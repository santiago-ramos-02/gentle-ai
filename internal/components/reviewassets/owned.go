package reviewassets

import (
	"path/filepath"

	"github.com/gentleman-programming/gentle-ai/v4/internal/model"
)

// NativeAgentOwned reports whether the native agent file at path is still
// exactly what Gentle AI installed there, as its ownership ledger recorded.
// A file the user changed, or one the ledger does not know, is not owned.
func NativeAgentOwned(agent model.AgentID, path string) (bool, error) {
	data, exists, err := nativeFile(path)
	if err != nil || !exists {
		return false, err
	}
	ledger, found, err := readOwnership(ledgerPath(filepath.Dir(path)), NativeAgentFileNames(agent))
	if err != nil || !found {
		return false, err
	}
	recorded, known := ledger.Files[filepath.Base(path)]
	return known && installedHash(data) == recorded, nil
}

// OwnershipLedgerPath is where the native agent installer records what it owns
// in an agents directory.
func OwnershipLedgerPath(dir string) string { return ledgerPath(dir) }
