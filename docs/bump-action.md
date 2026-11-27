# Bump pinned releases from GitHub Actions

**When:** a dotfiles repository on GitHub pins releases in its [package registry](registry.md#releases) or commit-pinned externals, and you want a scheduled pull request that moves the pins. Not for bumping by hand: use [`pkg bump`](cli.md#pkg-bump) locally.
**Preconditions:** the dotfiles repository is on GitHub, runs on a `ubuntu-latest` or `macos-latest` (ARM64) runner, and you can add a repository secret.

## What the action does

`giocaizzi/chezmoi-dots/.github/actions/pkg-bump` is a composite action. It:

1. installs a pinned, checksum-verified `chezmoi` (the same install the CI of this repository uses);
2. downloads the `chezmoi-dots` release named by `version` and checks it against the `checksums.txt` of the same release;
3. runs `chezmoi-dots pkg bump` against `source-dir` (`CHEZMOI_SOURCE_DIR`, so `.chezmoiroot` is honoured). `pkg bump` needs no token: it reads releases through `chezmoi data`, `git ls-remote` and plain HTTPS downloads;
4. if a pin changed, commits the tracked changes as `github-actions[bot]` on `branch`, force-pushes it, and opens a pull request, or updates the open one for that branch. When nothing changed it prints `all pins are up to date` and makes no pull request.

Only the last step receives the token.

## Inputs and outputs

| Input | Default | Meaning |
|---|---|---|
| `version` | required | chezmoi-dots release to install, `0.4.1` or `v0.4.1`. Explicit on purpose: the run is reproducible and never trusts an unpinned download |
| `source-dir` | `.` | Chezmoi source directory, relative to the workspace |
| `names` | empty | Whitespace-separated pins to bump; empty bumps all. Each is limited to letters, digits, `.`, `_`, `/` and `-` |
| `dry-run` | `false` | `true` runs `pkg bump --dry-run`; nothing is committed and no pull request is made, but `changed` still reports whether a pin would move |
| `token` | `${{ github.token }}` | Pushes the branch and opens the pull request |
| `branch` | `chore/bump-releases` | Branch the changes are pushed to (force-pushed on every run) |
| `base` | empty | Base branch; empty is the repository's default branch |
| `title` | `chore(deps): bump pinned releases` | Pull request title and commit message |

| Output | Meaning |
|---|---|
| `changed` | `true` when a pin changed, else `false` |
| `pull-request-url` | The opened or updated pull request, empty when none |

## Steps

1. Create a secret `BUMP_TOKEN` in the dotfiles repository: a fine-grained personal access token or a GitHub App token with `contents: write` and `pull-requests: write` on that repository only. A pull request opened with it starts your `pull_request` workflows.
2. Add the workflow below as `.github/workflows/bump-pins.yml`.
3. Pin the action by commit SHA with the release as a comment, and keep `version:` equal to that release. Dependabot moves the `uses:` ref; change `version` in the same pull request.
4. Run it once with `workflow_dispatch` and `dry-run: true` to see the result.

```yaml
name: bump-pins
on:
  schedule:
    - cron: "17 5 * * 1"
  workflow_dispatch:
permissions:
  contents: write
  pull-requests: write
jobs:
  bump:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - uses: giocaizzi/chezmoi-dots/.github/actions/pkg-bump@<sha> # v0.4.1
        with:
          version: 0.4.1
          source-dir: home
          token: ${{ secrets.BUMP_TOKEN || github.token }}
```

Replace `<sha>` with the commit of the release tag (`gh api repos/giocaizzi/chezmoi-dots/commits/v0.4.1 -q .sha`) and `home` with the directory that holds `.chezmoidata` (or `.` for a root source). The repository may be `octocat/dotfiles`; nothing else is specific to it.

## Token and permissions

- The workflow grants `contents: write` and `pull-requests: write` so the fallback `github.token` can push and open the pull request.
- With the `github.token` fallback, a pull request it opens does not trigger `pull_request` workflows (a GitHub rule). Start the checks by hand or close and reopen the pull request; prefer `BUMP_TOKEN`.
- Set `persist-credentials: false` on the checkout. Otherwise the checkout leaves its own `GITHUB_TOKEN` in the git config and the push uses it instead of `token`; the action warns (`::warning::`) when it finds one. The push takes its credential from `gh auth git-credential` for that single command and writes nothing to the git config.
- The token is passed to the pull request step as `GH_TOKEN` and is never printed.

## Supported runners and verification

- Linux X64 and macOS ARM64, the platforms that both the releases and the pinned `chezmoi` hashes cover. Any other runner fails with a message.
- The binary is checked against the `checksums.txt` published with the same release, by exact file name. A mismatch or a missing line fails the step.
- Build provenance attestations are not verified by the action. Check one by hand: `gh attestation verify chezmoi-dots_0.4.1_linux_amd64 --repo giocaizzi/chezmoi-dots`.

## Troubleshooting

| Symptom | Cause and fix |
|---|---|
| `invalid version` | `version` is not `X.Y.Z` or `vX.Y.Z` (a pre-release suffix is allowed) |
| `unsupported runner` | Use a Linux X64 or macOS ARM64 runner |
| `no checksum for ...` or `checksum mismatch` | The release lacks that asset, or the download is corrupt: do not use it, check the release page |
| `all pins are up to date` | Nothing to do |
| `pkg bump` finds no registry | `source-dir` is not the chezmoi source directory (see `.chezmoiroot`) |
| The pull request has no checks | It was opened with `github.token`; use `BUMP_TOKEN` |
| Push is rejected as `github-actions[bot]` | The checkout persisted credentials, or the token lacks `contents: write` |

## Verify

Run the workflow with `dry-run: true`, then without it: a pull request on `chore/bump-releases` appears when a pin is outdated. The tests of the action script run hermetically with `make test-action`.
