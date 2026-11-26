<div align="center">

# chezmoi-dots

**The Developer Companion Plugin for Chezmoi.**  
Declarative registry-driven editing, package survey and drift detection, pin bumping, a dotfiles status check, template dependency graphs, and live preview.

[![Release](https://img.shields.io/github/v/release/giocaizzi/chezmoi-dots?sort=semver)](https://github.com/giocaizzi/chezmoi-dots/releases/latest)
[![CI](https://github.com/giocaizzi/chezmoi-dots/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/giocaizzi/chezmoi-dots/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

<p align="center">
  <img src="docs/assets/showcase.png" alt="chezmoi-dots showcase: package inventory, matrix validation, live template previews" width="880">
</p>

</div>

[chezmoi](https://www.chezmoi.io/) is an exceptional dotfiles engine, but editing encrypted secrets, inspecting template composition DAGs, auditing external release versions, and previewing changes in real-time often requires hand-rolled scripts.

`chezmoi-dots` is a compiled Go plugin that extends `chezmoi` with a declarative, interactive developer toolkit.

---

## Highlights

- **Registry-Driven Editing (`dots <target>`)**: Open dotfiles by logical alias (`dots zsh`, `dots git`, `dots ssh/config`) with pre-save linters, atomic temp files, and post-save formatters.
- **Zero-Config Fallback**: Works on any `chezmoi managed` repository without configuration—fuzzy pick and edit any managed target directly.
- **Add & Register (`dots add`)**: Bring a new file under chezmoi and register it for editing in one step.
- **Status Snapshot (`dots status`)**: See the dotfiles checkout (branch, ahead/behind, local changes) and what `chezmoi apply` would change, without touching the network.
- **Package Survey & Pins (`dots pkg status`, `dots pkg bump`)**: Query Homebrew, Apt, winget, uv, npm, and GitHub Releases to detect version drift against your pins. Also tracks commit-pinned archive externals. Update pins with comment-preserving text edits.
- **Live Preview**: Editing can render live updates to a side pane using tmux or Windows Terminal; `live` is the internal pane command.
- **Template Composition (`dots view`)**: Inspect a target's composition tree and rendered content, including previews across OS/role configurations.
- **Template Linting (`dots lint`)**: Verify template syntax, detect unrendered delimiters, and validate cross-OS matrix configurations.

---

## Installation

### With Chezmoi (Recommended)

Declare `chezmoi-dots` as an external in your dotfiles repository:

```toml
# .chezmoiexternal.toml.tmpl
{{- $v := "0.3.0" }}
{{- $exe := ternary ".exe" "" (eq .chezmoi.os "windows") }}
[".local/bin/chezmoi-dots{{ $exe }}"]
type = "file"
url = "https://github.com/giocaizzi/chezmoi-dots/releases/download/v{{ $v }}/chezmoi-dots_{{ $v }}_{{ .chezmoi.os }}_{{ .chezmoi.arch }}{{ $exe }}"
executable = true
```

Release assets exist for darwin/amd64, darwin/arm64, linux/amd64, linux/arm64 and windows/amd64. Optionally add `checksum.sha256` from the release's `checksums.txt`.

Once the binary is on your `PATH` (for example `~/.local/bin`), chezmoi automatically delegates:

```bash
chezmoi dots <command>
```

Add a shell alias for convenience:

```bash
alias dots="chezmoi dots"
```

### Standalone

```bash
go install github.com/giocaizzi/chezmoi-dots/cmd/chezmoi-dots@latest
```

---

## Commands Reference

| Command | Arguments | Description |
|---|---|---|
| `dots [edit]` | `[<what>]` | Edit a registry entry, source file or managed target; omitting the selector opens a picker |
| `dots add` | `<path>` | Manage a file and register it for editing |
| `dots view` | `[<what>]` | Preview rendered content and composition; `--matrix` shows all configurations |
| `dots list` | none | List configured editable registry components |
| `dots status` | none | Show the dotfiles checkout and what `chezmoi apply` would change |
| `dots upgrade` | `[<step>...]` | Update dotfiles and tools via their native managers |
| `dots lint` | `[<check>...]` | Lint registries, template syntax, and matrix rules |
| `dots audit` | `[<check>...]` | Audit live shell aliases against the alias registry |
| `dots pkg status` | none | Survey installed packages against pins and upstream releases |
| `dots pkg bump` | `[<pin>...]` | Fetch latest releases and archive commits, and update the pins in the registry and externals files |
| `dots pkg add` | `<name>` | Add a package or pinned release to `packages.toml` |
| `dots version` | none | Print the binary version |
| `dots help` | `[<command> [<subcommand>]]` | Show root or local help |

Use `dots <command> --help` for command-specific options. See the full
[command reference](docs/cli.md) for defaults, environment, parsing and exit
semantics, including the internal `live` command. The day-to-day procedures are
in the [everyday workflow](docs/workflow.md) runbook. For source builds, see
[Build and install locally](docs/local-build-install.md).

See [Apply and publish source edits](docs/README.md#apply-and-publish-source-edits) for the chezmoi settings that control apply and Git automation.

---

## License

[MIT](LICENSE) © Giorgio Caizzi
