# Everyday workflow

**When:** routine use of `chezmoi dots` on a machine that applies a dotfiles repository. It is not for building the tool itself; see [Build and install locally](local-build-install.md).
**Preconditions:** `chezmoi` and `chezmoi-dots` installed, and a dotfiles repository initialised. Options and environment are in the [command reference](cli.md).

## Steps

1. Daily check. `status` is a local snapshot of the dotfiles checkout and of what `chezmoi apply` would change; it uses no network. Add `--fetch` to refresh the upstream refs first.

   ```sh
   chezmoi dots status
   chezmoi dots status --fetch
   ```

2. Edit. Open an existing entry, or bring a new file under chezmoi and register it.

   ```sh
   chezmoi dots edit git
   chezmoi dots add ~/.config/example/config.toml --desc "Example tool settings"
   ```

3. Keep the machine current. `upgrade` runs the dotfiles update and the native package managers; use `--dry-run` to print the commands only.

   ```sh
   chezmoi dots upgrade --dry-run
   chezmoi dots upgrade
   ```

4. Keep pins current: release pins in `.chezmoidata/packages.toml` and commit-pinned archive externals in `.chezmoiexternal` files. Survey, bump, review, publish, then apply on each machine.

   ```sh
   chezmoi dots pkg status
   chezmoi dots pkg bump --dry-run
   chezmoi dots pkg bump
   ```

   Review the change to `.chezmoidata/packages.toml` and to any externals file, then commit and push it in the dotfiles repository (`chezmoi git -- diff`, `chezmoi git -- commit`, `chezmoi git -- push`). On each machine run `chezmoi apply`, or `chezmoi dots upgrade dotfiles` to pull and apply. `pkg bump` never installs anything.

   To start tracking a new tool, `pkg add` appends it to `.chezmoidata/packages.toml` and validates the registry; preview with `--dry-run`. See [pkg add](cli.md#pkg-add).

   ```sh
   chezmoi dots pkg add wget --brew --apt
   chezmoi dots pkg add example-tool --release --version 1.2.3 --url 'https://example.com/{version}/tool-{target}.tar.gz' --target linux/amd64=linux-x86_64
   ```

5. CI. The checks exit 1 on findings and can gate a pipeline.

   ```sh
   chezmoi dots lint
   chezmoi dots pkg status --check
   chezmoi dots status --check
   ```

   Add `--offline` to `pkg status` where network access is restricted; see [pkg status](cli.md#pkg-status) for what it skips.

## Verify

`chezmoi dots status --check` and `chezmoi dots pkg status --check` both exit 0 once pins are applied and installed.

## Rollback

Revert the pin commit in the dotfiles repository and run `chezmoi apply`. A registry entry added by `add` is removed by deleting its `[edit.<name>]` block from `.chezmoidata/edit.toml`; `chezmoi forget <path>` stops managing the file.

## Migrating from 0.x command names

The old names were removed without aliases.

| 0.x | Now |
| --- | --- |
| `outdated` | `pkg status` |
| `bump` | `pkg bump` |
| `audit aliases` | `audit` (or `audit aliases`) |
| `view --all` | `view --matrix` |
| `upgrade` release-check footer | `pkg status` |

**Last verified:** not yet verified against a release; written for the CLI redesign.
