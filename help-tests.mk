# Focused black-box help regressions; no installs or full-suite runs.
.PHONY: test-help test-help-goldens

HELP_TEST_RUN ?= Test(LocalHelp|Help|UsageReportsLocalHelpOnce|LiveRepeatableGlobals|PkgUsage|Run)

test-help: ## Focused help regression tests
	GOFLAGS="-trimpath $(GOFLAGS)" go test -count=1 ./internal/cli ./cmd/chezmoi-dots -run '$(HELP_TEST_RUN)'

test-help-goldens: ## Regenerate the help golden files
	UPDATE_GOLDEN=1 GOFLAGS="-trimpath $(GOFLAGS)" go test -count=1 ./internal/cli -run '^TestHelpGoldens$$'
