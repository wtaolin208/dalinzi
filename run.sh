#!/bin/sh
set -eu
cd "$(dirname "$0")"
if [ "$#" -ne 1 ]; then
  echo 'usage: bash run.sh port' >&2
  exit 2
fi
binary=./main
if [ ! -x "$binary" ]; then
  binary=./agent
fi
if [ ! -x "$binary" ]; then
  echo 'missing executable: run make (or go build -o main main.go) first' >&2
  exit 1
fi
exec "$binary" -debug="${AGENT_DEBUG:-true}" -config "${AGENT_CONFIG:-configs/default.json}" "$1"
