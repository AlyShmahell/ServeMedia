#!/bin/sh
set -eu
STORE=/data/store
mkdir -p "$STORE/metadata/movies" "$STORE/metadata/tv" /data/transcode /data/backups /data/plugins /data/run/plugins
[ -f "$STORE/config.yaml" ] || cp /usr/share/medora/config.default.yaml "$STORE/config.yaml"

# Run as container root: under rootless Podman this maps to the host user, who can write
# the /media bind mount. Dropping to UID 1000 would map to a subuid and break sidecar writes.
exec /usr/local/bin/medora-watchdog
