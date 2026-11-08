# chezmoi-dots

The developer companion plugin for [chezmoi](https://www.chezmoi.io/) (`chezmoi-dots` standalone binary and `chezmoi dots` plugin).

## Purpose & Architecture

`chezmoi-dots` extends chezmoi with an interactive, declarative developer toolkit for managing multi-OS, multi-role dotfiles repositories:

- `cmd/chezmoi-dots/` — Main entrypoint (invoked as `dots` or `chezmoi-dots`).
- `internal/edit/` — Interactive registry-driven dotfiles editor with live pane previews and pre/post save hooks.
- `internal/matrix/` — 9-matrix OS (Darwin, Linux, Windows) × Role (Personal, Work, Light) evaluator.
- `internal/lint/` — Comprehensive template and registry integrity validator.
- `internal/pkgreg/` — Package and release registry validator and version comparator.
- `internal/release/` — Remote release fetcher and automated checksum bumper (`dots bump`).
- `internal/inventory/` — System package and tool inventory scanner across package managers.
- `internal/audit/` — Live unmanaged shell alias detector (`dots audit aliases`).
- `internal/aliasreg/` — Cross-shell alias registry compiler.
- `internal/chez/` — Chezmoi client abstraction and execution wrapper.
- `internal/term/` — Terminal styling, Catppuccin themes, and inventory tables.

## Principles & Public Repository Integrity

- **Strict Generality**: This is an open-source, universal tool meant for the entire developer community using chezmoi.
- **No Personal Configurations**: Never hardcode personal usernames, private machine hostnames, internal corporate paths, or proprietary repository names in code, tests, documentation, or templates.
- **Generic Placeholders**: Always use standard generic placeholders in examples and tests: `example/harness`, `octocat`, standard tools, and generic dotfiles repositories.
- **Cross-Platform Parity**: Every feature must work across macOS, Linux, and Windows with test coverage for each.

## Development

- `make check` — Run linter and tests with race detection (`check: lint test`).
- `make lint` — Run `golangci-lint run`.
- `make test` — Run unit and integration tests with `go test -race -shuffle=on ./...`.
- `make build` — Build cross-platform binaries into `bin/`.

## Conventional Commits

Commits must follow Conventional Commits (`feat:`, `fix:`, `chore:`, `docs:`, `test:`). Pull requests are auto-merged using squash commits once CI checks pass.
