# Edit registry

The edit registry names the components `chezmoi dots` edits. It is the chezmoi
data key `edit` of the source, conventionally in `.chezmoidata/edit.toml`, with
one table `[edit.<name>]` per component. [`edit`](cli.md#edit-default) opens an
entry by name, and [`add`](cli.md#add) appends entries. [`lint registry`](cli.md#lint)
checks the registry.

```toml
[edit.note]
desc = "Notes settings"
source = "dot_config/example/note.toml"
check = ["example-lint", "{file}"]
note = "Restart the example tool to reload"
diff = "first"

[edit.projects]
desc = "Notes folder"
local = "projects/notes"

[edit.registry]
desc = "Edit registry"
source = ".chezmoidata/edit.toml"
```

## Keys

| Key | Type | Meaning |
|---|---|---|
| `desc` | string | Required. One-line description shown in the picker. |
| `source` | string | Source-relative file to edit. Required unless the entry is `local`. |
| `local` | string | Home-relative folder of unmanaged files, edited through a file picker. Excludes `source`, `diff` and `note`. |
| `check` | list of strings | Command run on the edited file before anything is applied. `{file}` in an argument is replaced by the edited file. |
| `note` | string | Printed after an apply. |
| `diff` | string | Only `"first"`: the preview diff shows the first changed target only. The apply still covers every target. |

Any other key is rejected. A `local` folder must be relative to the home
directory: it is rejected when it is empty, starts with `/`, `~`, `./` or
`../`, or contains `/../`.

At edit time a failed `check` leaves the source with the edit and applies
nothing; for a `local` entry the check runs on the edited copy and the editor
reopens on request.

## Findings

`lint registry` prints one finding per line as `<where>: <message>`, where
`<where>` is the entry name (or a source path for coverage findings):

- `not a table`
- `unknown key <key>`
- `desc is required`
- `source is required`
- `local must be a folder relative to the home dir`
- `local excludes source, diff and note`
- `diff must be "first"`
- `check must be a command as a list: ["make", "lint"], with {file} for the edited file`
- `check must be a list of strings`
- `source <path> does not exist`
- `<path>: no edit registry entry`

## Coverage

Every `source` must exist. Every file under `.chezmoidata/` and every `.age`
file in the source needs an entry whose `source` is that path, otherwise the
finding is `<path>: no edit registry entry`.

This includes `.chezmoidata/edit.toml` itself. The first `chezmoi dots add` on a
source whose registry file has no entry creates the file with the new entry and
then validates the registry: it exits 1 with `.chezmoidata/edit.toml: no edit
registry entry` and keeps the entry. Fix it by registering the registry file:

```toml
[edit.registry]
desc = "Edit registry"
source = ".chezmoidata/edit.toml"
```
