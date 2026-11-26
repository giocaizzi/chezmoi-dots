# Matrix

The matrix is the set of nine configurations a dotfiles source is checked
against: OS `darwin`, `linux`, `windows` times role `personal`, `work`, `light`.
`chezmoi dots view --matrix`, `view --as` and `lint templates` render templates
for these configurations; `edit` uses them for previews.

## How a role's data is produced

For each role, `dots` renders the source's `.chezmoi.toml.tmpl` with
`chezmoi execute-template --init --no-tty` against a scratch config. Its
`[data]` holds only:

- `light` and `work`, booleans set from the role;
- the answers you declare (see below).

A `prompt*Once` without a declared answer returns its default when it has one.
Without a default, `promptStringOnce` and `promptChoiceOnce` return the prompt
text itself (so a choice matches no branch), `promptMultichoiceOnce` returns a
one-item list holding the prompt text, `promptBoolOnce` returns `false` and
`promptIntOnce` returns `0`. Avoid this by declaring the key in `[matrix.seed]`
(or a role table), or by giving the prompt a default. The `[data]` table of the
rendered config becomes that role's data.

Role data is only the rendered `[data]`. A key that only some roles render is set
to its zero value (`false`, `""`, empty list) for the others, and so is a key in
this machine's config `[data]` that no role renders, so nothing from this
machine's config reaches any configuration.

Coverage note: features whose prompt defaults to off stay off in every
configuration unless declared, so templates guarded by them are not exercised.
Declare answers to cover them.

## Declaration

The declaration is opt-in, under the `matrix` key of chezmoi data, conventionally
`.chezmoidata/matrix.toml`. As a `.chezmoidata` file it is also visible to
templates as `.matrix`.

```toml
[matrix.seed]            # answers for every role
name = "octocat"
featureX = true

[matrix.role.light]      # answers for one role (personal, work or light); override seed
featureX = false

[[matrix.variant]]       # an extra configuration that only `lint templates` renders
name = "one tool"
roles = ["personal", "work"]   # optional; all roles when absent
set = { tools = ["alpha"] }    # data overrides on top of the role's rendered data
```

Rules:

- Values are strings, booleans or lists of strings.
- `chezmoi`, `light` and `work` are reserved (`light` and `work` are set per role
  by the tool) and rejected in `seed`, `role` and `set`.
- Keys must be bare TOML keys: letters, digits, `_` and `-`.
- A variant's `name` must be a non-empty string; `roles`, when present, must be
  non-empty (omit it for every role).
- `role` tables are named `personal`, `work` or `light`; role answers override
  the seed.

## Variants

A variant is an extra configuration rendered only by `lint templates`. It needs a
unique `name`, an optional `roles` list (all roles when absent) and a non-empty
`set` applied on top of each selected role's rendered data. Findings show in
labels such as `linux/work [one tool]`.

## Validation

`chezmoi dots lint matrix` validates the declaration: unknown keys, unknown
roles, reserved keys, value types, a missing or duplicate variant name, `roles`
and an empty `set`. `view` and `lint templates` stop with "the matrix
declaration is invalid (chezmoi dots lint matrix)" when it is invalid. `edit` is
the exception: it warns, skips the per-configuration check and still opens the
editor, so the declaration can be repaired.

## Migrating

Earlier versions used built-in answers and compared the template's `[data]` keys
with a built-in list. Sources that relied on those answers add a `matrix.toml`
declaring the answers they want covered.
