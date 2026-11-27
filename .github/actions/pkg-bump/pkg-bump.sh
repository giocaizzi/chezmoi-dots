#!/usr/bin/env bash
# Logic of the pkg-bump composite action: install | bump | pr.
# Every input arrives through the environment; nothing is interpolated into a script.
#
#   PKG_BUMP_VERSION  chezmoi-dots release to install, 0.4.1 or v0.4.1 (install, pr)
#   PKG_BUMP_SOURCE   source dir relative to the workspace (bump, pr)
#   PKG_BUMP_NAMES    whitespace-separated pins to bump, empty for all (bump)
#   PKG_BUMP_DRY_RUN  true or false (bump, pr)
#   PKG_BUMP_BRANCH, PKG_BUMP_BASE, PKG_BUMP_TITLE  pull request settings (pr)
#   GH_TOKEN          only the pr step receives it
#   PKG_BUMP_DOWNLOAD_BASE  test override of the release download base (install)
set -euo pipefail

die() { echo "pkg-bump: $*" >&2; exit 1; }

normalize_version() {
  local v=${PKG_BUMP_VERSION:-}
  [[ $v =~ ^v?[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] || die "invalid version '$v'"
  echo "${v#v}"
}

check_branch() {
  local b=${PKG_BUMP_BRANCH:-}
  [[ $b =~ ^[A-Za-z0-9._/-]+$ && $b != -* && $b != /* && $b != */ && $b != *..* && $b != *//* && $b != *.lock && $b != refs/* ]] \
    || die "invalid branch '$b'"
}

source_path() {
  local s=${PKG_BUMP_SOURCE:-.}
  [[ $s =~ ^[A-Za-z0-9._/-]+$ && $s != /* && $s != *..* ]] || die "invalid source-dir '$s'"
  echo "${GITHUB_WORKSPACE:-$PWD}/$s"
}

cmd_install() {
  local v os arch asset base dir want got proto
  v=$(normalize_version)
  case "${RUNNER_OS:-}/${RUNNER_ARCH:-}" in
    Linux/X64) os=linux; arch=amd64 ;;
    macOS/ARM64) os=darwin; arch=arm64 ;;
    *) die "unsupported runner ${RUNNER_OS:-?}/${RUNNER_ARCH:-?}; use Linux X64 or macOS ARM64" ;;
  esac
  asset="chezmoi-dots_${v}_${os}_${arch}"
  base=${PKG_BUMP_DOWNLOAD_BASE:-https://github.com/giocaizzi/chezmoi-dots/releases/download}
  dir=$(mktemp -d "${RUNNER_TEMP:?}/chezmoi-dots.XXXXXX")
  # https only, redirects included; the test override may use file://.
  proto='=https'
  [ -z "${PKG_BUMP_DOWNLOAD_BASE:-}" ] || proto='=https,file'
  curl -fsSL --proto "$proto" --proto-redir "$proto" --retry 3 -o "$dir/$asset" "$base/v$v/$asset"
  curl -fsSL --proto "$proto" --proto-redir "$proto" --retry 3 -o "$dir/checksums.txt" "$base/v$v/checksums.txt"
  want=$(awk -v n="$asset" '$2 == n {print $1; exit}' "$dir/checksums.txt")
  [ -n "$want" ] || die "no checksum for $asset in checksums.txt"
  got=$(openssl dgst -sha256 "$dir/$asset" | awk '{print $NF}')
  [ "$got" = "$want" ] || die "checksum mismatch for $asset: got $got, want $want"
  mv "$dir/$asset" "$dir/chezmoi-dots"
  chmod +x "$dir/chezmoi-dots"
  echo "$dir" >> "${GITHUB_PATH:?}"
  "$dir/chezmoi-dots" version
}

cmd_bump() {
  local src name
  local -a args=(pkg bump) names=()
  src=$(source_path)
  [ -d "$src" ] || die "source-dir '$src' is not a directory"
  [ "${PKG_BUMP_DRY_RUN:-false}" = true ] && args+=(--dry-run)
  # Split on any whitespace, newlines included (a YAML block); read -d '' returns 1 at EOF.
  read -r -d '' -a names <<< "${PKG_BUMP_NAMES:-}" || true
  for name in ${names[@]+"${names[@]}"}; do
    [[ $name =~ ^[A-Za-z0-9._/-]+$ && $name != -* ]] || die "invalid pin name '$name'"
    args+=("$name")
  done
  CHEZMOI_SOURCE_DIR=$src chezmoi-dots "${args[@]}"
}

set_output() { echo "$1=$2" >> "${GITHUB_OUTPUT:?}"; }

# resolve_base sets base (default: the repository default branch) and refuses a branch that is
# the base or the default branch, which the force push would overwrite.
resolve_base() {
  default_branch=$(gh repo view "${GITHUB_REPOSITORY:?}" --json defaultBranchRef -q .defaultBranchRef.name)
  base=${PKG_BUMP_BASE:-$default_branch}
  [ "$branch" != "$base" ] && [ "$branch" != "$default_branch" ] \
    || die "branch '$branch' is the base or the default branch; it is force-pushed, use a dedicated branch"
}

# find_pr prints the URL of an open pull request from this repository on the branch, if any.
# gh matches --head by branch name alone, so fork PRs of the same name are filtered out.
find_pr() {
  gh pr list --head "$branch" --base "$base" --state open --json url,isCrossRepository \
    -q 'map(select(.isCrossRepository | not))[0].url // empty'
}

cmd_pr() {
  local src v branch base default_branch title url body rel
  v=$(normalize_version)
  check_branch
  branch=$PKG_BUMP_BRANCH
  title=${PKG_BUMP_TITLE:?}
  src=$(source_path)
  cd "$src"
  rel=$(git rev-parse --show-prefix)
  rel=${rel:-.}
  cd "$(git rev-parse --show-toplevel)"

  # Only the source directory counts: a tracked file changed elsewhere is not a pin bump.
  if [ -z "$(git status --porcelain --untracked-files=no -- "$rel")" ]; then
    set_output changed false
    echo "all pins are up to date"
    # pkg bump --dry-run writes nothing; a dry run makes no gh call.
    [ "${PKG_BUMP_DRY_RUN:-false}" = true ] && return 0
    resolve_base
    url=$(find_pr)
    if [ -n "$url" ]; then
      echo "::notice::pull request $url was left open and may be stale; close it if the pins were bumped elsewhere"
    fi
    return 0
  fi
  # pkg bump --dry-run writes nothing, so a change here was not made by it.
  if [ "${PKG_BUMP_DRY_RUN:-false}" = true ]; then
    set_output changed false
    echo "dry run: no commit or pull request made"
    return 0
  fi
  resolve_base
  set_output changed true

  if git config --get-regexp '^http\..*\.extraheader' > /dev/null; then
    echo "::warning::the checkout persisted a credential; the push uses it instead of the token input. Set persist-credentials: false on actions/checkout."
  fi

  git switch -C "$branch"
  git add -u -- "$rel"
  git -c user.name='github-actions[bot]' -c user.email='41898282+github-actions[bot]@users.noreply.github.com' commit -q -m "$title"
  git -c credential.helper= -c 'credential.helper=!gh auth git-credential' push --force origin "HEAD:refs/heads/$branch"

  tick='`'
  body=$(printf 'Versions, archive commits and checksums below come from %schezmoi-dots pkg bump%s (chezmoi-dots %s).\n\n%s%s%s\n%s\n%s%s%s\n' \
    "$tick" "$tick" "$v" "$tick" "$tick" "$tick" "$(git diff --stat HEAD~1)" "$tick" "$tick" "$tick")
  url=$(find_pr)
  if [ -n "$url" ]; then
    gh pr edit "$url" --title "$title" --body "$body"
  else
    url=$(gh pr create --base "$base" --head "$branch" --title "$title" --body "$body")
  fi
  set_output pull-request-url "$url"
  echo "pull request: $url"
}

case "${GITHUB_EVENT_NAME:-}" in
  pull_request*) die "pkg-bump does not run on pull_request events: the checkout is pull request code" ;;
esac

case "${1:-}" in
  install) cmd_install ;;
  bump) cmd_bump ;;
  pr) cmd_pr ;;
  *) die "usage: pkg-bump.sh install|bump|pr" ;;
esac
