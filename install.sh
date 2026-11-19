#!/usr/bin/env bash
# Installs the GNOME Shell extension for the current user.
#   ./install.sh [--host 127.0.0.1] [--port N] [--token SECRET] [--render auto|content|style]
# The port defaults to PET_PORT from .env (created here with a random free port
# on first use), so petd and the extension always agree.
set -euo pipefail

UUID="mnemo-pet@cs3306"
here="$(cd "$(dirname "$0")" && pwd)"
host=""; port=""; token=""; render=""
while [ $# -gt 0 ]; do
  case "$1" in
    --host) host="$2"; shift 2 ;;
    --port) port="$2"; shift 2 ;;
    --token) token="$2"; shift 2 ;;
    --render) render="$2"; shift 2 ;;
    -h|--help) sed -n '2,3p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done

data="${XDG_DATA_HOME:-$HOME/.local/share}"
conf="${XDG_CONFIG_HOME:-$HOME/.config}"
dest="$data/gnome-shell/extensions/$UUID"

rm -rf "$dest"
mkdir -p "$dest/skins"
cp -r "$here/extension/." "$dest/"
cp -r "$here"/skins/. "$dest/skins/"
mkdir -p "$data/mnemo-pet/skins" "$conf/mnemo-pet"   # custom skins go in the first one

# Ports live in .env (random free pair, generated on first use).
python3 "$here/tools/mkenv.py" >/dev/null
if [ -z "$port" ]; then
  port="$(sed -n 's/^PET_PORT=\([0-9]*\).*/\1/p' "$here/.env" | tail -n1)"
fi

cfg="$conf/mnemo-pet/extension.json"
# Always refresh host/port (they follow .env); keep an existing token/render
# unless given on the command line.
python3 - "$cfg" "${host:-}" "$port" "$token" "$render" <<'PY'
import json, sys
path, host, port, token, render = sys.argv[1:6]
try:
    cur = json.load(open(path))
except (OSError, ValueError):
    cur = {}
cur["host"] = host or cur.get("host", "127.0.0.1")
cur["port"] = int(port)
cur["render"] = render or cur.get("render", "auto")
if token or "token" not in cur:
    cur["token"] = token or cur.get("token", "")
json.dump(cur, open(path, "w"), indent=2)
open(path, "a").write("\n")
PY

echo "Installed extension to $dest"
echo "Config: $cfg  (port $port, from .env)"
if gnome-extensions enable "$UUID" 2>/dev/null; then
  echo "Enabled."
else
  echo "Could not enable yet (GNOME has not seen the new extension)."
fi
if [ "${XDG_SESSION_TYPE:-}" = "wayland" ]; then
  echo
  echo "You are on Wayland: log out and back in once so GNOME Shell discovers the extension,"
  echo "then run:  gnome-extensions enable $UUID"
else
  echo "On X11 you can restart the shell with Alt+F2, r, Enter, then: gnome-extensions enable $UUID"
fi
