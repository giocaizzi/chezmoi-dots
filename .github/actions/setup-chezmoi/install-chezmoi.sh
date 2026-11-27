#!/usr/bin/env bash
# Install a pinned, checksum-verified chezmoi release and add it to PATH.
# Hashes come from chezmoi_2.74.0_checksums.txt of the twpayne/chezmoi v2.74.0 release.
set -euo pipefail

CHEZMOI_VERSION=2.74.0
SHA_LINUX_AMD64=1b98a567f726f41540414aea4caf912699d53e248b29ccf8369cf553657b2374
SHA_DARWIN_ARM64=9d89672a597a9333ab47ea5173fcececb85c04864205686a7aa5699c3d8bd98c
SHA_WINDOWS_AMD64=13dee2831d371c6ce2ac30fc9ae1aad4f70c68e7d44700f6129fe4b244ee7a95

case "$RUNNER_OS" in
  Linux) asset="linux_amd64.tar.gz"; want="$SHA_LINUX_AMD64" ;;
  macOS) asset="darwin_arm64.tar.gz"; want="$SHA_DARWIN_ARM64" ;;
  Windows) asset="windows_amd64.zip"; want="$SHA_WINDOWS_AMD64" ;;
  *) echo "unsupported runner OS: $RUNNER_OS" >&2; exit 1 ;;
esac
name="chezmoi_${CHEZMOI_VERSION}_${asset}"
dest="$RUNNER_TEMP/chezmoi"
mkdir -p "$dest"
curl -fsSL --retry 3 -o "$dest/$name" "https://github.com/twpayne/chezmoi/releases/download/v${CHEZMOI_VERSION}/${name}"
got=$(openssl dgst -sha256 "$dest/$name" | awk '{print $NF}')
if [ "$got" != "$want" ]; then
  echo "checksum mismatch for $name: got $got, want $want" >&2
  exit 1
fi
case "$asset" in
  *.zip) 7z x -y -o"$dest" "$dest/$name" > /dev/null ;;
  *) tar -xzf "$dest/$name" -C "$dest" ;;
esac
if [ "$RUNNER_OS" = Windows ]; then cygpath -w "$dest" >> "$GITHUB_PATH"; else echo "$dest" >> "$GITHUB_PATH"; fi
