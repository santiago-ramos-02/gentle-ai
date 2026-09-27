package service

import (
	"errors"
	"os"

	"github.com/gentleman-programming/gentle-ai/v3/internal/state"
	"github.com/gentleman-programming/gentle-ai/v3/internal/statecoord"
)

// MarkPendingSync records that the next gentle-ai launch must sync, after a
// self-upgrade left the running binary stale. It re-reads the latest state
// inside the install-state lock so a concurrent writer's changes survive, and
// never writes over an existing state file it cannot read.
func MarkPendingSync(homeDir string) error {
	return statecoord.WithLock(homeDir, func() error {
		s, readErr := state.Read(homeDir)
		if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
			return nil
		}
		s.PendingSync = true
		return state.Write(homeDir, s)
	})
}
