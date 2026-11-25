# Command reference

Invoke the program as `chezmoi dots` (plugin) or `chezmoi-dots` (standalone).
Examples below use `chezmoi dots`; `dots` also works if you define that alias.

## Command index

| Command | Arguments | Purpose |
| --- | --- | --- |
| `edit` (default) | `[<what>]` | Edit a registry entry, source file or managed target |
| `add` | `<path>` | Manage a file and register it for editing |
| `view` | `[<what>]` | Preview rendered content and composition |
| `list` | none | List editable registry components |
| `status` | none | Show the dotfiles checkout and what `chezmoi apply` would change |
| `upgrade` | `[<step>...]` | Run system and dotfiles upgrades |
| `lint` | `[<check>...]` | Validate registries and templates |
| `audit` | `[<check>...]` | Audit live machine state against the registries |
| `pkg` | `status`, `bump` | Survey and update pinned packages and releases |
| `version` | none | Print the binary version |
| `help` | `[<command> [<subcommand>]]` | Show root or local help |

Only `pkg` has subcommands (`status`, `bump`). Lint checks, audit checks, pin
names and upgrade steps are arguments, not subcommands. The internal `live`
command is [documented separately](#internal-live-command) and omitted from the
root index. There is no `tree` command and `list` does not accept a pattern.
For the daily procedures see the [everyday workflow](workflow.md).

## Help, parsing and errors

- Root options are only `-h` and `--help`; other options belong to their command.
- Use `chezmoi dots --help`, `chezmoi dots view --help`, or
  `chezmoi dots help view`. Both `-h` and `--help` work on every command page,
  including `pkg status` and internal `live`. `help` without a path shows the
  root; `help pkg status` shows the subcommand page.
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
source directory also honors `.chezmoiroot`. These apply to `edit`, `add`,
`view`, `list`, `status`, `lint`, `pkg status` and `pkg bump`; `upgrade` uses
`CHEZMOI_EXECUTABLE` for its dotfiles step. Child tools may use their own environment and configuration.

Help uses blue titles and section headings plus mauve command names. Styled
output is disabled for non-terminal streams, a nonempty `NO_COLOR`, or
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

## add

```text
chezmoi dots add [--name <name>] [--desc <text>] [--template] [--encrypt] [--local] <path>
```

Bring a file under chezmoi and register it in the edit registry in one step.
`path` is a target path: `~/` expands to the home directory and relative paths
resolve from the current directory. A folder is rejected (usage error, exit 2)
before anything changes: use `--local` to register a folder, or add one file.
If chezmoi does not manage the file yet, `add` asks `chezmoi add` (dry run) which
source file it would create, appends the `[edit.<name>]` entry with `desc` and
`source` to `.chezmoidata/edit.toml`, and only then runs the real `chezmoi add`
(forwarding `--template` and `--encrypt`). The entry comes first so that
chezmoi's `git.autoAdd`, `git.autoCommit` and `git.autoPush` commit and push the
source file and its entry together; as with every `chezmoi add`, that automation
stages every pending change in the source. If `chezmoi add` fails, the registry
file is restored. Afterwards `add` validates the whole registry; findings exit 1
and the entry is kept.

Registering a file chezmoi already manages, or a `--local` folder, runs no
`chezmoi add`, so `add` follows the same git settings as `edit`: with
`git.autoAdd`, `git.autoCommit` or `git.autoPush` it stages only
`.chezmoidata/edit.toml`, with `autoCommit` or `autoPush` it commits only that
file, and with `autoPush` it pushes (a push failure keeps the commit and exits
non-zero).

- `--name <name>`: registry name. Without it the name is inferred from the file
  name: leading dots and the extension are dropped and the result is lowercased.
  On a collision the name becomes `<parent>-<name>`; if that collides too, pass
  `--name`.
- `--desc <text>`: entry description. Prompted for on a terminal (Ctrl-C at the
  prompt cancels, exit 130); required otherwise (a usage error, exit 2).
- `--template`, `--encrypt`: forwarded to `chezmoi add`. Both are errors on a
  file chezmoi already manages.
- `--local`: register the folder (a file's parent folder) under the home
  directory as `local = "<folder>"` without running `chezmoi add`. It cannot be
  combined with `--template` or `--encrypt`.
- `-h`, `--help`: local help.

A path that is already registered prints a note and exits 0. Environment: shared
chezmoi and output settings.

```sh
chezmoi dots add ~/.config/example/config.toml --desc "Example tool settings"
chezmoi dots add ~/.gitconfig --template --name git --desc "Git settings"
chezmoi dots add ~/projects/notes --local --desc "Notes folder"
```

## view

```text
chezmoi dots view [--as <os>[,<role>]] [--matrix] [<what>]
```

With one selector, show composition and the rendered target for this machine by
default. `--as` renders one configuration: OS is `darwin`, `linux` or `windows`;
role is `personal` (default), `work` or `light`. Both `linux,work` and
`linux/work` are accepted. `--matrix` renders all nine configurations. With no
selector, `--matrix` shows an overview across registry entries; without it a
selector is required. `--as` cannot be combined with `--matrix`.

Options: `--as` (default empty/current machine), `--matrix` (false), `-h`,
`--help`. Environment: shared chezmoi and output settings only.

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

## status

```text
chezmoi dots status [--fetch] [--check]
```

Fast local snapshot, in two parts:

- The dotfiles Git checkout: branch, ahead/behind its upstream (from the last
  fetched refs), local changes and the age of the last fetch.
- `chezmoi status`: what `chezmoi apply` would change, one line per target with
  its two status columns.

There is no network access unless `--fetch` is given. Options:

- `--fetch`: run `git fetch` first. Git never prompts for credentials.
- `--check`: exit 1 when the checkout is ahead, behind or dirty, or when apply
  would change anything.
- `-h`, `--help`: local help.

Environment: shared chezmoi and output settings; `CHEZMOI_WORKING_TREE`
overrides working-tree discovery through chezmoi.

```sh
chezmoi dots status
chezmoi dots status --fetch --check
```

## upgrade

```text
chezmoi dots upgrade [--dry-run] [<step>...]
```

Default: run all applicable steps in plan order. Explicit step arguments select a subset
without reordering the plan. The platform plans are:

| Platform | Steps in order |
| --- | --- |
| macOS | `dotfiles`, `brew`, `uv`, `npm` |
| Linux | `dotfiles`, `apt`, `uv`, `npm` |
| Windows | `dotfiles`, `winget`, `uv`, `npm`, `psmodules` |

`dotfiles` runs `chezmoi update --refresh-externals`. Other steps use their
native managers; absent executables are skipped. Failed steps do not prevent
later steps from running. `upgrade` makes no release check and no network
request of its own; pinned releases are reported by
[`pkg status`](#pkg-status) and moved by [`pkg bump`](#pkg-bump).

Options: `--dry-run` (false; print commands only, executing nothing),
`-h`, `--help`. Environment: `CHEZMOI_EXECUTABLE` (default
`chezmoi`), `PATH`, the user's home directory (Linux npm prefix is `~/.local`),
and shared output settings.

```sh
chezmoi dots upgrade --dry-run
chezmoi dots upgrade dotfiles
chezmoi dots upgrade uv npm --dry-run
```

## audit

```text
chezmoi dots audit [<check>...]
```

Like `lint`, checks are arguments. The only check is `aliases`; with no argument
all checks run, and an unknown check is a usage error (exit 2). The `aliases`
check audits interactive Zsh and Bash aliases against rendered alias files and
enabled Oh My Zsh plugins. Shells absent from `PATH` are skipped; unmanaged
aliases make the check fail. Options: only `-h`, `--help`.

Environment: `XDG_CONFIG_HOME` (default `~/.config`) locates
`shell/aliases.sh` and optional `shell/omz-aliases.zsh`. Plugin definitions are
read under `~/.oh-my-zsh/plugins`. Apply your dotfiles first so rendered aliases
exist. Interactive shell startup files affect the aliases observed.

```sh
chezmoi dots audit
chezmoi dots audit aliases
```

## pkg status

```text
chezmoi dots pkg status [--offline] [--fetch] [--check]
```

Survey tools on this machine, comparing installed versions, pins and upstream
releases. No positional arguments. All options default to false:

- `--offline`: skip upstream release lookups, `uv` and `npm` latest lookups and
  the Git fetch. For pinned npm packages (including the `npm view` lookup) the
  latest is the newest version inside the pin. `brew` and `apt` still report
  latest from their local package index, and version commands or
  `winget export` may still use the network.
- `--fetch`: fetch the dotfiles Git working tree before evaluating it.
- `--check`: fail (exit 1) if anything is outdated, drifted or not installed.
- `-h`, `--help`: local help.

Git operations never prompt for credentials: both `git ls-remote` (release
lookups) and `--fetch` run with `GIT_TERMINAL_PROMPT=0` and `ssh -o BatchMode=yes`
unless you set `GIT_SSH_COMMAND`, `GIT_SSH` or `core.sshCommand`. A missing `winget` or `uv` is reported as not installed.
Ctrl-C exits 130.

Environment: shared settings; `CHEZMOI_WORKING_TREE` overrides working-tree
discovery through chezmoi. Package-manager discovery uses `PATH`.

```sh
chezmoi dots pkg status --offline
chezmoi dots pkg status --fetch --check
```

## pkg bump

```text
chezmoi dots pkg bump [--dry-run] [<pin>...]
```

Select pin names from `.chezmoidata/packages.toml`, or all configured pins by
default. Fetch newer upstream versions and checksums, then update the pins in
that file. It edits only that file and never installs anything; commit the
change and run `chezmoi apply` to install. Options: `--dry-run` (false; fetch
and report changes without writing), `-h`, `--help`. Environment: shared
chezmoi and output settings; network access is needed even for a dry run.

```sh
chezmoi dots pkg bump --dry-run
chezmoi dots pkg bump chezmoi-dots
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
chezmoi dots help [<command> [<subcommand>]]
```

Default: root index. Paths name command pages, not argument values:
`help lint` and `help pkg status` are valid, but `help lint registry` is not. Options: `-h`, `--help`.
No command-specific environment or operational dependencies; see
[help and errors](#help-parsing-and-errors) for streams and exit codes.

```sh
chezmoi dots help
chezmoi dots help view
chezmoi dots help pkg status
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
