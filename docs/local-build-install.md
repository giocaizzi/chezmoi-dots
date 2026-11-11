# Build and install locally

**When:** verify a source checkout and optionally replace your local CLI. This
procedure does not publish a release or change your dotfiles.

**Preconditions:** Go matching `go.mod`, Git, GNU-compatible `make`, and a
POSIX-compatible shell for the Makefile recipes. Go's module/tool cache must be
available (a cold cache may need network access). Run from the repository root.
For plugin verification, chezmoi must already be installed. Installation changes
`~/.local/bin/chezmoi-dots`; save an existing binary before replacing it.

## Steps

1. Inspect the task entrypoints and validate the checkout:

   ```sh
   make help
   make check
   ```

   `check` runs the tidy/format/lint checks for Linux, macOS and Windows, then
   tests with the race detector and shuffled order. Race tests require the
   platform's C toolchain. Do not continue on a failure.

2. Build the current host's static binary (not a cross-platform bundle):

   ```sh
   make build
   ./bin/chezmoi-dots version
   ./bin/chezmoi-dots --help
   ./bin/chezmoi-dots help view
   ./bin/chezmoi-dots help live
   ```

   Output is `bin/chezmoi-dots`. `version` prints `chezmoi-dots devel-<revision>`
   when VCS revision metadata is available, otherwise `chezmoi-dots devel`.
   Release builds can stamp a different version. Help should exit successfully
   without needing a dotfiles registry, network connection or user input.

3. **Optional, explicit local replacement:** save any existing installed binary
   outside this checkout, then run:

   ```sh
   make install
   ```

   This invokes `build` again, creates `~/.local/bin`, and copies the binary to
   `~/.local/bin/chezmoi-dots`. It does not edit `PATH`. Ensure that directory is
   on your shell's `PATH`. These recipes use POSIX `install` and paths; on
   Windows, run them in a compatible POSIX environment, not plain PowerShell.
   They do not provide a native Windows installer or add an `.exe` suffix.

## Verify

In a POSIX shell, verify the installed path directly before testing command
lookup:

```sh
"$HOME/.local/bin/chezmoi-dots" version
"$HOME/.local/bin/chezmoi-dots" --help
"$HOME/.local/bin/chezmoi-dots" help audit aliases
command -v chezmoi-dots
chezmoi-dots version
chezmoi-dots help view
command -v chezmoi
chezmoi dots version
chezmoi dots --help
```

The built and installed versions should match. Help must show the compact root
index and the selected local pages, not execute those commands. Plugin and
direct invocation should resolve the same version. If your shell caches an old
command location, start a new shell and repeat the lookup.

For a native Windows binary already installed by your chosen distribution
method, inspect PowerShell command resolution (including aliases and functions):

```powershell
Get-Command chezmoi-dots -All | Select-Object CommandType, Name, Source, Definition
Get-Command chezmoi -All | Select-Object CommandType, Name, Source, Definition
chezmoi-dots version
chezmoi-dots --help
chezmoi-dots help view
chezmoi dots version
chezmoi dots --help
$LASTEXITCODE
```

Expect exit 0 after successful help. If the version differs, inspect the resolved
path rather than assuming the first binary on `PATH` is your new build. In
PowerShell, use `& '<resolved executable path>' version` to test a specific
executable. The Makefile's extensionless POSIX install is not a substitute for a
native Windows installation.

## Rollback

Restore the saved binary to the same installed path. If this was a first-time
installation, remove only the binary you installed. Leave other files in
`~/.local/bin` intact. Start a new shell and repeat the lookup/version checks.
Build output can be regenerated with `make build`; no dotfiles changes need
reverting.

**Last verified:** 2026-11-11 by source review of the Makefile and entrypoint;
installation and platform-specific command lookup have not been executed as
part of this documentation change.
