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
| `pkg` | `status`, `bump`, `add` | Survey and update pinned packages, releases and archive externals, and add packages and releases |
| `version` | none | Print the binary version |
| `help` | `[<command> [<subcommand>]]` | Show root or local help |

Only `pkg` has subcommands (`status`, `bump`, `add`). Lint checks, audit checks, pin
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
`view`, `list`, `status`, `lint`, `pkg status`, `pkg bump` and `pkg add`; `upgrade` uses
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
before anything changes: use `--local` to register a folder, or add one file. A
symlink is added as a symlink, even when it points at a folder.
If chezmoi does not manage the file yet, `add` asks `chezmoi add` (dry run) which
source file it would create, appends the `[edit.<name>]` entry with `desc` and
`source` to `.chezmoidata/edit.toml`, and only then runs the real `chezmoi add`
(forwarding `--template` and `--encrypt`). The entry comes first so that
chezmoi's `git.autoAdd`, `git.autoCommit` and `git.autoPush` commit and push the
source file and its entry together; as with every `chezmoi add`, that automation
stages every pending change in the source. If `chezmoi add` fails before it
created the source file, the registry file is restored. If it failed afterwards
(for example a failed `git.autoPush` after the commit), `add` keeps the entry,
exits non-zero with chezmoi's error (naming the added file and the kept entry)
and skips the registry validation; run `chezmoi dots lint registry` afterwards.
When `chezmoi add` succeeded, `add` validates the whole registry; findings exit 1
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

Checks are `registry`, `aliases`, `packages` (the rules are in the
[package registry](registry.md)), `matrix`, `templates`. With none,
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
releases. The tools come from the [package registry](registry.md) through your
dotfiles' `role`, `releases` and `packages` templates (see
[what your dotfiles provide](registry.md#what-your-dotfiles-provide)). No positional arguments. All options default to false:

- `--offline`: skip upstream release and archive HEAD lookups, `uv` and `npm` latest lookups and
  the Git fetch. For pinned npm packages (including the `npm view` lookup) the
  latest is the newest version inside the pin. `brew` and `apt` still report
  latest from their local package index, and version commands or
  `winget export` may still use the network.
- `--fetch`: fetch the dotfiles Git working tree before evaluating it.
- `--check`: fail (exit 1) if anything is outdated, drifted or not installed,
  including an outdated pinned archive.
- `-h`, `--help`: local help.

Git operations never prompt for credentials: both `git ls-remote` (release and
archive HEAD lookups) and `--fetch` run with `GIT_TERMINAL_PROMPT=0` and `ssh -o BatchMode=yes`
unless you set `GIT_SSH_COMMAND`, `GIT_SSH` or `core.sshCommand`. A missing `winget` or `uv` is reported as not installed.
Ctrl-C exits 130.

Environment: shared settings; `CHEZMOI_WORKING_TREE` overrides working-tree
discovery through chezmoi. Package-manager discovery uses `PATH`.

A "Pinned archives" table (columns TOOL, PINNED, LATEST, STATE; commits shown as
7 characters) lists every external in the source's TOML externals files
(`.chezmoiexternal.toml`, `.chezmoiexternal.toml.tmpl`, and `.toml` or
`.toml.tmpl` files in `.chezmoiexternals/` directories) whose URL is a commit
archive: GitHub `https://github.com/<owner>/<repo>/archive/<sha>.tar.gz` or
GitLab-shaped `https://<host>/<group>/<repo>/-/archive/<sha>/<repo>-<sha>.tar.gz`,
where `<sha>` is a full 40-hex commit. The row's name is the repository path
(`octocat/example`). Latest is the commit `git ls-remote <repo> HEAD` reports
(the default branch head); the pin is outdated when the two differ.

Limits: only TOML externals files; the URL must be written literally (a commit or
host coming from a template expression is not recognised); an entry with more
than one archive URL or more than one `sha256` (for example per-OS template
branches), or with a `sha384`, `sha512` or `size` checksum, is skipped (bump it
by hand); table headers may have spaces inside the brackets and a trailing
comment (`[ ".k" ] # note`), but `[[...]]` is not read; only the default branch HEAD is compared (no tags or other
branches); archives are listed from the whole source regardless of this
machine's OS or role; the installed copy is not inspected (`chezmoi apply`
installs the pin).

Skipped entries are listed in the table's note (`skipped N: <entry>: <reason>`,
the first three, then `; ...`). An unreadable directory or file in the source is
reported the same way and never fails the command; the rest is still read.

```sh
chezmoi dots pkg status --offline
chezmoi dots pkg status --fetch --check
```

## pkg bump

```text
chezmoi dots pkg bump [--dry-run] [<pin>...]
```

`<pin>` is a release name from `.chezmoidata/packages.toml` (see the
[release table](registry.md#releases)) or an archive
repository path (`octocat/example`); with no names, everything. Fetch newer
upstream versions and checksums, then update the pins. It edits
`.chezmoidata/packages.toml` and/or the externals files and never installs
anything; commit the change and run `chezmoi apply` to install.

For each outdated archive pin (recognition and limits are in
[pkg status](#pkg-status)) it replaces the commit in the URL (both places in a
GitLab URL), downloads the new archive and writes its SHA-256 into the entry's
`sha256` checksum (`checksum.sha256 = "..."`, a `[<entry>.checksum]` subtable, or
an inline `checksum = { sha256 = ... }`), editing the externals file in place as
text (templates and comments are kept). An entry without a checksum gets only the
URL rewritten. An entry with a `sha384`, `sha512` or `size` checksum is not a pin
(a bump cannot keep it valid) and is left untouched. Output is one line per pin,
`<name> <old7> → <new7>`; when nothing is outdated it prints
`all pins are up to date`. Each skipped entry or unreadable path is printed to
stderr as `skip <file>[ <entry>]: <reason>` and is not an error.

Files are written atomically: each new text goes to a temp file beside its
original (same mode), and the files are renamed over the originals only once all
temp files are ready, so nothing is written if any new file cannot be prepared.
If a rename fails, the error names the files already written.

Options: `--dry-run` (false; download and report changes without writing), `-h`, `--help`. Environment: shared
chezmoi and output settings; network access is needed even for a dry run.

```sh
chezmoi dots pkg bump --dry-run
chezmoi dots pkg bump chezmoi-dots
```

## pkg add

```text
chezmoi dots pkg add <name> [--brew[=<formula>]] [--cask <name>] [--apt[=<package>]]
                     [--winget <id>] [--uv[=<spec>]] [--npm[=<spec>]]
                     [--roles <roles>] [--requires <answer>] [--yes] [--dry-run]
chezmoi dots pkg add <name> --release --version <v> --url <template>
                     --target <os/arch>=<name>... [--roles <roles>] [--requires <answer>] [--dry-run]
chezmoi dots pkg add <name> --github <owner/repo> [--roles <roles>] [--requires <answer>] [--yes] [--dry-run]
```

Add one new entry to `.chezmoidata/packages.toml` in the chezmoi source dir, or the
keys an existing package lacks. `<name>` matches `[A-Za-z0-9_-]+`. A new entry is
added as text at the end of the file, after one blank line unless the file already
ends with one; the rest of the file (comments, order, CRLF or LF line endings) is
kept byte for byte, and the new entry uses the file's line ending. A missing file
(and `.chezmoidata/`) is created with mode 0644. The file is written atomically: the
new text goes to a temp file beside it, which is then renamed over it. A release with
that name is never changed, and neither is a package in release mode: it exits 1 with
`<kind> <name> already exists; change it with chezmoi dots edit or move a release with chezmoi dots pkg bump`.

**Package mode** (default) writes a `[package.<name>]` table with its keys in this
order: `brew`, `cask`, `apt`, `winget`, `uv`, `npm`, `roles`, `requires`. The install
sources come from flags or, when no source flag is given, from detection.

- `--brew`, `--apt`, `--uv`, `--npm`: the value is optional. Bare (`--brew`) it is
  `<name>`; `--brew=wget` installs `wget` instead. For `--uv` and `--npm` the value
  is the manager's spec, for example `--npm=typescript@5`. The value must be
  attached with `=`; `--brew wget` reads `wget` as the entry name.
- `--cask <name>`, `--winget <id>`: the value is required.

**Detection** runs only when no source flag (`--brew`, `--cask`, `--apt`, `--winget`,
`--uv`, `--npm`) is given: any of them turns it off. It asks the package managers on
this machine what they know of `<name>`: `brew` (formula, else cask) on macOS, `apt`
on Linux, `winget` on Windows, and `uv` and `npm` everywhere. It reads only this
machine's installed packages and the managers' own local indexes, never a public
index, and never another OS: the keys of the other OSes come only from flags or from
running `pkg add` on that OS. A manager that is not on `PATH` is not asked. The
proposal goes to stderr, one line per source, `  <key> = "<id>"  (installed)` or
`(available)`, and `  nothing found by <managers>` for the managers that found
nothing.

- On a terminal each source is confirmed: `<key> = "<id>"? [Y/n/e]` (Enter or `y`
  keeps it, `n` drops it, `e` asks for another id; an empty id keeps the detected
  one).
- Without a terminal the sources are accepted only with `--yes`; otherwise it exits 2
  with `detected sources need confirmation; pass --yes or set them with flags`.
- Nothing found, or every source declined, exits 2 and names the managers asked.
- `--dry-run` still detects (it only reads local state) and needs `--yes` off a
  terminal as a write does.

**Existing package.** When `<name>` is already a `[package.<name>]` table, `pkg add`
adds the keys it lacks and nothing else. A key the entry already has is left
unchanged and listed on stderr (`<key> already set; change it with chezmoi dots edit`).
The new key lines go after the table's last key line, before the blank lines and
comments that precede the next table header, in the file's line ending. If nothing is
left to add it exits 1 with
`package <name> already has these keys; change it with chezmoi dots edit`; a package
that is not a `[package.<name>]` table in this file (an inline table, or defined in
another file) exits 1 and is not changed. This is how another OS fills in its key: run
`pkg add <name>` there. It prints `updated package <name>: <keys>`.

**Notes.** After a package entry is written (or printed by `--dry-run`), one line per
OS none of whose keys the entry has goes to stdout: `not set for <os>: <keys>; pass
--<key> or run chezmoi dots pkg add <name> on <os>`. The keys are `brew` and `cask`
for darwin, `apt` for linux and `winget` for windows; `uv` and `npm` count for none.

**Release mode** (`--release`) pins a release binary and needs `--version`, `--url`
(a template that contains `{version}`, and optionally `{target}`) and one or more
`--target <os>/<arch>=<name>` (os `darwin`, `linux` or `windows`; arch `amd64` or
`arm64`; `<name>` is what the release calls that platform). It writes
`[release.<name>]` with `version`, `url`, `roles` and `requires` when given, then
`[release.<name>.target]` (sorted) and `[release.<name>.sha256]`. Like
[pkg bump](#pkg-bump), it downloads the asset of every target to compute its
SHA-256, so it needs network access; if a download fails nothing is written.

**GitHub mode** (`--github <owner/repo>`) implies release mode and derives the entry
from the latest GitHub release of the repository; it does not combine with
`--version`, `--url`, `--target` or a package source flag (usage errors).
`GH_TOKEN` or `GITHUB_TOKEN`, when set, is sent as a bearer token to raise the API
rate limit; neither is required. The assets are read by name only:

- Checksums, signatures, SBOMs and installers (`.sha256`, `.sig`, `.deb`, `.dmg`,
  `.msi`, `checksums`, ...) are left out, as are assets with no recognizable OS
  (`darwin`/`macos`/`osx`, `linux`, `windows`/`win`) or arch (`amd64`/`x86_64`/`x64`,
  `arm64`/`aarch64`); other arches (`386`, `armv7`, ...) are left out. A darwin
  `universal` asset with no arch counts for both darwin arches unless an
  arch-specific darwin asset exists.
- Per os/arch archives (`.tar.gz`, `.tgz`, `.tar.xz`, `.zip`) win over bare binaries,
  `.tar.gz` wins over `.zip` on darwin and linux and `.zip` over `.tar.gz` on
  windows. Two equal candidates are reported as ambiguous and both are left out.
- The chosen assets must be all archives (`type = "archive-file"`) or all bare
  binaries (`type = "file"`); a mix is an error. `exe = true` is written when a
  windows target is mapped.
- The release version (the tag without a leading `v`) becomes `{version}` in the URL;
  the part of the names that differs per platform becomes `{target}` and each
  target's value.

For `type = "archive-file"` the `path` is read from the archives. Before the proposal
is printed (and so before the confirmation), every target's archive is downloaded
once, for the checksum and, when it is a `.tar.gz`, `.tgz` or `.zip`, for the list of
its regular files; nothing is extracted to disk (a zip is spooled to a temporary file
that is removed). The same size cap and timeout as in release mode apply, and a
download that fails stops the command with nothing written. A target's candidates
are its executable files whose name is the entry name or the repository name (on
windows `<name>.exe`, any case); a file in an archive that records no modes counts
as executable. When every listed target has exactly one candidate and they are the
same path once the version and the target's full value are replaced by `{version}` and
`{target}` where they are whole tokens, that is the `path` (for example
`tool_{version}/bin/tool`; never with `.exe`). `{target}` always stands for the whole
registry value, archive extension included: with extension-carrying values
(`linux_amd64.tar.gz`) a directory like `tool_1.2.3_linux_amd64` is not settled and is
reported as not found. Otherwise the `path` stays the repository name. A `.tar.xz` archive is not listed, and targets with one are left out
of the check; an archive that cannot be read counts the same.

The proposal goes to stderr: `version`, `url`, `type`, `path`, `exe`, each target
and its value, then `left out: ...` and `ambiguous: ...`. The `path` line says how it
was settled: `(found in the archives)`, `(not found in the archives)` followed by a
line `path candidates: <target>: <files>; ...` (what matched, or up to five
executable files, or `none`; each file name is quoted with Go escapes, as the
names come from the archive), or `(unchecked: <reason>)` when no archive could be
listed. On a terminal it is confirmed with `Add this release? [y/N]`; without one it
needs `--yes` (otherwise exit 2, and nothing is downloaded). The entry has the keys
`version`, `url`, `type`, `path`, `exe`, `roles`, `requires`, then the target and
sha256 tables; the checksums come from the same downloads, so the archives are not
fetched twice. Bare binaries (`type = "file"`) have no `path` and are downloaded for
the checksum only after the confirmation, as in release mode.

`--roles` takes a comma list of `personal`, `work`, `light` and writes
`roles = [...]`; `--requires <answer>` writes `requires = "<answer>"`. Both work
in every mode. Package source flags with `--release`, release flags
(`--version`, `--url`, `--target`) without it, a missing version, url or target in
release mode, a bad name, role, target or url, and a name other than exactly one are
usage errors (exit 2).

`--dry-run` (false) prints the entry to stdout and changes nothing; for an existing
package it prints the key lines it would add. In release and GitHub mode it downloads
no archive, ends the entry with the comment `# sha256 computed when written`
where the checksums go and leaves `path` as the repository name, marked
`(unchecked: --dry-run downloads no archive)` (`--github` still reads the GitHub
API). It makes no git changes.

After the write, the whole package registry is validated as in
`lint packages`. Findings are printed to stderr (`✗ packages`, the findings, and
`<name> was written to <file>; fix it (chezmoi dots lint packages)`) and exit 1; the
entry is kept and git is not touched. Otherwise it prints `added package <name>`,
`updated package <name>: <keys>` or `added release <name>`, and follows the same git
settings as [add](#add) for a managed file: with `git.autoAdd`, `git.autoCommit` or
`git.autoPush` it stages, commits and pushes only `.chezmoidata/packages.toml` (a push
failure keeps the commit and exits 1). It never installs anything; run
`chezmoi apply` to install.

Environment: shared chezmoi and output settings; `GH_TOKEN` / `GITHUB_TOKEN` for
`--github`.

```sh
chezmoi dots pkg add wget --brew --apt
chezmoi dots pkg add wget            # ask this machine's package managers
chezmoi dots pkg add wget --yes      # accept what they find, no prompt
chezmoi dots pkg add example-app --cask example-app --roles personal,work
chezmoi dots pkg add example-tool --release --version 1.2.3 \
  --url 'https://example.com/{version}/tool-{target}.tar.gz' \
  --target linux/amd64=linux-x86_64 --target darwin/arm64=macos-arm --dry-run
chezmoi dots pkg add example-tool --github octocat/example-tool --dry-run
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
