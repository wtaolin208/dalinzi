#!/bin/sh
set -eu
cd "$(dirname "$0")"
if [ "$#" -ne 1 ]; then
  echo 'usage: bash run.sh port' >&2
  exit 2
fi
if [ -n "${AGENT_CONFIG:-}" ]; then
  exec ./agent -debug="${AGENT_DEBUG:-true}" -config "$AGENT_CONFIG" "$1"
fi
exec ./agent -debug="${AGENT_DEBUG:-true}" -config configs/default.json "$1"
