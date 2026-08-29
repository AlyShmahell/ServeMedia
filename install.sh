#!/usr/bin/env bash
# Install a published Medora release into ~/.medora (userscope desktop + bin).
set -euo pipefail

REPO="${MEDORA_REPO:-AlyShmahell/medora}"
DEST="${MEDORA_HOME:-$HOME/.medora}"
BIN_DIR="${XDG_BIN_HOME:-$HOME/.local/bin}"
APP_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/applications"
ICON_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/scalable/apps"

usage() {
  cat <<EOF
Usage: curl -fsSL https://raw.githubusercontent.com/AlyShmahell/medora/main/install.sh | bash
  Interactive TTY menu: pick a GitHub release, then slim or bundled.
  Installs into ~/.medora, links ~/.local/bin/medora, and installs a userscope .desktop.

  MEDORA_REPO   GitHub owner/name (default AlyShmahell/medora)
  MEDORA_HOME   Install prefix (default ~/.medora)
EOF
}

if [[ ! -t 1 || ! -c /dev/tty || ! -r /dev/tty ]]; then
  usage
  echo "error: a terminal is required (try running from a real TTY)" >&2
  exit 1
fi

if ! command -v curl >/dev/null 2>&1; then
  echo "error: curl is required" >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "error: python3 is required" >&2
  exit 1
fi

menu() {
  local opts=("$@")
  local n=${#opts[@]}
  local i=0
  local key rest j
  printf '\e[?25l'
  restore() { printf '\e[?25h\e[0m'; }
  trap restore EXIT
  draw() {
    for j in "${!opts[@]}"; do
      if (( j == i )); then
        printf '\e[7m> %s\e[0m\n' "${opts[j]}"
      else
        printf '  %s\n' "${opts[j]}"
      fi
    done
  }
  draw
  while true; do
    IFS= read -rsn1 key </dev/tty
    rest=""
    if [[ $key == $'\e' ]]; then
      IFS= read -rsn2 -t 0.1 rest </dev/tty || rest=""
      key+="$rest"
    fi
    case "$key" in
      $'\e[A')
        i=$(( (i - 1 + n) % n ))
        ;;
      $'\e[B')
        i=$(( (i + 1) % n ))
        ;;
      "")
        printf '\e[%dA' "$n"
        for j in "${!opts[@]}"; do
          printf '\e[2K\n'
        done
        printf '\e[%dA' "$n"
        restore
        trap - EXIT
        CHOICE=$i
        return 0
        ;;
      *)
        continue
        ;;
    esac
    printf '\e[%dA' "$n"
    draw
  done
}

json_tmp="$(mktemp)"
trap 'rm -f "$json_tmp"' EXIT
if ! curl -fsSL -A medora-install -H "Accept: application/vnd.github+json" \
  "https://api.github.com/repos/${REPO}/releases" >"$json_tmp"; then
  echo "error: could not list releases for ${REPO}" >&2
  exit 1
fi

mapfile -t TAGS < <(python3 -c '
import json, sys
rels = json.load(open(sys.argv[1]))
if not isinstance(rels, list) or not rels:
    sys.exit(2)
for r in rels:
    tag = (r.get("tag_name") or "").strip()
    if tag:
        print(tag)
' "$json_tmp") || {
  echo "error: no releases published for ${REPO}" >&2
  exit 1
}

if [[ ${#TAGS[@]} -eq 0 ]]; then
  echo "error: no releases published for ${REPO}" >&2
  exit 1
fi

echo "Select a Medora release (${REPO})"
CHOICE=
menu "${TAGS[@]}"
TAG="${TAGS[$CHOICE]}"

echo
echo "Select archive for ${TAG}"
CHOICE=
menu "bundled (ffmpeg + llama)" "slim (run --prepare later)"
case "$CHOICE" in
  0) KIND=bundled ;;
  *) KIND=slim ;;
esac

ASSET_URL="$(python3 -c '
import json, sys
tag, kind = sys.argv[2], sys.argv[3]
rels = json.load(open(sys.argv[1]))
rel = next((r for r in rels if (r.get("tag_name") or "") == tag), None)
if not rel:
    sys.exit(2)
assets = rel.get("assets") or []
want = []
for a in assets:
    name = a.get("name") or ""
    url = a.get("browser_download_url") or ""
    if kind == "bundled" and name.endswith("-linux-amd64-bundled.tar.gz"):
        want.append(url)
    if kind == "slim" and name.endswith("-linux-amd64.tar.gz") and "bundled" not in name:
        want.append(url)
if not want:
    sys.exit(3)
print(want[0])
' "$json_tmp" "$TAG" "$KIND")" || {
  echo "error: ${KIND} tarball not found on ${TAG}" >&2
  exit 1
}

work="$(mktemp -d)"
trap 'rm -rf "$work" "$json_tmp"' EXIT
tarball="$work/medora.tar.gz"
echo "Downloading ${ASSET_URL}"
curl -fL -A medora-install --retry 3 --retry-delay 1 -o "$tarball" "$ASSET_URL"
tar -xzf "$tarball" -C "$work"
src=""
if [[ -x "$work/medora/medora" ]]; then
  src="$work/medora"
elif [[ -x "$work/medora" && -d "$work/config" ]]; then
  src="$work"
else
  echo "error: unexpected archive layout" >&2
  exit 1
fi

mkdir -p "$DEST"
for item in medora config public tools vendor share LICENSE VERSION; do
  if [[ -e "$src/$item" ]]; then
    rm -rf "$DEST/$item"
    cp -a "$src/$item" "$DEST/"
  fi
done
chmod +x "$DEST/medora"

mkdir -p "$BIN_DIR" "$APP_DIR" "$ICON_DIR"
ln -sfn "$DEST/medora" "$BIN_DIR/medora"

if [[ -f "$DEST/public/logo.svg" ]]; then
  cp -a "$DEST/public/logo.svg" "$ICON_DIR/medora.svg"
fi

desktop_src="$DEST/share/applications/medora.desktop"
desktop_dst="$APP_DIR/medora.desktop"
if [[ ! -f "$desktop_src" ]]; then
  echo "error: desktop file missing from release" >&2
  exit 1
fi
sed "s|^Exec=.*|Exec=${DEST}/medora|" "$desktop_src" >"$desktop_dst"
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$APP_DIR" >/dev/null 2>&1 || true
fi

echo "Installed ${TAG} (${KIND}) to ${DEST}"
echo "  ${BIN_DIR}/medora"
echo "  ${desktop_dst}"
echo "Add ${BIN_DIR} to PATH if medora is not found."
