# Package registry

The package registry is `.chezmoidata/packages.toml` in your chezmoi source directory. It declares the tools your dotfiles install: packages for the system package managers and pinned release binaries. `chezmoi-dots` validates it (`lint packages`), surveys it (`pkg status`), writes to it (`pkg add`) and rewrites its pins (`pkg bump`). It does not install anything.

What the keys mean at install time is up to your own chezmoi templates (externals, install scripts and three `.chezmoitemplates` files, described [below](#what-your-dotfiles-provide)). `chezmoi-dots` only checks the shape, so the same registry can drive any install scheme. Commit-pinned archive externals are not in the registry; they live in `.chezmoiexternal` files (see [pkg status](cli.md#pkg-status)).

## Packages

One table per package: `[package.<name>]`.

```toml
[package.example-lint]
brew = "example-lint"
apt = "example-lint"
winget = "Octocat.ExampleLint"
roles = ["personal", "work"]
```

| Key | Type | Meaning | Read by |
| --- | --- | --- | --- |
| `brew` | string | Homebrew formula | `pkg status`, `pkg add` |
| `cask` | string | Homebrew cask | `pkg status`, `pkg add` |
| `apt` | string | apt package | `pkg status`, `pkg add` |
| `winget` | string | winget package id | `pkg status`, `pkg add` |
| `uv` | string | uv tool spec | `pkg status`, `pkg add` |
| `npm` | string | npm global package spec (`typescript@5` pins a version prefix) | `pkg status`, `pkg add` |
| `script` | list of strings | Operating systems (`darwin`, `linux`, `windows`) where an official installer script installs it | `pkg status`; `lint` checks the list |
| `az`, `psmodule`, `python`, `ompFont` | any | Further install methods your install scripts understand | `lint` only |
| `agents` | any | Counts as an install method; what it means is up to your templates | `lint` only |
| `fontFile` | any | Required by `ompFont`; what it means is up to your templates | `lint` only |
| `scope` | string | `"machine"` | `lint` only |
| `roles` | list of strings | Roles the package is for: `personal`, `work`, `light` | `lint` checks it; your templates apply it; `pkg add --roles` writes it |
| `requires` | string | Name of a `[data]` answer that must be true | `lint` checks it; your templates apply it; `pkg add --requires` writes it |
| `cmd` | string | The command the package provides (default: the package name) | `pkg status` |
| `versionCmd` | list of strings | argv that prints the installed version (default: `[<cmd>, "--version"]`) | `pkg status` |

`lint packages` reports, per package:

- `unknown key <key>`: any key not in the table above.
- `no install method (one of ...)`: none of the install keys (`brew`, `cask`, `apt`, `winget`, `uv`, `npm`, `az`, `psmodule`, `python`, `ompFont`, `script`) and no `agents`.
- `script must list operating systems`: `script` is not a list of `darwin`, `linux`, `windows`.
- `scope must be "machine"`.
- `ompFont needs fontFile`.
- `roles must be a subset of [personal work light]`.
- `requires must name a [data] answer`: not a non-empty string. It does not check that the answer exists.
- `versionCmd must be a non-empty argv list`: a list of non-empty strings.

`pkg add` writes only `brew`, `cask`, `apt`, `winget`, `uv`, `npm`, `roles` and `requires`, in that order. See [pkg add](cli.md#pkg-add).

## Releases

One table per pinned release binary, with two sub-tables: `[release.<name>]`, `[release.<name>.target]` and `[release.<name>.sha256]`.

```toml
[release.example-tool]
version = "1.2.3"
url = "https://github.com/octocat/example-tool/releases/download/v{version}/example-tool_{version}_{target}.tar.gz"
type = "archive-file"
path = "example-tool"
exe = true
roles = ["personal", "work"]

[release.example-tool.target]
"darwin/arm64" = "darwin_arm64"
"linux/amd64" = "linux_amd64"

[release.example-tool.sha256]
"darwin/arm64" = "2222222222222222222222222222222222222222222222222222222222222222"
"linux/amd64" = "3333333333333333333333333333333333333333333333333333333333333333"
```

| Key | Type | Meaning | Read by |
| --- | --- | --- | --- |
| `version` | string | The pinned version, without a leading `v`. Required | `pkg status`, `pkg bump` (rewrites it) |
| `url` | string | Download URL. Must contain `{version}`; may contain `{target}` | `pkg status`, `pkg bump`, `pkg add` |
| `type` | string | `file`, `archive` or `archive-file` | `lint` checks it; your externals template uses it |
| `path` | string | The file inside an archive (for `archive-file`) | your templates only |
| `dest` | string | Home-relative install path | your `releases` template only |
| `stripComponents` | any | Leading path components to strip from an archive | your templates only |
| `exe` | bool | The binary gets `.exe` on Windows | your templates only (`pkg add` writes it) |
| `roles`, `requires` | as for packages | Limit the release | `lint` checks them; your templates apply them |
| `versionCmd` | list of strings | argv printing the installed version; `{dest}` expands to the absolute install path (default: `["{dest}", "--version"]`) | `pkg status` |
| `target` | table | `"<os>/<arch>"` to the value `{target}` takes. Required, not empty | `pkg status`, `pkg bump` (`pkg add` writes it) |
| `sha256` | table | `"<os>/<arch>"` to the 64 lowercase hex digits of that target's download | `pkg bump` (rewrites it), `lint` |

Rules `lint packages` enforces, per release: `unknown key`, `version is required`, `url must contain {version}`, `type must be file, archive or archive-file`, the `roles`, `requires` and `versionCmd` rules above, `target is required`, `target <t> must be <os>/<arch>` (`darwin`, `linux` or `windows`, then `amd64` or `arm64`), `target <t> has no valid sha256` (not 64 lowercase hex digits; `pkg bump` fills it), and `sha256 <t> has no target` (a checksum for a target not in `target`).

### Upstream versions

`pkg status` and `pkg bump` find the newest version from the release's `url`, which must have one of these shapes (the text before `{version}` in the tag is kept as the tag prefix):

| URL shape | Looked up in |
| --- | --- |
| `https://github.com/<owner>/<repo>/releases/download/[<prefix>]{version}/...` | the repository's tags |
| `https://gitlab.com/<group>/<repo>/-/releases/[<prefix>]{version}/...` | the repository's tags |
| `https://releases.hashicorp.com/<product>/{version}/...` | `hashicorp/<product>` tags (prefix `v`) |
| `https://go.dev/dl/go{version}...` | the Go download index |

Any other URL still validates and pins, but its latest version is reported as unknown and `pkg bump` cannot update it.

`pkg bump` rewrites only the `version` line and the `sha256` values of the release, as text: comments, order and everything else stay.

## What your dotfiles provide

`chezmoi-dots` does not know how your tools are installed. `pkg status` asks your dotfiles through chezmoi, so it applies the same selection rules as your install scripts. Provide three templates in `.chezmoitemplates/`:

| Template | Called with | Prints |
| --- | --- | --- |
| `role` | the root data | `personal`, `work` or `light` |
| `releases` | the root data | A JSON object: release name to `{"dest": "<home-relative path>"}`, for the releases this machine installs |
| `packages` | `dict "manager" <m> "ctx" <root data>` | A JSON list of `{"name": ..., "id": ..., "entry": <the package table>}` for the packages of manager `<m>` this machine installs |

The managers are `brew`, `cask`, `apt`, `winget`, `uv`, `npm` and `script`, each asked once. Details of the contract, from the code:

- `id` must be a string. For `script` the key's value is a list, so use the package name as the `id` there.
- Only the managers of this OS are surveyed: `brew` and `cask` on macOS, `apt` on Linux, `winget` on Windows; `uv` and `npm` everywhere.
- A `script` package is dropped when its table has this OS's manager key (`brew` or `cask`, `apt`, `winget`): it is installed by the manager there. Filter `script` packages to the OSes listed in the key yourself, as the example does.
- Per package, `cmd` defaults to the name and `versionCmd` to `[<cmd>, "--version"]`, read from `entry`.
- Per release, `versionCmd` is read from the registry table and defaults to `["{dest}", "--version"]`; `dest` comes from your `releases` template, so its default (`.local/bin/<name>`) and the `.exe` suffix are decided there.
- A release in the `releases` output that has no table in the registry is surveyed without a version or URL.
- `roles`, `requires`, `type`, `path`, `dest`, `stripComponents`, `exe` and the install keys have no effect unless these templates and your externals and install scripts read them.

`pkg add --github` reads `path` from the downloaded archives and may write it with `{version}` and `{target}` placeholders (for example `example-tool_{version}_{target}/example-tool`), or the repository name when the archives do not settle it. It never writes `.exe` in `path`, and writes `exe = true` when a Windows asset is mapped. Your externals template should therefore replace both placeholders in `path` and append `.exe` to it on Windows when `exe` is set, as the example below does. See [pkg add](cli.md#pkg-add).

## Worked example

A registry with two packages and one release, and the templates that read it. Everything runs in a scratch directory with no network.

`.chezmoidata/packages.toml`:

```toml
[package.example-lint]
brew = "example-lint"
apt = "example-lint"
winget = "Octocat.ExampleLint"
roles = ["personal", "work"]

# Only where the answer `gui` is true in chezmoi's [data].
[package.example-viewer]
cask = "example-viewer"
winget = "Octocat.ExampleViewer"
requires = "gui"

[release.example-tool]
version = "1.2.3"
url = "https://github.com/octocat/example-tool/releases/download/v{version}/example-tool_{version}_{target}.tar.gz"
type = "archive-file"
path = "example-tool"
exe = true
roles = ["personal", "work"]

[release.example-tool.target]
"darwin/amd64" = "darwin_amd64"
"darwin/arm64" = "darwin_arm64"
"linux/amd64" = "linux_amd64"
"linux/arm64" = "linux_arm64"
"windows/amd64" = "windows_amd64"

[release.example-tool.sha256]
"darwin/amd64" = "1111111111111111111111111111111111111111111111111111111111111111"
"darwin/arm64" = "2222222222222222222222222222222222222222222222222222222222222222"
"linux/amd64" = "3333333333333333333333333333333333333333333333333333333333333333"
"linux/arm64" = "4444444444444444444444444444444444444444444444444444444444444444"
"windows/amd64" = "5555555555555555555555555555555555555555555555555555555555555555"
```

`.chezmoitemplates/role`: the role comes from `[data]` answers (`light = true`, `work = true`), personal otherwise.

```text
{{- if get . "light" -}}light{{- else if get . "work" -}}work{{- else -}}personal{{- end -}}
```

`.chezmoitemplates/releases`: the releases for this role, answers and OS/arch, with their destination.

```text
{{- $role := includeTemplate "role" . | trim -}}
{{- $plat := printf "%s/%s" .chezmoi.os .chezmoi.arch -}}
{{- $out := dict -}}
{{- range $name, $r := (get . "release" | default dict) -}}
{{-   if and (or (not (hasKey $r "roles")) (has $role $r.roles)) (or (not (hasKey $r "requires")) (get $ $r.requires)) (hasKey $r.target $plat) -}}
{{-     $dest := get $r "dest" | default (printf ".local/bin/%s" $name) -}}
{{-     if and (get $r "exe") (eq $.chezmoi.os "windows") -}}
{{-       $dest = printf "%s.exe" $dest -}}
{{-     end -}}
{{-     $_ := set $out $name (dict "dest" $dest) -}}
{{-   end -}}
{{- end -}}
{{- $out | toJson -}}
```

`.chezmoitemplates/packages`: the packages of one manager, filtered the same way.

```text
{{- $m := .manager -}}
{{- $role := includeTemplate "role" .ctx | trim -}}
{{- $os := .ctx.chezmoi.os -}}
{{- $out := list -}}
{{- range $name, $e := (get .ctx "package" | default dict) -}}
{{-   if and (hasKey $e $m) (or (not (hasKey $e "roles")) (has $role $e.roles)) (or (not (hasKey $e "requires")) (get $.ctx $e.requires)) -}}
{{- /* script = ["darwin", ...]: an installer for those OSes; its id is the name */ -}}
{{-     if eq $m "script" -}}
{{-       if has $os $e.script -}}
{{-         $out = append $out (dict "name" $name "id" $name "entry" $e) -}}
{{-       end -}}
{{-     else -}}
{{-       $out = append $out (dict "name" $name "id" (index $e $m) "entry" $e) -}}
{{-     end -}}
{{-   end -}}
{{- end -}}
{{- $out | toJson -}}
```

`.chezmoiexternal.toml.tmpl`: installs the releases the `releases` template selected, so the filter lives in one place.

```text
{{- $plat := printf "%s/%s" .chezmoi.os .chezmoi.arch -}}
{{- range $name, $sel := includeTemplate "releases" . | fromJson -}}
{{-   $r := index $.release $name -}}
{{-   $target := index $r.target $plat -}}
{{-   $path := get $r "path" | default "" | replace "{version}" $r.version | replace "{target}" $target -}}
{{-   if and $path (get $r "exe") (eq $.chezmoi.os "windows") -}}
{{-     $path = printf "%s.exe" $path -}}
{{-   end }}
[{{ $sel.dest | quote }}]
    type = {{ get $r "type" | default "file" | quote }}
    url = {{ $r.url | replace "{version}" $r.version | replace "{target}" $target | quote }}
    executable = true
{{- if $path }}
    path = {{ $path | quote }}
{{- end }}
    checksum.sha256 = {{ index $r.sha256 $plat | quote }}
{{ end -}}
```

On macOS arm64 with `gui = true` in the `[data]` of the chezmoi config, rendering the externals template gives:

```toml
[".local/bin/example-tool"]
    type = "archive-file"
    url = "https://github.com/octocat/example-tool/releases/download/v1.2.3/example-tool_1.2.3_darwin_arm64.tar.gz"
    executable = true
    path = "example-tool"
    checksum.sha256 = "2222222222222222222222222222222222222222222222222222222222222222"
```

and `chezmoi dots pkg status --offline` lists `example-tool` under pinned releases, `example-lint` and `example-viewer` under Homebrew. With `light = true` and no `gui` answer (or `gui = false`), nothing is selected: the release and `example-lint` are for `personal` and `work`, and the viewer needs `gui`. With `light = true` and `gui = true`, only `example-viewer` is selected, since it sets no `roles`. `chezmoi dots lint packages` passes, and `chezmoi dots pkg add example-cli --brew --dry-run` prints `[package.example-cli]` with `brew = "example-cli"`.

To try a registry without touching your machine, point `HOME`, the `XDG_*` variables, `CHEZMOI_SOURCE_DIR` and `CHEZMOI_CONFIG_FILE` at a scratch directory, and give plain `chezmoi` the matching `--source`, `--config` and `--destination` flags.
