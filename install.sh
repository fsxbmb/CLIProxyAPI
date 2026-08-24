#!/bin/sh
set -eu

if ! command -v go >/dev/null 2>&1; then
  echo "Go 1.26 is required. Install it with: brew install go" >&2
  exit 1
fi

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$script_dir"
make install

echo "Installed cliproxy-lite to $HOME/bin/cliproxy-lite"
echo "Run: $HOME/bin/cliproxy-lite serve"
