#!/usr/bin/env bash
# Install a published ServeMedia release into XDG HOME (.local/bin + .local/share).
set -euo pipefail

REPO="${SERVEMEDIA_REPO:-AlyShmahell/servemedia}"
BIN_DIR="${XDG_BIN_HOME:-$HOME/.local/bin}"
DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
SHARE_DIR="$DATA_HOME/servemedia"
APP_DIR="$DATA_HOME/applications"
ICON_DIR="$DATA_HOME/icons/hicolor/scalable/apps"

usage() {
  cat <<EOF
Usage: curl -fsSL https://raw.githubusercontent.com/AlyShmahell/servemedia/main/install.sh | bash
  Interactive TTY menu: pick a GitHub release.
  Unpacks the XDG tarball into ~/.local (bin + share) and installs a userscope .desktop.

  SERVEMEDIA_REPO   GitHub owner/name (default AlyShmahell/servemedia)
  XDG_BIN_HOME      Binary dir (default ~/.local/bin)
  XDG_DATA_HOME     Share dir (default ~/.local/share)
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
if ! curl -fsSL -A servemedia-install -H "Accept: application/vnd.github+json" \
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

echo "Select a ServeMedia release (${REPO})"
CHOICE=
menu "${TAGS[@]}"
TAG="${TAGS[$CHOICE]}"

ASSET_URL="$(python3 -c '
import json, sys
tag = sys.argv[2]
rels = json.load(open(sys.argv[1]))
rel = next((r for r in rels if (r.get("tag_name") or "") == tag), None)
if not rel:
    sys.exit(2)
assets = rel.get("assets") or []
bundled, plain = [], []
for a in assets:
    name = a.get("name") or ""
    url = a.get("browser_download_url") or ""
    if name.endswith("-linux-amd64-bundled.tar.gz"):
        bundled.append(url)
    elif name.endswith("-linux-amd64.tar.gz") and "bundled" not in name:
        plain.append(url)
if bundled:
    print(bundled[0])
elif plain:
    print(plain[0])
else:
    sys.exit(3)
' "$json_tmp" "$TAG")" || {
  echo "error: linux-amd64 tarball not found on ${TAG}" >&2
  exit 1
}

work="$(mktemp -d)"
trap 'rm -rf "$work" "$json_tmp"' EXIT
tarball="$work/servemedia.tar.gz"
echo "Downloading ${ASSET_URL}"
curl -fL -A servemedia-install --retry 3 --retry-delay 1 -o "$tarball" "$ASSET_URL"
tar -xzf "$tarball" -C "$work"
src=""
if [[ -x "$work/servemedia/.local/bin/servemedia" ]]; then
  src="$work/servemedia"
elif [[ -x "$work/servemedia/servemedia" ]]; then
  src="$work/servemedia"
elif [[ -x "$work/servemedia" && -d "$work/config" ]]; then
  src="$work"
else
  echo "error: unexpected archive layout" >&2
  exit 1
fi

mkdir -p "$BIN_DIR" "$SHARE_DIR" "$APP_DIR" "$ICON_DIR"
MM_SHARE="$DATA_HOME/matchmedia"
if [[ -x "$src/.local/bin/servemedia" ]]; then
  cp -a "$src/.local/bin/servemedia" "$BIN_DIR/servemedia"
  if [[ -x "$src/.local/bin/matchmedia" ]]; then
    cp -a "$src/.local/bin/matchmedia" "$BIN_DIR/matchmedia"
  fi
  if [[ -d "$src/.local/share/servemedia" ]]; then
    cp -a "$src/.local/share/servemedia"/. "$SHARE_DIR/"
  fi
  if [[ -d "$src/.local/share/matchmedia" ]]; then
    mkdir -p "$MM_SHARE"
    cp -a "$src/.local/share/matchmedia"/. "$MM_SHARE/"
  fi
  if [[ -f "$src/.local/share/applications/servemedia.desktop" ]]; then
    cp -a "$src/.local/share/applications/servemedia.desktop" "$APP_DIR/servemedia.desktop"
  fi
else
  cp -a "$src/servemedia" "$BIN_DIR/servemedia"
  for item in config public tools vendor LICENSE; do
    if [[ -e "$src/$item" ]]; then
      rm -rf "$SHARE_DIR/$item"
      cp -a "$src/$item" "$SHARE_DIR/"
    fi
  done
  if [[ -x "$SHARE_DIR/tools/matchmedia/matchmedia" ]]; then
    cp -a "$SHARE_DIR/tools/matchmedia/matchmedia" "$BIN_DIR/matchmedia"
    mkdir -p "$MM_SHARE"
    for item in config public LICENSE; do
      if [[ -e "$SHARE_DIR/tools/matchmedia/$item" ]]; then
        cp -a "$SHARE_DIR/tools/matchmedia/$item" "$MM_SHARE/"
      fi
    done
  fi
  if [[ -f "$src/share/applications/servemedia.desktop" ]]; then
    cp -a "$src/share/applications/servemedia.desktop" "$APP_DIR/servemedia.desktop"
  fi
fi
if [[ -f "$src/LICENSE" && ! -f "$SHARE_DIR/LICENSE" ]]; then
  cp -a "$src/LICENSE" "$SHARE_DIR/LICENSE"
fi
chmod +x "$BIN_DIR/servemedia"
if [[ -x "$BIN_DIR/matchmedia" ]]; then
  chmod +x "$BIN_DIR/matchmedia"
fi

icon_src="$SHARE_DIR/public/logo.svg"
if [[ -f "$icon_src" ]]; then
  cp -a "$icon_src" "$ICON_DIR/servemedia.svg"
fi
if command -v gtk-update-icon-cache >/dev/null 2>&1; then
  gtk-update-icon-cache -f -t "$(dirname "$(dirname "$ICON_DIR")")" >/dev/null 2>&1 || true
fi

desktop_src="$APP_DIR/servemedia.desktop"
if [[ ! -f "$desktop_src" ]]; then
  echo "error: desktop file missing from release" >&2
  exit 1
fi
icon_path="$ICON_DIR/servemedia.svg"
if [[ ! -f "$icon_path" ]]; then
  icon_path="$icon_src"
fi
sed -e "s|^Exec=.*|Exec=${BIN_DIR}/servemedia|" -e "s|^Icon=.*|Icon=${icon_path}|" "$desktop_src" >"${desktop_src}.tmp"
mv "${desktop_src}.tmp" "$desktop_src"
if command -v update-desktop-database >/dev/null 2>&1; then
  update-desktop-database "$APP_DIR" >/dev/null 2>&1 || true
fi

echo "Installed ${TAG} to ${SHARE_DIR}"
echo "  ${BIN_DIR}/servemedia"
echo "  ${desktop_src}"
echo "Add ${BIN_DIR} to PATH if servemedia is not found."
