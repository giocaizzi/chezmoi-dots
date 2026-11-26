# Secrets registry, `audit secrets` and `secrets run`

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
| `command` | all, optional | CLI to use instead of the default (`bws`, `bw`, `rbw`): an absolute path (including a Windows `C:\...` path), `~/...` (expands to the home directory), or a bare name looked up on `PATH`. A relative path with a separator is refused |

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
- `command` a relative path with a separator (`./bws`, `bin/bws`): `command must be an absolute path, ~/..., or a bare name looked up on PATH`. It would resolve against the current directory of whoever runs chezmoi, so `secrets run` and the audit also refuse it (`command <c> is not an absolute path`).
- `command` starting with `~/` when the home directory is unknown: refused at run/audit time with `cannot expand ~/ in command: home directory unknown`.
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
| `bws` | access token read from the keyring, then `bws project get <id>` | item missing or value empty: `fail`, `no access token in the keyring (service <s>, user <u>)`, hint `chezmoi secret keyring set --service=<s> --user=<u>`. Any other keyring failure (chezmoi not found, locked keychain, no Secret Service on headless Linux): `fail`, `keyring read failed: <cause>`, hint `check that chezmoi can read the OS keyring: chezmoi secret keyring get --service=<s> --user=<u>`. Project readable: `ok`, `project <id> reachable`. Network failure or timeout: `warn`, `unreachable: ...`, hint `check the network and rerun`. Other failure: `fail`, `bws project get failed: <first stderr line>`, hint `check the token and that its machine account can read project <id>` |
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

## Running a CLI with its token: secrets run

```text
chezmoi dots secrets run <name> [--timeout <duration>] [--] [<args>...]
```

Runs the CLI of the declared entry `<name>` with its access token in the
environment, so a template or a script can call it without handling the token.
It works for `bws` entries only: `bw` and `rbw` have no token to inject, so
`secrets run` on them is a usage error (exit 2); use the CLI's own session
(`BW_SESSION`, the `rbw` agent).

- The arguments are split at the first literal `--`. What comes before is parsed
  by `secrets run` (`--timeout`, `-h`, `--help`, the name); everything after is
  passed to the CLI verbatim and never parsed, so `-h` or `--output` after `--`
  reach `bws`. An unknown option before `--` is a usage error that adds
  `put the CLI's arguments after --`. chezmoi passes plugin arguments verbatim,
  including `--`, `--output` and `--color`, so
  `chezmoi dots secrets run example-vault -- secret list <project-id> --output json --color no`
  reaches `bws` unchanged.
- The CLI is resolved like the audit does: the entry's `command` (absolute path, `~/...`
  expanded to the home directory, or a bare name looked up on `PATH`) or `bws` on `PATH`. The token is read from the
  entry's keyring (`chezmoi secret keyring get`) and set as `BWS_ACCESS_TOKEN` in
  the child's environment only.
- `--timeout <duration>` is a Go duration (`30s`, `2m`); the default `0` means no
  timeout. A negative value is a usage error. On expiry it fails with
  `bws timed out after <duration>`.
- A missing keyring item (or an empty value) fails with `no access token in the keyring (service <s>, user <u>)` and the `chezmoi secret keyring set` hint. Any other keyring failure (chezmoi not found, locked keychain, no Secret Service on headless Linux) fails with `read the access token (service <s>, user <u>): <cause>`.
- The CLI's output goes straight to stdout and stderr, and stdin is passed through.

Exit codes: the CLI's own exit code when it fails; 2 for a usage error (no name,
unknown subcommand, a name that is not declared, a `bw` or `rbw` entry, an
unknown option before `--`); 1 for everything else (invalid registry, CLI not
found, no token in the keyring, timeout).

```sh
chezmoi dots secrets run example-vault -- project list
chezmoi dots secrets run example-vault --timeout 30s -- secret list 00000000-0000-0000-0000-000000000000 --output json --color no
```

## Using it from a template

The same template works on every OS: no `sh` or PowerShell branches and no shell
quoting, because the arguments are passed to the command one by one.

```text
{{- if lookPath "chezmoi-dots" }}
{{-   $json := output "chezmoi" "dots" "secrets" "run" "example-vault" "--" "secret" "list" .secretsProjectId "--output" "json" "--color" "no" }}
{{-   range (fromJson $json) }}
export {{ .key }}={{ .value | quote }}
{{-   end }}
{{- end }}
```

- chezmoi's [`lookPath`](https://www.chezmoi.io/reference/templates/functions/lookPath/)
  returns an empty string when the executable is not on `PATH`, and
  [`output`](https://www.chezmoi.io/reference/templates/functions/output/) fails
  the whole template if the command exits non-zero, and runs on every render.
- When the answer may be empty (`projectFrom`), gate on it too, for example
  `{{- if and (lookPath "chezmoi-dots") .secretsProjectId }}`.
- Bootstrap: on a new machine the first `chezmoi apply` has no `chezmoi-dots`
  yet, because templates render before externals are fetched. The `lookPath`
  gate makes that render skip the block. Install `chezmoi-dots` as a chezmoi
  external (see [Installation](../README.md#installation)) and run
  `chezmoi apply` a second time. Alternatively, list the file in
  `.chezmoiignore` (itself a template) until the binary exists.

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
- `secrets run` executes only a CLI declared in the registry, at its declared
  `command` or on `PATH`, and refuses names that are not declared. No flag or
  environment variable overrides the command, and no shell is ever involved.
- The token goes only into the child's environment (`BWS_ACCESS_TOKEN`): never
  into an argument, never into our stdout or stderr, and it is replaced by
  `[redacted]` in our own error messages. The child inherits the rest of the
  environment.
- `secrets run` does not filter what the CLI prints. Whatever `bws` writes, for
  example secret values from `secret list`, goes to stdout; that is the point
  for templates. It is not a general secret reader and adds no access the token
  does not already grant.
- Anyone who can edit the registry's `command` can make it hand the token to
  another program; this is the same trust as the dotfiles source itself.
- Every audit CLI call has a 15 second timeout. Being offline gives `warn`
  (`unreachable`), not `fail`.
