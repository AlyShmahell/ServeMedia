#!/bin/sh
set -e
mkdir -p /app/data/matchora
if [ ! -f /app/data/matchora/secrets ]; then
  printf 'omdb: test\n' > /app/data/matchora/secrets
fi
export MEDORA_NO_BROWSER=1
exec /app/medora "$@"
