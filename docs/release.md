# Release chezmoi-dots

**When:** a maintainer publishes a new version of `chezmoi-dots`. A release is made by pushing a version tag by hand; there is no release bot or changelog file. It is not for installing the tool (see the [README](../README.md#installation)) or for building it locally ([Build and install locally](local-build-install.md)).
**Preconditions:** push access to `main` and tags on `giocaizzi/chezmoi-dots`, the `gh` CLI signed in, and a clone with the latest `main` checked out.

## What the release workflow does

[`.github/workflows/release.yml`](../.github/workflows/release.yml) runs on every pushed tag matching `v*`, on `ubuntu-latest`:

1. Builds with the Go version of `go.mod`, one binary per target (`darwin/amd64`, `darwin/arm64`, `linux/amd64`, `linux/arm64`, `windows/amd64`), with `CGO_ENABLED=0`, `-trimpath` and `-ldflags "-s -w -X main.buildVersion=<version without the leading v>"`. Assets are named `chezmoi-dots_<version>_<os>_<arch>`, with `.exe` appended on Windows.
2. Writes `checksums.txt` (`sha256sum` of every asset).
3. Attests build provenance for the assets listed in `checksums.txt` (`actions/attest`).
4. Creates the GitHub release: `gh release create <tag> dist/* --title "chezmoi-dots <version>" --generate-notes --verify-tag`. The notes are generated from the merged pull requests.

## Steps

1. Pick the version with [semver](https://semver.org). The project is pre-1.0 (all tags are `v0.x.y`): a breaking change raises the minor version, anything else the patch.

   ```sh
   git fetch --tags
   git tag --sort=-v:refname | head -3
   ```

2. Check that you are on an up-to-date `main` with green CI and a passing local check.

   ```sh
   git switch main && git pull --ff-only
   gh run list --branch main --workflow ci.yml -L 1
   make check
   ```

   The latest CI run must be `completed` with conclusion `success`.

3. Create the tag and push it. Existing tags are annotated (`git cat-file -t v0.3.0` prints `tag`) and their message is the version and a short summary of the release; do the same.

   ```sh
   git tag -a vX.Y.Z -m "vX.Y.Z: <short summary>"
   git push origin vX.Y.Z
   ```

4. Watch the release run.

   ```sh
   gh run list --workflow release.yml -L 1
   gh run watch
   ```

5. Open the generated release notes and edit them if they need it.

   ```sh
   gh release view vX.Y.Z --web
   ```

## Verify

1. The release has the five binaries and `checksums.txt`.

   ```sh
   gh release view vX.Y.Z --json assets -q '.assets[].name'
   ```

   Expected: `checksums.txt`, then `chezmoi-dots_X.Y.Z_<os>_<arch>` for `darwin_amd64`, `darwin_arm64`, `linux_amd64`, `linux_arm64` and `windows_amd64.exe`.

2. Download one asset and the checksums into an empty directory, check the checksum and the provenance.

   ```sh
   cd "$(mktemp -d)"   # any empty directory
   gh release download vX.Y.Z --repo giocaizzi/chezmoi-dots -p checksums.txt -p 'chezmoi-dots_X.Y.Z_linux_amd64'
   sha256sum -c checksums.txt --ignore-missing    # macOS: shasum -a 256 -c checksums.txt --ignore-missing
   gh attestation verify chezmoi-dots_X.Y.Z_linux_amd64 --repo giocaizzi/chezmoi-dots
   ```

   Expected: `chezmoi-dots_X.Y.Z_linux_amd64: OK`, and the second command exits 0.

3. The binary reports the version. Run it from the asset for your platform:

   ```sh
   chmod +x chezmoi-dots_X.Y.Z_linux_amd64
   ./chezmoi-dots_X.Y.Z_linux_amd64 version   # prints chezmoi-dots X.Y.Z
   ```

## Update a dotfiles repository to the new release

A dotfiles repository that installs `chezmoi-dots` declares it as a release in its [package registry](registry.md#releases) and lets `chezmoi dots pkg bump` pin it:

```toml
# .chezmoidata/packages.toml
[release.chezmoi-dots]
version = "0.3.0"
url = "https://github.com/giocaizzi/chezmoi-dots/releases/download/v{version}/chezmoi-dots_{version}_{target}"
type = "file"
exe = true

[release.chezmoi-dots.target]
"darwin/amd64" = "darwin_amd64"
"darwin/arm64" = "darwin_arm64"
"linux/amd64" = "linux_amd64"
"linux/arm64" = "linux_arm64"
"windows/amd64" = "windows_amd64.exe"

[release.chezmoi-dots.sha256]
"darwin/amd64" = "<sha256>"
# ... one per target
```

The `{target}` values are the asset names without the `chezmoi-dots_<version>_` prefix. Your externals template turns the entry into a chezmoi external (see the [worked example](registry.md#worked-example)). To take the new release:

```sh
chezmoi dots pkg bump chezmoi-dots
chezmoi git -- diff        # review the new version and checksums
chezmoi git -- commit -am "chore: bump chezmoi-dots"
chezmoi git -- push
chezmoi apply
```

`pkg bump` downloads each target's asset to compute its SHA-256, so the checksums are the ones of the published binaries; compare them with `checksums.txt` if you want a second check.

## Rollback

A bad release is replaced, not edited:

1. Ship a fixed patch version with the steps above. Prefer this: anyone who already took the bad version moves forward with `pkg bump`.
2. If the release must disappear (for example it contains a secret or is unusable), delete the release and its tag:

   ```sh
   gh release delete vX.Y.Z --cleanup-tag --yes
   git tag -d vX.Y.Z
   ```

   Deleting it does not change dotfiles repositories that already bumped: they keep the pinned version and checksum, and `chezmoi apply` fails to download until they pin another version. Never reuse the version number; publish a new one.

**Last verified:** 2026-11-26 by Claude: the workflow description is read from `release.yml`; the asset names, checksum and attestation checks were run against the v0.3.0 release. The tag and release steps have not been run end to end for a new version.
