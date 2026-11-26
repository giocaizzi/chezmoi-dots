# Secrets registry and `audit secrets`

`.chezmoidata/secrets.toml` declares which secret managers your templates read
from. `chezmoi dots lint secrets` validates the declaration and
`chezmoi dots audit secrets` probes, read-only, that each declared manager is
usable on this machine for this source. The registry never holds secret values.

## Registry

Each entry is a table under `secret` (chezmoi data key `secret`), named freely:

| Key | Kinds | Meaning |
| --- | --- | --- |
| `kind` | all, required | `bws`, `bw` or `rbw` |
| `project` | `bws` | Literal Bitwarden Secrets Manager project id. Exactly one of `project` / `projectFrom` |
| `projectFrom` | `bws` | Name of a `[data]` answer that holds the project id. Exactly one of `project` / `projectFrom` |
| `keyring` | `bws`, required | Inline table `{ service, user }`: where the access token lives in the OS keyring |
| `command` | all, optional | Path of the CLI to use instead of the default (`bws`, `bw`, `rbw`); a leading `~/` expands to the home directory |

Keys are camelCase, like the other registries.

```toml
# Secrets Manager: the project id comes from a [data] answer
[secret.example-vault]
kind = "bws"
projectFrom = "secretsProjectId"
keyring = { service = "bws", user = "access-token" }
# command = "~/.local/bin/bws"

# Secrets Manager: literal project id
[secret.ci-project]
kind = "bws"
project = "00000000-0000-0000-0000-000000000000"
keyring = { service = "bws", user = "ci-token" }

# Password manager CLI
[secret.passwords]
kind = "bw"

# Unofficial Bitwarden CLI
[secret.rbw]
kind = "rbw"
```

## Lint

`chezmoi dots lint secrets` runs on the isolated (OS/role independent) data,
like `packages`. Findings are reported as `secret <name>: <message>`:

- `secret` absent: no findings. Present but not a table: `must be a table of entries`.
- Entry not a table: `must be a table`.
- Unknown key: `unknown key <k>`; inside `keyring`: `unknown key keyring.<k>`.
- `kind` missing: `missing kind (one of bws, bw, rbw)`; other value: `unknown kind "<v>" (one of bws, bw, rbw)`.
- `command` present but not a non-empty string: `command must be a non-empty string`.
- `bws`: neither `project` nor `projectFrom`: `kind bws needs project or projectFrom`; both: `set project or projectFrom, not both`; either not a non-empty string: `project must be a non-empty string` / `projectFrom must be a non-empty string`.
- `bws` keyring: missing: `kind bws needs keyring = { service, user }`; not a table: `keyring must be a table`; `service` or `user` missing or empty: `keyring needs service` / `keyring needs user`.
- `bw` / `rbw` with `project`, `projectFrom` or `keyring`: `<key> is only for kind bws`.

## Audit

```sh
chezmoi dots audit secrets
```

With no `secret` entries it prints
`secrets: no entries declared (.chezmoidata/secrets.toml)` and exits 0. An
invalid registry prints `✗ secrets`, the findings and
`fix the registry (chezmoi dots lint secrets)`, then exits 1 without probing.
Otherwise every entry is probed in name order, using this machine's chezmoi
data (the answers `projectFrom` points to).

States:

- `ok`: usable, or not applicable on this machine.
- `warn`: could not be confirmed (locked, offline); does not fail the audit.
- `fail`: not usable; the audit exits 1.

Any `fail` exits 1; `warn` alone exits 0. Details are one line and never contain
a secret value, token or account email. Each non-ok entry gets a hint line
under the table.

### What each kind probes

| Kind | Probe | Result |
| --- | --- | --- |
| all | CLI looked up on `PATH` (or `command`) | missing: `fail`, `<name> not found`, hint `install <kind> or set command in .chezmoidata/secrets.toml` |
| `bws` with `projectFrom` | the answer in this machine's data | empty or absent: `ok`, `not configured (answer <key> is empty)`, nothing else probed; not a string: `fail`, `answer <key> is not a string` |
| `bws` | access token read from the keyring, then `bws project get <id>` | token missing or empty: `fail`, `no access token in the keyring (service <s>, user <u>)`, hint `chezmoi secret keyring set --service=<s> --user=<u>`. Project readable: `ok`, `project <id> reachable`. Network failure or timeout: `warn`, `unreachable: ...`, hint `check the network and rerun`. Other failure: `fail`, `bws project get failed: <first stderr line>`, hint `check the token and that its machine account can read project <id>` |
| `bw` | `bw status`, only its `status` field | `unlocked`: `ok`, `vault unlocked`. `locked`: `warn`, `vault locked`, hint `bw unlock, then export BW_SESSION`. `unauthenticated`: `fail`, `not logged in`, hint `bw login`. Anything else: `fail`, `bw status failed: ...`. Timeout: `warn`, `unreachable: timed out after <timeout>` |
| `rbw` | `rbw unlocked` | exit 0: `ok`, `agent unlocked`. Timeout: `warn`. Other error: `warn`, `locked or agent not running`, hint `rbw unlock` |

### Sample output

```text
NAME             KIND  STATE  DETAIL
ci-project       bws   ok     project 00000000-0000-0000-0000-000000000000 reachable
example-vault    bws   ok     not configured (answer secretsProjectId is empty)
passwords        bw    warn   vault locked
rbw              rbw   fail   rbw not found
  passwords: bw unlock, then export BW_SESSION
  rbw: install rbw or set command in .chezmoidata/secrets.toml
```

This run exits 1 because of the `fail`.

## Relation to chezmoi

The registry declares what your templates read; the audit only probes. It never
reads or wraps secret values and does not replace chezmoi's own functions.

- Templates read values with `bitwarden`, `bitwardenFields` (`bw`),
  `bitwardenSecrets` (`bws`), `rbw` / `rbwFields` (`rbw`) and `keyring` (the OS
  keyring).
- `chezmoi secret keyring get` / `set` manage keyring entries. The audit reads
  the `bws` token with `secret keyring get` and the hint for a missing token is
  the matching `secret keyring set`.
- `chezmoi doctor` reports whether the CLIs are found. The audit goes further
  and checks they are usable for this source: logged in or unlocked, token
  present, project reachable.

## Security notes

- Read-only: the audit never creates, changes or deletes a secret.
- No values, tokens or account details are printed. The stdout of `bw status`
  and `bws` is never shown; error details are the first stderr line with any
  token replaced by `[redacted]`.
- The `bws` token is read with `chezmoi secret keyring get` and handed to `bws`
  only through the `BWS_ACCESS_TOKEN` environment variable, never as an
  argument (chezmoi's docs advise against tokens on command lines). `bws` runs
  with `--output none`, so no output is produced.
- Every CLI call has a 15 second timeout. Being offline gives `warn`
  (`unreachable`), not `fail`.
