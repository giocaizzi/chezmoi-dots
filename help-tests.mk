# Focused black-box help regressions; no installs or full-suite runs.
.PHONY: test-help test-help-goldens

HELP_TEST_RUN ?= Test(LocalHelp|Help|UsageReportsLocalHelpOnce|LiveRepeatableGlobals|OutdatedUsage|Run)

test-help:
	GOFLAGS="-trimpath $(GOFLAGS)" go test -count=1 ./internal/cli ./cmd/chezmoi-dots -run '$(HELP_TEST_RUN)'

test-help-goldens:
	UPDATE_GOLDEN=1 GOFLAGS="-trimpath $(GOFLAGS)" go test -count=1 ./internal/cli -run '^TestHelpGoldens$$'
