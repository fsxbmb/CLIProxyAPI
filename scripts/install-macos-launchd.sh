#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
project_dir=$(CDPATH= cd -- "$script_dir/.." && pwd)
install_dir=${INSTALL_DIR:-"$HOME/.local/bin"}
label=com.fsxbmb.cliproxyapi-lite
plist="$HOME/Library/LaunchAgents/$label.plist"
log_dir="$HOME/Library/Logs/CLIProxyAPI-Lite"

mkdir -p "$install_dir" "$HOME/Library/LaunchAgents" "$log_dir"
if [ -x "$project_dir/cliproxy-lite" ]; then
  cp "$project_dir/cliproxy-lite" "$install_dir/cliproxy-lite"
elif [ -x "$project_dir/bin/cliproxy-lite" ]; then
  cp "$project_dir/bin/cliproxy-lite" "$install_dir/cliproxy-lite"
else
  INSTALL_DIR="$install_dir" "$project_dir/install.sh"
fi

cat > "$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>Label</key><string>$label</string>
  <key>ProgramArguments</key><array>
    <string>$install_dir/cliproxy-lite</string><string>serve</string><string>--no-open</string>
  </array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>ProcessType</key><string>Background</string>
  <key>StandardOutPath</key><string>$log_dir/service.log</string>
  <key>StandardErrorPath</key><string>$log_dir/service-error.log</string>
</dict></plist>
EOF

launchctl bootout "gui/$(id -u)/$label" 2>/dev/null || true
launchctl bootstrap "gui/$(id -u)" "$plist"
launchctl kickstart -k "gui/$(id -u)/$label"
echo "Installed and started launchd agent: $label"
