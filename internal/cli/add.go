package cli

import (
	"context"
	"errors"
)

// add tracks a file with chezmoi and registers it in the edit registry.
func (d Deps) add(_ context.Context, args []string) error {
	if _, err := parse(newFlags(cmdAdd), args); err != nil {
		return err
	}
	return errors.New("add: not implemented")
}
