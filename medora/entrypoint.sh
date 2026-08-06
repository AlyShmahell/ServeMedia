#!/bin/sh
set -eu
STORE=/data/store
SOCKET="${MEDORA_PROVIDERS_SOCKET:-/data/run/providers.sock}"
mkdir -p "$STORE/metadata/movies" "$STORE/metadata/tv" /data/transcode /data/backups /data/providers "$(dirname "$SOCKET")"
[ -f "$STORE/config.yaml" ] || cp /usr/share/medora/config.default.yaml "$STORE/config.yaml"

# Providers sidecar (metadata RPC). Same container; main talks over the Unix socket.
rm -f "$SOCKET"
/usr/local/bin/medora-providers &
# Wait briefly for the socket to appear.
i=0
while [ ! -S "$SOCKET" ] && [ "$i" -lt 20 ]; do
  i=$((i + 1))
  sleep 1
done
if [ ! -S "$SOCKET" ]; then
  echo "medora-providers failed to create $SOCKET" >&2
  exit 1
fi

# Run as container root: under rootless Podman this maps to the host user, who can write
# the /media bind mount. Dropping to UID 1000 would map to a subuid and break sidecar writes.
exec /usr/local/bin/medora-watchdog
