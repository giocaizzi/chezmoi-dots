# Command reference

Invoke the program as `chezmoi dots` (plugin) or `chezmoi-dots` (standalone).
Examples below use `chezmoi dots`; `dots` also works if you define that alias.

## Command index

| Command | Arguments | Purpose |
| --- | --- | --- |
| `edit` (default) | `[<what>]` | Edit a registry entry, source file or managed target |
| `view` | `[<what>]` | Preview rendered content and composition |
| `list` | none | List editable registry components |
| `lint` | `[<check>...]` | Validate registries and templates |
| `audit` | `aliases` | Audit live shell aliases |
| `outdated` | none | Compare installed, pinned and latest versions |
| `bump` | `[<release>...]` | Update release pins and checksums |
| `upgrade` | `[<step>...]` | Run system and dotfiles upgrades |
| `version` | none | Print the binary version |
| `help` | `[<command> [<child>]]` | Show root or local help |

Only `audit` has a child command, `aliases`. Lint checks, release names and
upgrade steps are arguments, not child commands. The internal `live` command is
[documented separately](#internal-live-command) and omitted from the root index.
There is no `tree` command and `list` does not accept a pattern.

## Help, parsing and errors

- Root options are only `-h` and `--help`; other options belong to their command.
- Use `chezmoi dots --help`, `chezmoi dots view --help`, or
  `chezmoi dots help view`. Both `-h` and `--help` work on every command page,
  including `audit aliases` and internal `live`. `help` without a path shows the
  root; `help audit aliases` shows the child page.
- Successful help prints to stdout and exits 0, without reading registries,
  writing files, running subprocesses, using the network or prompting for input.
  Help is recognized by the executing command's flag parser, not by scanning
  every argument: a flag value containing `--help` remains a value.
- Options can precede or follow positional arguments, for example
  `view --matrix git` and `view git --matrix`. Value options accept
  `--as=linux,work` or `--as linux,work`; booleans default to false and can use
  `--matrix=true` or `--matrix=false`.
- Historical `--` behavior is **not** a permanent end-of-options marker. It ends
  the current flag-parser pass; one following positional is consumed and parsing
  resumes. Thus `edit -- --help` selects the literal `--help`, while
  `view -- git --matrix` still parses `--matrix` as an option. Do not assume all
  tokens after `--` are literal selectors.
- Omitting the command opens the edit picker. An unrecognized first token is an edit
  selector, not automatically an unknown-command error: `chezmoi dots git` means
  `chezmoi dots edit git`.
- An invalid explicit help path prints a diagnostic and the nearest valid help
  page to stderr. Usage errors print a diagnostic and local help once to stderr
  and exit 2. The CLI API preserves `errors.Is(err, ErrUsage)` for callers.
  Operational failures (including failed checks) exit 1; cancellation exits 130.

## Shared environment

Commands using chezmoi honor `CHEZMOI_EXECUTABLE` (default `chezmoi`),
`CHEZMOI_CONFIG_FILE` (otherwise chezmoi's normal configuration), and
`CHEZMOI_SOURCE_DIR` (otherwise discovered through chezmoi). An explicitly set
source directory also honors `.chezmoiroot`. These apply to `edit`, `view`,
`list`, `lint`, `outdated` and `bump`; `upgrade` uses `CHEZMOI_EXECUTABLE` for its
dotfiles step. Child tools may use their own environment and configuration.

Styled output is disabled for non-terminal streams, a nonempty `NO_COLOR`, or
`TERM=dumb`. Executable discovery uses `PATH`; home-relative paths use the
current user's home directory. Command sections below list additional relevant
environment. `version` has no command-specific environment.

## edit (default)

```text
chezmoi dots [edit] [<what>]
```

`what` is a registry name, source-relative file, or managed target path. Omitting
it opens the interactive registry picker. A local registry entry can open a
folder's file picker. Source edits use a temporary copy, validation and preview
before saving. Apply and Git automation follow the
[chezmoi edit settings](README.md#apply-and-publish-source-edits).

Options: only `-h`, `--help`. Editor selection is `EDITOR`, then `VISUAL`, then
`vi` (`notepad` on Windows). `TMUX` with `tmux` available, or `WT_SESSION` with
`wt` on Windows, enables a live side pane; these are optional.

```sh
chezmoi dots
chezmoi dots edit git
chezmoi dots dot_config/git/config.tmpl
```

## view

```text
chezmoi dots view [--as <os>[,<role>]] [--matrix] [--all] [<what>]
```

With one selector, show composition and the rendered target for this machine by
default. `--as` renders one configuration: OS is `darwin`, `linux` or `windows`;
role is `personal` (default), `work` or `light`. Both `linux,work` and
`linux/work` are accepted. `--matrix` renders all nine configurations. `--all`
has the same effect as `--matrix`. With no selector, either flag shows an
overview across registry entries; without either flag a selector is required.
`--as` cannot be combined with `--matrix` or `--all`.

Options: `--as` (default empty/current machine), `--matrix` and `--all` (both
false), `-h`, `--help`. Environment: shared chezmoi and output settings only.

```sh
chezmoi dots view git
chezmoi dots view git --as linux,work
chezmoi dots view --matrix
```

## list

```text
chezmoi dots list
```

List configured editable registry components. No arguments or filtering pattern.
Options: only `-h`, `--help`. Environment: shared chezmoi and output settings.

```sh
chezmoi dots list
```

## lint

```text
chezmoi dots lint [<check>...]
```

Checks are `registry`, `aliases`, `packages`, `matrix`, `templates`. With none,
run all in that order; explicit arguments run in the supplied order. Findings
and check summaries go to stderr. Matrix checks evaluate the OS/role
configurations; template checks validate rendered files and available linters.

Options: only `-h`, `--help`. Environment: shared chezmoi/output settings and
`PATH` for external validators. Alias validation can use the age identity
configured in chezmoi when available.

```sh
chezmoi dots lint
chezmoi dots lint registry packages
```

## audit / audit aliases

```text
chezmoi dots audit aliases
```

`audit` requires the `aliases` child; it has no default action. Audit interactive
Zsh and Bash aliases against rendered alias files and enabled Oh My Zsh plugins.
Shells absent from `PATH` are skipped; unmanaged aliases make the check fail.
Both pages accept only `-h`, `--help`; `aliases` takes no arguments.

Environment: `XDG_CONFIG_HOME` (default `~/.config`) locates
`shell/aliases.sh` and optional `shell/omz-aliases.zsh`. Plugin definitions are
read under `~/.oh-my-zsh/plugins`. Apply your dotfiles first so rendered aliases
exist. Interactive shell startup files affect the aliases observed.

```sh
chezmoi dots help audit
chezmoi dots audit aliases
```

## outdated

```text
chezmoi dots outdated [--offline] [--fetch] [--check]
```

Survey tools on this machine, comparing installed versions, pins and upstream
releases. No positional arguments. All options default to false:

- `--offline`: skip network lookups.
- `--fetch`: fetch the dotfiles Git working tree before evaluating it.
- `--check`: fail if anything is outdated, drifted or not installed.
- `-h`, `--help`: local help.

Environment: shared settings; `CHEZMOI_WORKING_TREE` overrides working-tree
discovery through chezmoi. Package-manager discovery uses `PATH`.

```sh
chezmoi dots outdated --offline
chezmoi dots outdated --fetch --check
```

## bump

```text
chezmoi dots bump [--dry-run] [<release>...]
```

Select release names from `.chezmoidata/packages.toml`, or all configured
releases by default. Fetch newer upstream versions and checksums, then update
pins in that file; this does not install them. Run `chezmoi apply` to install.
Options: `--dry-run` (false; fetch and report changes without writing), `-h`,
`--help`. Environment: shared chezmoi and output settings; network access is
needed even for a dry run.

```sh
chezmoi dots bump --dry-run
chezmoi dots bump chezmoi-dots
```

## upgrade

```text
chezmoi dots upgrade [--dry-run] [<step>...]
```

Default: run all applicable steps in plan order, then check pinned releases for
newer versions (report only, not a bump). Explicit step arguments select a subset
without reordering the plan. The platform plans are:

| Platform | Steps in order |
| --- | --- |
| macOS | `dotfiles`, `brew`, `uv`, `npm` |
| Linux | `dotfiles`, `apt`, `uv`, `npm` |
| Windows | `dotfiles`, `winget`, `uv`, `npm`, `psmodules` |

`dotfiles` runs `chezmoi update --refresh-externals`. Other steps use their
native managers; absent executables are skipped. Failed steps do not prevent
later steps from running. `releases` is not a selectable step name.

Options: `--dry-run` (false; show commands without executing upgrade steps),
`-h`, `--help`. A default dry run still performs the final release-version
lookup; it is not an offline mode. Environment: `CHEZMOI_EXECUTABLE` (default
`chezmoi`), `PATH`, the user's home directory (Linux npm prefix is `~/.local`),
and shared output settings.

```sh
chezmoi dots upgrade --dry-run
chezmoi dots upgrade dotfiles
chezmoi dots upgrade uv npm --dry-run
```

## version

```text
chezmoi dots version
```

Print `chezmoi-dots <version>`. No arguments are required. Extra arguments and
unknown flags are ignored for compatibility. `-h` and `--help` show help only
when the parser reaches them; an unknown flag stops parsing, so
`chezmoi dots version --unknown --help` still prints the version.
Local builds report `devel-<revision>` when VCS metadata is available, otherwise
`devel`; release builds can stamp a version.

```sh
chezmoi dots version
```

## help

```text
chezmoi dots help [<command> [<child>]]
```

Default: root index. Paths name command pages, not argument values:
`help lint` is valid, but `help lint registry` is not. Options: `-h`, `--help`.
No command-specific environment or operational dependencies; see
[help and errors](#help-parsing-and-errors) for streams and exit codes.

```sh
chezmoi dots help
chezmoi dots help view
chezmoi dots help audit aliases
chezmoi dots help live
```

## Internal live command

`live` remains callable for editor preview panes but is not a user-facing root
entry. Use `help live` or `live --help` for its explicit internal help.

```text
chezmoi-dots live [options]
```

| Option | Default | Purpose |
| --- | --- | --- |
| `--bin <path>` | `chezmoi` | Chezmoi executable |
| `--src <path>` | empty | Shadow source directory |
| `--real-src <path>` | empty | Original source directory |
| `--file <path>` | empty | Source-relative file to preview |
| `--edited <path>` | empty | Edited temporary file |
| `--dir <path>` | empty | Scratch directory; a `done` file here ends the loop |
| `--global <value>` | none | Forward one global chezmoi argument; repeatable, in supplied order |
| `-h`, `--help` | n/a | Internal help |

There is no target positional argument in the pane protocol. The editor supplies
these paths; empty defaults are not a useful standalone preview configuration.
The loop refreshes every 500 ms. Unlike normal chezmoi-backed commands, its
executable, source and forwarded options come from flags, not the
`CHEZMOI_*` overrides. Shared output styling applies.

```sh
chezmoi-dots help live
chezmoi-dots live --help
```
