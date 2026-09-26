#!/bin/sh
set -e
export HOME="${HOME:-/xdg}"
export XDG_DATA_HOME="${XDG_DATA_HOME:-$HOME/.local/share}"
export XDG_CACHE_HOME="${XDG_CACHE_HOME:-$HOME/.cache}"
export XDG_STATE_HOME="${XDG_STATE_HOME:-$HOME/.local/state}"
export XDG_CONFIG_HOME="${XDG_CONFIG_HOME:-$HOME/.config}"
export SERVEMEDIA_ROOT="${SERVEMEDIA_ROOT:-/app/.local/share/servemedia}"
mkdir -p "$XDG_DATA_HOME/matchmedia" "$XDG_CACHE_HOME" "$XDG_STATE_HOME" "$XDG_CONFIG_HOME"
if [ ! -f "$XDG_DATA_HOME/matchmedia/secrets" ]; then
  printf 'omdb: test\n' > "$XDG_DATA_HOME/matchmedia/secrets"
fi
export SERVEMEDIA_NO_BROWSER=1
exec /app/.local/bin/servemedia "$@"
