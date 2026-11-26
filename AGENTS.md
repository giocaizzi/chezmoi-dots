# chezmoi-dots

The developer companion plugin for [chezmoi](https://www.chezmoi.io/) (`chezmoi-dots` standalone binary and `chezmoi dots` plugin).

## Purpose & Architecture

`chezmoi-dots` extends chezmoi with an interactive, declarative developer toolkit for managing multi-OS, multi-profile dotfiles repositories:

- `cmd/chezmoi-dots/` — Main entrypoint (invoked as `dots` or `chezmoi-dots`).
- `internal/edit/` — Interactive registry-driven dotfiles editor with live pane previews and pre/post save hooks.
- `internal/matrix/` — OS (Darwin, Linux, Windows) × declared profiles evaluator; seeds profile answers from the opt-in `matrix` declaration ([docs/matrix.md](docs/matrix.md)).
- `internal/lint/` — Comprehensive template and registry integrity validator.
- `internal/pkgreg/` — Package and release registry validator and version comparator.
- `internal/release/` — Remote release fetcher, commit HEAD lookups and automated checksum bumper (`dots pkg bump`).
- `internal/extpin/` — Commit-pinned archive externals in `.chezmoiexternal` files: scan and in-place bump.
- `internal/archive/` — Lists the regular files of a `.tar.gz`, `.tgz` or `.zip` stream without extracting it (`pkg add --github` reads the binary path from it).
- `internal/inventory/` — System package and tool inventory scanner across package managers.
- `internal/audit/` — Live unmanaged shell alias detector (`dots audit`).
- `internal/aliasreg/` — Alias registry validator (`lint aliases`).
- `internal/detect/` — Asks this machine's package managers what they know of a name (`pkg add` without a source flag).
- `internal/ghrelease/` — Reads the latest GitHub release of a repository and derives a release entry from its assets (`pkg add --github`).
- `internal/vaultcheck/` — Secrets registry validation and read-only secret-manager CLI probe (`dots audit secrets`); see [docs/secrets.md](docs/secrets.md).
- `internal/cli/` — Command parsing, help and wiring of adapters into use cases.
- `internal/survey/` — Fills the `pkg status` overview from package managers, release binaries and git.
- `internal/registry/` — Edit registry (`.chezmoidata/edit.toml`) parse and add.
- `internal/upgrade/` — Upgrade plan and run for dotfiles, system packages and language tools.
- `internal/finding/` — Result type of the lint checks.
- `internal/tree/` — Composition tree of a source file (includes, symlink targets, data files).
- `internal/udiff/` — Unified diffs of two texts.
- `internal/chez/` — Chezmoi client abstraction and execution wrapper.
- `internal/term/` — Terminal styling, Catppuccin themes, and inventory tables.

## Principles & Public Repository Integrity

- **Strict Generality**: This is an open-source, universal tool meant for the entire developer community using chezmoi.
- **No Personal Configurations**: Never hardcode personal usernames, private machine hostnames, internal corporate paths, or proprietary repository names in code, tests, documentation, or templates.
- **Generic Placeholders**: Always use standard generic placeholders in examples and tests: `example/harness`, `octocat`, standard tools, and generic dotfiles repositories.
- **Cross-Platform Parity**: Every feature must work across macOS, Linux, and Windows with test coverage for each.

## Development

- `make check` — Run linter and tests with race detection (`check: lint test`).
- `make lint` — Check `go mod tidy`, then run `golangci-lint` for Linux, macOS and Windows.
- `make test` — Run unit and integration tests with `go test -race -shuffle=on ./...`.
- `make check-fast` — Run tests without the race detector.
- `make test-help` — Run the focused help regression tests (from `help-tests.mk`).
- `make test-help-goldens` — Regenerate the help golden files.
- `make build` — Build this host's static binary into `bin/` (release binaries come from `.github/workflows/release.yml`).
- `make fmt` — Format, tidy and apply modernizers.
- `make vuln` — Reachable known vulnerabilities (needs the network).
- `make install` — Build and copy the binary to `~/.local/bin`.

## Docs

The documentation lives in `docs/`; start at [docs/README.md](docs/README.md).

## Conventional Commits

Commits must follow Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`). Pull requests are auto-merged using squash commits once CI checks pass.
