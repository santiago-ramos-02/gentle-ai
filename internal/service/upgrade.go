package service

import (
	"errors"
	"os"

	"github.com/gentleman-programming/gentle-ai/v4/internal/state"
	"github.com/gentleman-programming/gentle-ai/v4/internal/statecoord"
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

// ClearPendingSync records that a sync of every agent has run since the last upgrade, as the
// TUI's deferred sync does on launch. Nothing is written when the flag is already clear.
func ClearPendingSync(homeDir string) error {
	return statecoord.WithLock(homeDir, func() error {
		s, err := state.Read(homeDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !s.PendingSync {
			return nil
		}
		s.PendingSync = false
		return state.Write(homeDir, s)
	})
}
