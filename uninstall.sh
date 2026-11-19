#!/usr/bin/env bash
# Removes the GNOME Shell extension (keeps ~/.config/mnemo-pet and your skins).
set -euo pipefail
UUID="mnemo-pet@cs3306"
gnome-extensions disable "$UUID" 2>/dev/null || true
rm -rf "${XDG_DATA_HOME:-$HOME/.local/share}/gnome-shell/extensions/$UUID"
echo "Removed $UUID"
