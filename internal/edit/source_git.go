package edit

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/giocaizzi/chezmoi-dots/internal/chez"
)

// sourceGitPath resolves the literal path to use when Git automation is enabled.
func sourceGitPath(cfg chez.EditConfig, path string) (string, error) {
	if !cfg.Git.AutoAdd && !cfg.Git.AutoCommit && !cfg.Git.AutoPush {
		return "", nil
	}
	if cfg.WorkingTree == "" {
		return "", fmt.Errorf("cannot automate Git for %q without a working tree", path)
	}
	file, err := filepath.Rel(cfg.WorkingTree, path)
	if err != nil {
		return "", fmt.Errorf("resolve Git path for %q: %w", path, err)
	}
	if !filepath.IsLocal(file) || file == "." {
		return "", fmt.Errorf("source file %q is outside the Git working tree", path)
	}
	return filepath.ToSlash(file), nil
}

func (s *Service) saveSource(ctx context.Context, cfg chez.EditConfig, file, path string, before [32]byte) error {
	if file == "" {
		return nil
	}
	after, err := sum(path)
	if err != nil {
		return err
	}
	if before == after {
		return nil
	}
	status, err := s.Git.FileStatus(ctx, cfg.WorkingTree, file)
	if err != nil {
		return fmt.Errorf("check Git status of %q: %w", file, err)
	}
	if status == "" {
		return nil
	}
	if err := s.Git.Stage(ctx, cfg.WorkingTree, file); err != nil {
		return fmt.Errorf("stage %q: %w", file, err)
	}
	if !cfg.Git.AutoCommit && !cfg.Git.AutoPush {
		return nil
	}
	status, err = s.Git.FileStatus(ctx, cfg.WorkingTree, file)
	if err != nil {
		return fmt.Errorf("check staged status of %q: %w", file, err)
	}
	if status == "" || status[0] == ' ' || status[0] == '?' {
		return nil
	}
	if err := s.Git.Commit(ctx, cfg.WorkingTree, file); err != nil {
		return fmt.Errorf("commit %q: %w", file, err)
	}
	if cfg.Git.AutoPush {
		if err := s.Git.Push(ctx, cfg.WorkingTree); err != nil {
			return fmt.Errorf("push Git changes (commit kept locally): %w", err)
		}
	}
	return nil
}
