#!/usr/bin/env python3
"""fake_render: a stand-in for the GNOME Shell extension, for development.

Connects to petd as the renderer, prints everything petd tells the pet to do,
simulates animation timing (anim_end) and walking (arrived), and lets you type
events on stdin:

    click | dblclick | drag_start | drag_end | idle [sec] | active [sec]
    focus <app> | notify <app> | text <message> | menu <id> | switch <id> <on|off> | event <name> [json] | quit

Usage: python3 tools/fake_render.py [--host 127.0.0.1] [--port $PET_PORT] [--token T]
                                    [--skin skins/default] [--script "click;text hi"]
"""
import argparse
import json
import os
import socket
import sys
import threading
import time


def default_port():
    """PET_PORT from the environment, else from the repo's .env, else the fallback."""
    if os.environ.get("PET_PORT"):
        return int(os.environ["PET_PORT"])
    try:
        for line in open(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".env")):
            if line.startswith("PET_PORT=") and line.strip().split("=", 1)[1].isdigit():
                return int(line.strip().split("=", 1)[1])
    except OSError:
        pass
    return 23817


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--host", default="127.0.0.1")
    ap.add_argument("--port", type=int, default=default_port())
    ap.add_argument("--token", default=os.environ.get("PET_TOKEN", ""))
    ap.add_argument("--skin", default=os.path.join(os.path.dirname(__file__), "..", "skins", "default"))
    ap.add_argument("--script", default="", help="semicolon-separated commands to run, then exit after --wait seconds")
    ap.add_argument("--wait", type=float, default=2.0)
    a = ap.parse_args()

    manifest = {}
    try:
        with open(os.path.join(a.skin, "pet.json"), encoding="utf-8") as f:
            manifest = json.load(f).get("anims", {})
    except OSError:
        pass

    s = socket.create_connection((a.host, a.port), timeout=5)
    s.settimeout(None)
    lock = threading.Lock()

    def send(obj):
        with lock:
            s.sendall((json.dumps(obj, ensure_ascii=False) + "\n").encode())

    hello = {"t": "hello", "role": "render"}
    if a.token:
        hello["token"] = a.token
    send(hello)
    cur = {"anim": None}

    def later(delay, fn):
        t = threading.Timer(delay, fn)
        t.daemon = True
        t.start()

    def reader():
        for line in s.makefile("r", encoding="utf-8"):
            m = json.loads(line)
            print("<-", json.dumps(m, ensure_ascii=False), flush=True)
            t = m.get("t")
            if t == "anim":
                name = m["name"]
                cur["anim"] = name
                if not m.get("loop"):
                    spec = manifest.get(name, {})
                    dur = max(1, len(spec.get("frames", [1, 1, 1]))) / max(1.0, spec.get("fps", 6))
                    later(dur, lambda n=name: cur["anim"] == n and send({"t": "event", "name": "anim_end", "data": {"name": n}}))
            elif t == "move":
                later(1.0, lambda: send({"t": "event", "name": "arrived", "data": {"x": 500}}))
            elif t == "error":
                print("server error:", m.get("msg"), file=sys.stderr)
                os._exit(2)

    threading.Thread(target=reader, daemon=True).start()

    def run(cmd):
        parts = cmd.strip().split(None, 1)
        if not parts:
            return True
        c, arg = parts[0], parts[1] if len(parts) > 1 else ""
        ev = lambda n, d=None: send({"t": "event", "name": n, "data": d or {}})
        if c == "click": ev("click", {"x": 10, "y": 10})
        elif c == "dblclick": ev("double_click")
        elif c == "drag_start": ev("drag_start")
        elif c == "drag_end": ev("drag_end", {"x": 300, "y": 600})
        elif c == "idle": ev("idle", {"sec": int(arg or 700)})
        elif c == "active": ev("active", {"idle_sec": int(arg or 700)})
        elif c == "focus": ev("focus", {"app": arg or "terminal"})
        elif c == "notify": ev("notify", {"app": arg or "Telegram"})
        elif c == "text": ev("user_text", {"text": arg})
        elif c == "menu": ev("menu.click", {"id": arg})
        elif c == "switch":  # toggle a menu switch, e.g. "switch channel:report.cpu on"
            sid, _, state = arg.partition(" ")
            ev("menu.click", {"id": sid, "checked": state.strip() != "off"})
        elif c == "event":
            n, _, d = arg.partition(" ")
            ev(n, json.loads(d) if d else {})
        elif c == "quit": return False
        else: print("unknown command:", c)
        return True

    if a.script:
        for cmd in a.script.split(";"):
            run(cmd)
            time.sleep(0.4)
        time.sleep(a.wait)
        return
    for line in sys.stdin:
        if not run(line):
            break


if __name__ == "__main__":
    main()
