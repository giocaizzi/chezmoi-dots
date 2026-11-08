<div align="center">

# chezmoi-dots

**The Developer Companion Plugin for Chezmoi.**  
Declarative registry-driven editing, package inventory drift detection, automated release bumping, template dependency graphs, and live preview.

[![Release](https://img.shields.io/github/v/release/giocaizzi/chezmoi-dots?sort=semver)](https://github.com/giocaizzi/chezmoi-dots/releases/latest)
[![CI](https://github.com/giocaizzi/chezmoi-dots/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/giocaizzi/chezmoi-dots/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

<p align="center">
  <img src="docs/assets/showcase.svg" alt="chezmoi-dots showcase: package inventory, matrix validation, live template previews" width="880">
</p>

</div>

[chezmoi](https://www.chezmoi.io/) is an exceptional dotfiles engine, but editing encrypted secrets, inspecting template composition DAGs, auditing external release versions, and previewing changes in real-time often requires hand-rolled scripts.

`chezmoi-dots` is a compiled Go plugin that extends `chezmoi` with a declarative, interactive developer toolkit.

---

## Highlights

- **Registry-Driven Editing (`dots <target>`)**: Open dotfiles by logical alias (`dots zsh`, `dots git`, `dots ssh/config`) with pre-save linters, atomic temp files, and post-save formatters.
- **Zero-Config Fallback**: Works on any `chezmoi managed` repository without configuration—fuzzy pick and edit any managed target directly.
- **Inventory & Drift Audit (`dots outdated`, `dots bump`)**: Query Homebrew, Pacman, Apt, and GitHub Releases to detect version drift against your pins. Update pins with comment-preserving text edits.
- **Live Preview (`dots live`)**: Watch edited template sources and render live updates to a side pane using tmux or Windows Terminal.
- **Template Dependency Graph (`dots tree`)**: Visualize template AST dependency graphs, detect cycles, and trace which roles or OS targets consume which snippets.
- **Template Linting (`dots lint`)**: Verify template syntax, detect unrendered delimiters, and validate cross-OS matrix configurations.

---

## Installation

### With Chezmoi (Recommended)

Declare `chezmoi-dots` as a release external in your dotfiles repository:

```toml
# .chezmoidata/packages.toml
[release.chezmoi-dots]
repo = "giocaizzi/chezmoi-dots"
version = "0.1.0"
```

Once installed to `~/.local/bin/chezmoi-dots`, chezmoi automatically delegates:

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
| `dots <alias>` | `[target]` | Open mapped target in `$EDITOR` with atomic save & lint |
| `dots list` | `[pattern]` | List managed targets, aliases, and templates |
| `dots live` | `<target>` | Watch file and render live preview in side tmux/terminal pane |
| `dots tree` | `[target]` | Render template dependency tree and composition graph |
| `dots outdated` | `[flags]` | Survey installed packages against pins and upstream releases |
| `dots bump` | `[package]` | Fetch latest releases and update pinned versions in registry |
| `dots upgrade` | `[flags]` | Update packages via their native package managers |
| `dots lint` | `[flags]` | Lint registries, template syntax, and matrix rules |
| `dots audit` | `aliases` | Audit live shell environment against alias registry |

---

## License

[MIT](LICENSE) © Giorgio Caizzi
