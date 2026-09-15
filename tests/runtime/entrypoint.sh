#!/bin/sh
set -e
mkdir -p /app/data/matchmedia
if [ ! -f /app/data/matchmedia/secrets ]; then
  printf 'omdb: test\n' > /app/data/matchmedia/secrets
fi
export SERVEMEDIA_NO_BROWSER=1
exec /app/servemedia "$@"
