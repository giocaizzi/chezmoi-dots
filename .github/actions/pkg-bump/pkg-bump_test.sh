#!/usr/bin/env bash
# Hermetic tests of pkg-bump.sh: no network, fake gh and chezmoi-dots on PATH, local bare origin.
set -uo pipefail

script=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/pkg-bump.sh
root=$(mktemp -d)
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_SYSTEM=/dev/null
trap 'rm -rf "$root"' EXIT
fails=0
token=ghs_TESTTOKEN_must_never_leak

pass() { echo "ok   $1"; }
fail() { echo "FAIL $1"; fails=$((fails + 1)); }
# check <name> <command...>: passes when the command succeeds.
check() { local n=$1; shift; if "$@"; then pass "$n"; else fail "$n"; fi; }
# xfail <name> <command...>: a known bug, strict. Expected to fail; a pass fails the suite so the
# fix removes the marker and turns the line into a check.
xfail() {
  local n=$1; shift
  if "$@"; then echo "XPASS $n: fixed, turn this xfail into a check"; fails=$((fails + 1)); else echo "xfail $n"; fi
}
# rejects <name> <env...> -- <subcommand>: passes when the script exits non-zero.
rejects() {
  local n=$1; shift
  local -a envs=()
  while [ "$1" != -- ]; do envs+=("$1"); shift; done
  shift
  if env "${envs[@]}" "$script" "$@" > "$root/out" 2>&1; then fail "$n"; else pass "$n"; fi
}

# Fake gh: logs argv, answers the calls pr makes. FAKE_GH_PRS, a JSON list of open PRs as the API
# returns them for the head branch name, is filtered with the caller's -q expression.
mkdir -p "$root/bin"
cat > "$root/bin/gh" <<'EOF'
#!/usr/bin/env bash
echo "gh $*" >> "$FAKE_GH_LOG"
q=; prev=
for a in "$@"; do [ "$prev" = -q ] && q=$a; prev=$a; done
case "$1 $2" in
  "repo view") echo main ;;
  "pr list") if [ -n "${FAKE_GH_PRS:-}" ]; then jq -r "$q // empty" <<< "$FAKE_GH_PRS"; else echo "${FAKE_GH_EXISTING:-}"; fi ;;
  "pr create") echo "https://example.test/example/harness/pull/7" ;;
esac
exit 0
EOF
cat > "$root/bin/chezmoi-dots" <<'EOF'
#!/usr/bin/env bash
echo "chezmoi-dots $* source=$CHEZMOI_SOURCE_DIR token=${GH_TOKEN:-none}" >> "$FAKE_DOTS_LOG"
EOF
chmod +x "$root/bin/gh" "$root/bin/chezmoi-dots"

# --- validation ---
rejects "version: empty rejected" PKG_BUMP_VERSION= -- install
rejects "version: injection rejected" 'PKG_BUMP_VERSION=1.2.3; rm -rf /' -- install
rejects "version: two parts rejected" PKG_BUMP_VERSION=1.2 -- install
rejects "install: unsupported runner" PKG_BUMP_VERSION=0.4.1 RUNNER_OS=Windows RUNNER_ARCH=X64 -- install
rejects "name: bad name rejected" PKG_BUMP_SOURCE=. "PKG_BUMP_NAMES=ok \$(id)" GITHUB_WORKSPACE="$root" PATH="$root/bin:$PATH" FAKE_DOTS_LOG="$root/dots.log" -- bump
rejects "name: leading dash rejected" PKG_BUMP_SOURCE=. PKG_BUMP_NAMES='--evil' GITHUB_WORKSPACE="$root" PATH="$root/bin:$PATH" FAKE_DOTS_LOG="$root/dots.log" -- bump
rejects "source: parent dir rejected" PKG_BUMP_SOURCE=../x GITHUB_WORKSPACE="$root" -- bump
for b in '-x' 'a..b' 'a b' 'a;b' '/a' 'a/' 'x.lock'; do
  rejects "branch: '$b' rejected" PKG_BUMP_VERSION=0.4.1 "PKG_BUMP_BRANCH=$b" PKG_BUMP_TITLE=t GITHUB_WORKSPACE="$root" GITHUB_OUTPUT="$root/o" -- pr
done

# --- install ---
version=0.4.1
asset=chezmoi-dots_${version}_linux_amd64
dl="$root/dl/v$version"
mkdir -p "$dl"
printf '#!/bin/sh\necho "chezmoi-dots test build"\n' > "$dl/$asset"
(cd "$dl" && openssl dgst -sha256 "$asset" | awk -v n="$asset" '{print $NF "  " n}' > checksums.txt)
install_env=(PKG_BUMP_VERSION="v$version" RUNNER_OS=Linux RUNNER_ARCH=X64 RUNNER_TEMP="$root/tmp" GITHUB_PATH="$root/path" "PKG_BUMP_DOWNLOAD_BASE=file://$root/dl")
mkdir -p "$root/tmp"

check "install: good checksum accepted" env "${install_env[@]}" "$script" install > "$root/out" 2>&1
check "install: binary on PATH file and runnable" bash -c "[ -x \"\$(head -n1 '$root/path')/chezmoi-dots\" ]"

echo tampered >> "$dl/$asset"
rejects "install: tampered binary rejected" "${install_env[@]}" -- install
check "install: mismatch message" grep -q 'checksum mismatch' "$root/out"

grep -v "$asset" "$dl/checksums.txt" > "$dl/c2" || true
mv "$dl/c2" "$dl/checksums.txt"
rejects "install: missing checksums line rejected" "${install_env[@]}" -- install
check "install: missing line message" grep -q 'no checksum for' "$root/out"

# --- bump ---
ws="$root/ws"
mkdir -p "$ws/home"
: > "$root/dots.log"
env PKG_BUMP_SOURCE=home PKG_BUMP_NAMES='alpha  beta/x' PKG_BUMP_DRY_RUN=true GITHUB_WORKSPACE="$ws" PATH="$root/bin:$PATH" FAKE_DOTS_LOG="$root/dots.log" GH_TOKEN=$token "$script" bump > "$root/out" 2>&1
check "bump: args, dry-run and source dir" grep -qF "chezmoi-dots pkg bump --dry-run alpha beta/x source=$ws/home" "$root/dots.log"

# A YAML block (names: |) separates pins by newlines; read -a stops at the first one.
: > "$root/dots.log"
env PKG_BUMP_SOURCE=home "PKG_BUMP_NAMES=alpha"$'\n'"beta" GITHUB_WORKSPACE="$ws" PATH="$root/bin:$PATH" FAKE_DOTS_LOG="$root/dots.log" "$script" bump > "$root/out" 2>&1
xfail "bump: newline-separated names all reach pkg bump" grep -qF "chezmoi-dots pkg bump alpha beta source=" "$root/dots.log"

# --- pr ---
origin="$root/origin.git"
repo="$root/ws2"
git init -q --template= --bare "$origin"
git init -q --template= -b main "$repo"
git -C "$repo" config user.name t
git -C "$repo" config user.email t@example.test
git -C "$repo" remote add origin "$origin"
echo "version = 1" > "$repo/pins.toml"
git -C "$repo" add pins.toml
git -C "$repo" commit -q -m init
git -C "$repo" push -q origin main
export GITHUB_REPOSITORY=example/harness
pr_env=(PKG_BUMP_VERSION=0.4.1 PKG_BUMP_SOURCE=. PKG_BUMP_BRANCH=chore/bump-releases PKG_BUMP_TITLE="chore(deps): bump pinned releases" GITHUB_WORKSPACE="$repo" GITHUB_OUTPUT="$root/gh_out" PATH="$root/bin:$PATH" FAKE_GH_LOG="$root/gh.log" "GH_TOKEN=$token")
: > "$root/gh.log"; : > "$root/gh_out"

env "${pr_env[@]}" "$script" pr > "$root/out" 2>&1
check "pr: no changes sets changed=false" grep -qx 'changed=false' "$root/gh_out"
check "pr: no changes says up to date" grep -q 'all pins are up to date' "$root/out"
check "pr: no changes makes no gh call" test ! -s "$root/gh.log"

echo "version = 2" > "$repo/pins.toml"
: > "$root/gh_out"
env "${pr_env[@]}" PKG_BUMP_DRY_RUN=true "$script" pr > "$root/out" 2>&1
check "pr: dry-run reports no change" grep -qx 'changed=false' "$root/gh_out"
check "pr: dry-run makes no commit" test "$(git -C "$repo" rev-list --count HEAD)" = 1
check "pr: dry-run makes no gh call" test ! -s "$root/gh.log"

: > "$root/gh_out"
env "${pr_env[@]}" "$script" pr > "$root/out" 2>&1
check "pr: change sets changed=true" grep -qx 'changed=true' "$root/gh_out"
check "pr: branch pushed to origin" git -C "$origin" rev-parse --verify --quiet --end-of-options refs/heads/chore/bump-releases >/dev/null
check "pr: commit message is the title" test "$(git -C "$origin" log -1 --format=%s chore/bump-releases)" = "chore(deps): bump pinned releases"
check "pr: commit author is the bot" test "$(git -C "$origin" log -1 --format=%ae chore/bump-releases)" = "41898282+github-actions[bot]@users.noreply.github.com"
check "pr: base resolved from the default branch" grep -q 'gh repo view example/harness' "$root/gh.log"
check "pr: gh pr create with title" grep -qF 'gh pr create --base main --head chore/bump-releases --title chore(deps): bump pinned releases' "$root/gh.log"
check "pr: url written to output" grep -qx 'pull-request-url=https://example.test/example/harness/pull/7' "$root/gh_out"
check "pr: body names chezmoi-dots version" grep -q 'chezmoi-dots 0.4.1' "$root/gh.log"
check "pr: no config persisted a credential helper" test -z "$(git -C "$repo" config --local --get-regexp 'credential' || true)"

# Existing PR: edit, not create.
echo "version = 3" > "$repo/pins.toml"
: > "$root/gh.log"; : > "$root/gh_out"
env "${pr_env[@]}" PKG_BUMP_BASE=main FAKE_GH_EXISTING=https://example.test/example/harness/pull/7 "$script" pr > "$root/out" 2>&1
check "pr: existing PR is edited" grep -q 'gh pr edit https://example.test/example/harness/pull/7 --title' "$root/gh.log"
check "pr: existing PR is not created again" test -z "$(grep 'gh pr create' "$root/gh.log" || true)"
check "pr: explicit base skips repo view" test -z "$(grep 'gh repo view' "$root/gh.log" || true)"

# The token never shows in argv logs or output.
check "token: absent from gh argv log" test -z "$(grep -F "$token" "$root/gh.log" || true)"
check "token: absent from script output" test -z "$(grep -F "$token" "$root/out" || true)"

# Persisted checkout credential triggers a warning.
echo "version = 4" > "$repo/pins.toml"
git -C "$repo" config http.https://example.test/.extraheader "AUTHORIZATION: basic x"
env "${pr_env[@]}" "$script" pr > "$root/out" 2>&1
check "pr: persisted credential warns" grep -q '^::warning::' "$root/out"
check "token: absent from output with warning" test -z "$(grep -F "$token" "$root/out" || true)"
git -C "$repo" config --unset http.https://example.test/.extraheader

# gh pr list --head matches the branch name in any repository, so an open PR from a fork branch
# named like the bump branch is returned: the action must not edit it and must open its own.
echo "version = 5" > "$repo/pins.toml"
: > "$root/gh.log"; : > "$root/gh_out"
fork_pr='[{"url":"https://example.test/example/harness/pull/9","isCrossRepository":true,"headRepositoryOwner":{"login":"fork-owner"}}]'
env "${pr_env[@]}" PKG_BUMP_BASE=main "FAKE_GH_PRS=$fork_pr" "$script" pr > "$root/out" 2>&1
xfail "pr: a fork PR on the same branch name is not edited" test -z "$(grep 'gh pr edit https://example.test/example/harness/pull/9' "$root/gh.log" || true)"
xfail "pr: a fork PR on the same branch name does not stop the PR" grep -q 'gh pr create' "$root/gh.log"

# branch equal to the base (or default) branch: a commit merged on origin after the checkout must
# survive; the force push drops it. The action must refuse before pushing.
git -C "$repo" switch -q -C main origin/main
other="$root/other"
git clone -q -b main "$origin" "$other"
git -C "$other" -c user.name=t -c user.email=t@example.test commit -q --allow-empty -m "merged meanwhile"
git -C "$other" push -q origin main
merged=$(git -C "$other" rev-parse HEAD)
echo "version = 6" > "$repo/pins.toml"
: > "$root/gh.log"
env "${pr_env[@]}" PKG_BUMP_BRANCH=main "$script" pr > "$root/out" 2>&1
xfail "pr: branch equal to the default branch never force-pushes it" git -C "$origin" merge-base --is-ancestor "$merged" refs/heads/main

echo
if [ "$fails" -ne 0 ]; then echo "$fails failure(s)"; exit 1; fi
echo "all pkg-bump tests passed"
