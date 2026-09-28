#!/bin/sh
# Installs the latest release of this gentle-ai fork on macOS or Linux:
#
#   curl -fsSL https://raw.githubusercontent.com/santiago-ramos-02/gentle-ai/main/scripts/t3-install.sh | sh
#
# It replaces the gentle-ai already on PATH, or installs to ~/.local/bin. From
# then on gentle-ai updates itself from the fork's releases. GENTLE_AI_FORK
# names another fork to install from.
set -eu

repo="${GENTLE_AI_FORK:-santiago-ramos-02/gentle-ai}"
case "$(uname -s)" in
  Darwin) os=darwin ;;
  Linux) os=linux ;;
  *) echo "Unsupported system: $(uname -s). On Windows, use t3-install.ps1." >&2; exit 1 ;;
esac
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  arm64 | aarch64) arch=arm64 ;;
  *) echo "Unsupported architecture: $(uname -m)" >&2; exit 1 ;;
esac

tag=$(curl -fsSL "https://api.github.com/repos/$repo/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1)
[ -n "$tag" ] || { echo "Could not find the latest release of $repo" >&2; exit 1; }
name="gentle-ai_${tag#v}_${os}_${arch}"
download="https://github.com/$repo/releases/download/$tag"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
curl -fsSL "$download/$name.tar.gz" -o "$work/$name.tar.gz"
curl -fsSL "$download/checksums.txt" -o "$work/checksums.txt"
expected=$(awk -v file="$name.tar.gz" '$2 == file { print $1 }' "$work/checksums.txt")
[ -n "$expected" ] || { echo "$name.tar.gz is not listed in the release's checksums.txt" >&2; exit 1; }
if command -v sha256sum > /dev/null; then
  actual=$(sha256sum "$work/$name.tar.gz" | cut -d ' ' -f 1)
else
  actual=$(shasum -a 256 "$work/$name.tar.gz" | cut -d ' ' -f 1)
fi
[ "$actual" = "$expected" ] || { echo "$name.tar.gz does not match its checksum" >&2; exit 1; }
tar -xzf "$work/$name.tar.gz" -C "$work"

target=$(command -v gentle-ai || true)
if [ -z "$target" ]; then
  target="$HOME/.local/bin/gentle-ai"
  case ":$PATH:" in
    *":$HOME/.local/bin:"*) ;;
    *) echo "Add $HOME/.local/bin to your PATH to use gentle-ai." ;;
  esac
fi
mkdir -p "$(dirname "$target")"
# Moving the new binary into place keeps any running gentle-ai working.
cp "$work/$name/gentle-ai" "$target.new"
chmod 755 "$target.new"
mv -f "$target.new" "$target"
echo "Installed $("$target" version) at $target"
