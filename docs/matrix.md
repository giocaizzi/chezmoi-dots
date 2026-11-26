# Matrix

The matrix is the set of configurations a dotfiles source is checked against:
OS (`darwin`, `linux`, `windows`, the only fixed set) times the profiles declared
in chezmoi data under `matrix`. `chezmoi dots view --matrix`, `view --as` and
`lint templates` render templates for these configurations; `edit` uses them for
previews.

Without a declaration (or without profile tables) there is a single profile,
`default`, which uses the template's own defaults plus any seed answers. The
tool sets no keys of its own in the data.

## Profiles

A profile is a named set of answers. Names use lowercase letters, digits, `_`
and `-`, and start with a letter or digit. Profiles are listed in name order.

Where a column needs a short label (the `view` grid header and the overview
legend), the label is the shortest prefix of the name that is not a prefix of
any other profile name (the whole name otherwise), first letter upper-cased:
`home`/`minimal` give `H`/`M`, `work`/`web` give `Wo`/`We`, `dev`/`devops` give
`Dev`/`Devo`.

## How a configuration's data is produced

For each (OS, profile), `dots` renders the source's `.chezmoi.toml.tmpl` with
`chezmoi execute-template --init --no-tty`, with `.chezmoi.os` set to that OS,
against a scratch config. Its `[data]` holds only the seed answers and the
profile's answers (profile overrides seed). The `[data]` table of the rendered
config becomes that configuration's data.

A `prompt*Once` without a declared answer returns its default when it has one.
Without a default, `promptStringOnce` and `promptChoiceOnce` return the prompt
text itself (so a choice matches no branch), `promptMultichoiceOnce` returns a
one-item list holding the prompt text, `promptBoolOnce` returns `false` and
`promptIntOnce` returns `0`. Avoid this by declaring the key in `[matrix.seed]`
(or a profile table), or by giving the prompt a default.

Configuration data is only the rendered `[data]`. A key that only some
configurations render is set to its zero value (`false`, `""`, empty list) for
the others, and so is a key in this machine's config `[data]` that no
configuration renders, so nothing from this machine's config reaches any
configuration.

Coverage note: features whose prompt defaults to off stay off in every
configuration unless declared, so templates guarded by them are not exercised.
Declare answers to cover them.

## Declaration

The declaration is opt-in, under the `matrix` key of chezmoi data, conventionally
`.chezmoidata/matrix.toml`. As a `.chezmoidata` file it is also visible to
templates as `.matrix`.

```toml
[matrix.seed]                  # answers for every profile
name = "octocat"
secretsProjectId = ""          # keep renders away from external services

[matrix.profile.home]          # a profile; its answers override seed; may be empty
featureX = true

[matrix.profile.minimal]

[[matrix.variant]]             # extra configuration rendered only by lint templates
name = "one tool"
profiles = ["home"]            # optional; all profiles when absent
set = { tools = ["alpha"] }    # data overrides on top of the profile's rendered data
```

Rules:

- Values are strings, booleans or lists of strings.
- `chezmoi` is reserved and rejected in `seed`, `profile` and `set`.
- Keys must be bare TOML keys: letters, digits, `_` and `-`.
- A variant's `name` must be a non-empty string and unique.
- A variant's `profiles`, when present, must be non-empty and name declared
  profiles (or `default` when none are declared); omit it for every profile.
- A variant's `set` must be non-empty.

## Variants

A variant is an extra configuration rendered only by `lint templates`, with the
`set` values applied on top of each selected profile's rendered data. Findings
show in labels such as `linux/home [one tool]`.

## Isolation

Matrix renders must never reach external services. Declare answers that switch
on network or secret access (for example an answer holding a secrets-manager
project id) as empty in `[matrix.seed]` or the profiles, such as
`secretsProjectId = ""`. A template gated on such an answer (via `.chezmoiignore`
or an `if`) is then skipped in every render. Stripping tools from `PATH` does not
help for templates that call a tool by absolute path, so the declaration is the
mechanism.

## Template linting

`lint templates` renders every template whose target is a shell (`.sh`, `.bash`),
zsh, PowerShell (`.ps1`, windows configurations only) or JSON file, once per
configuration and variant, labelled `<os>/<profile>` or `<os>/<profile> [name]`.
It skips partials (`.chezmoitemplates`), chezmoi's own special files,
encrypted, symlink and modify entries, and any template whose actions call a
password-manager, secret or decrypt function. Linters: shellcheck (bash dialect
when there is no shebang), `zsh -n`, the PowerShell parser and JSON validity; a
missing linter is skipped.

## Validation

`chezmoi dots lint matrix` validates the declaration: unknown keys, invalid
profile names, reserved keys, value types, a missing or duplicate variant name,
`profiles` and an empty `set`. `view` and `lint templates` stop with "the matrix
declaration is invalid (chezmoi dots lint matrix)" when it is invalid. `edit` is
the exception: it warns, skips the per-configuration check and still opens the
editor, so the declaration can be repaired.

## Migrating

Sources that relied on earlier built-in `personal`, `work` and `light` roles
declare them as profiles holding the answers each implies (for example the
booleans the template used to receive), and put shared answers in
`[matrix.seed]`.
