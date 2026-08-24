#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
cd "$script_dir"
install_dir=${INSTALL_DIR:-"$HOME/.local/bin"}
mkdir -p "$install_dir"

target="$install_dir/cliproxy-lite"
bundled="$script_dir/cliproxy-lite"
source_binary="$script_dir/bin/cliproxy-lite"

if [ -x "$bundled" ]; then
  cp "$bundled" "$target"
elif [ -x "$source_binary" ]; then
  cp "$source_binary" "$target"
elif command -v go >/dev/null 2>&1; then
  version=${VERSION:-dev}
  commit=${COMMIT:-none}
  build_date=${BUILD_DATE:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}
  CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=$version -X main.commit=$commit -X main.buildDate=$build_date" \
    -o "$target" \
    ./cmd/cliproxy-lite
else
  echo "No bundled cliproxy-lite binary found and Go is not installed." >&2
  echo "Install Go from https://go.dev/dl/ or use a release archive containing the binary." >&2
  exit 1
fi

chmod 0755 "$target" 2>/dev/null || true

echo "Installed cliproxy-lite to $target"
echo "Initialize: $target init"
echo "Run:        $target serve"
echo "API:        http://127.0.0.1:8317/v1"
echo "Web UI:     http://127.0.0.1:8318/ui/"
