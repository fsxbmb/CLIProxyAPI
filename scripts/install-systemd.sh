#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
unit_dir="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
mkdir -p "$unit_dir"

install -m 0644 "$project_dir/packaging/systemd/cliproxy-lite.service" "$unit_dir/cliproxy-lite.service"
mkdir -p "$HOME/.local/bin"
if [ -x "$project_dir/cliproxy-lite" ]; then
  install -m 0755 "$project_dir/cliproxy-lite" "$HOME/.local/bin/cliproxy-lite"
elif [ -x "$project_dir/bin/cliproxy-lite" ]; then
  install -m 0755 "$project_dir/bin/cliproxy-lite" "$HOME/.local/bin/cliproxy-lite"
else
  echo "No cliproxy-lite binary found in package root or bin/" >&2
  exit 1
fi
systemctl --user daemon-reload
systemctl --user enable --now cliproxy-lite.service

echo "Installed and started systemd user service: cliproxy-lite.service"
