# Docs

Source of truth for decisions, architecture and runbooks of this repo.

- [Command reference](cli.md): arguments, options, defaults, environment and help/error semantics.
- [Secrets registry and audit](secrets.md): `.chezmoidata/secrets.toml`, `lint secrets` rules and the read-only `audit secrets` probe.
- [Everyday workflow](workflow.md): daily check, editing, upgrades, pin updates, CI usage and migrating from 0.x command names.
- [Matrix](matrix.md): OS x declared profiles, how their data is produced and the opt-in `matrix` declaration.
- [Package registry](registry.md): the `.chezmoidata/packages.toml` schema, the templates `pkg status` renders, and a worked example.
- [Release chezmoi-dots](release.md): runbook for tagging and publishing a version, verifying it, and bumping it in a dotfiles repository.
- [Build and install locally](local-build-install.md): checks, build, optional installation, verification and rollback.

## Apply and publish source edits

Configure the existing [chezmoi settings](https://www.chezmoi.io/reference/configuration-file/variables/)
in your local chezmoi configuration, then open a source file with `chezmoi dots <target>`.

- `edit.apply = true` applies the affected local targets after validation and preview. With the setting off, `dots` asks before applying.
- `git.autoAdd = true` stages the edited source file. `git.autoCommit = true` stages and commits it, and `git.autoPush = true` also pushes after committing.

The commit includes the whole validated file, including any earlier edits to that file. Other staged changes stay in the index. If Git rejects the push, the commit stays local. Inspect it with `chezmoi git -- log -1` and follow your repository's branch policy to publish it.
