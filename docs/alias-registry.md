# Alias registry

The alias registry declares the shell aliases a dotfiles source defines. It is
read as chezmoi data, through the keys `alias`, `aliasBlock` and `aliasExternal`
(the package documentation places it in `.chezmoidata/aliases.toml`, but any
chezmoi data with these keys is read). [`lint aliases`](cli.md#lint) validates
the registry and [`audit aliases`](cli.md#audit) compares it with the aliases
live in your shells.

`chezmoi-dots` does not render the alias file: your own templates do. The
registry is only data that those templates and the two commands share.

## Keys

`[alias.<name>]` declares an alias. `<name>` must match
`^[A-Za-z0-9._][A-Za-z0-9._-]*$`. Allowed keys:

| Key | Meaning |
|---|---|
| `cmd` | One command for every shell. Exclusive with `posix` and `powershell`. |
| `posix` | Command for POSIX shells. |
| `powershell` | Command for PowerShell. |
| `shells` | Non-empty subset of `["posix", "powershell"]`; all shells when absent. |
| `os` | List within `["darwin", "linux", "windows"]`. |
| `machines` | List of non-empty machine names. |
| `why` | Reason; required when `shells` has a single shell. |

`[aliasBlock.<name>]` is exactly a `message` string and nothing else.
`[aliasExternal.<shell>.<name>]` is a boolean.

```toml
[alias.ll]
posix = "ls -l"
powershell = "Get-ChildItem"

[alias.gs]
cmd = "git status"

[alias.open]
posix = "xdg-open"
shells = ["posix"]
why = "no PowerShell equivalent"

[aliasBlock.example]
message = "Aliases from the example block"
```

## Findings

`lint aliases` prints `<where>: <message>`. For an alias `<where>` is
`alias <name>`; for a block `block <name>`; for an external
`external <shell>.<name>`.

- `invalid name`
- `unknown key <key>`
- `use cmd or posix/powershell, not both`
- `no <shell> command (add one, or limit shells with why)`
- `<shell> command set but <shell> not in shells`
- `shells must be a non-empty subset of [posix powershell]`
- `limited to [<shell>] without why`
- `os must be a subset of [darwin linux windows]`
- `machines must be a list of non-empty names`
- `needs exactly a message string` (block)
- `must be true or false` (external)

## Audit

`audit aliases` reads the alias file your templates rendered,
`$XDG_CONFIG_HOME/shell/aliases.sh` (default `~/.config/shell/aliases.sh`), and
the optional `shell/omz-aliases.zsh` beside it. The Oh My Zsh plugins that file
enables are read from `~/.oh-my-zsh/plugins`. For each of `zsh` and `bash` found
on `PATH` it runs an interactive shell (`zsh -ic`, `bash -ic`) to list the live
aliases and prints, per shell:

```text
zsh: all aliases accounted for
bash: aliases not in the registry:
  <alias>
```

Unaccounted aliases make the check exit 1.

If `shell/aliases.sh` is missing, the check fails with `read the rendered
aliases (chezmoi apply?)` and exit 1. This is an error, not a finding, so a
plain `chezmoi dots audit` stops there and the `secrets` check does not run;
apply your dotfiles first, or run `chezmoi dots audit secrets`.
