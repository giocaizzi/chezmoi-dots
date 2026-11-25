package cli

import (
	"context"
	"errors"
)

// status prints the dotfiles repository state and what chezmoi apply would change.
func (d Deps) status(_ context.Context, args []string) error {
	if _, err := parse(newFlags(cmdStatus), args); err != nil {
		return err
	}
	return errors.New("status: not implemented")
}
