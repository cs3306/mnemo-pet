"""petplugin: tiny stdlib-only SDK for mnemo-pet plugins.

    from petplugin import Plugin
    p = Plugin("hello", subscribe=["click"])

    @p.on("click")
    def _(data):
        p.act(say="hi!", anim="wave")

    p.run()

The connection settings come from the environment that petd sets for managed
plugins (PET_HOST, PET_PORT, PET_TOKEN, PET_DATA_DIR). For an externally
started plugin, set them yourself. The wire protocol is documented in
docs/PROTOCOL.md; this module is just a convenience wrapper around it.
"""
import fnmatch
import json
import os
import socket
import sys
import threading
import time


def log(*a):
    print(*a, file=sys.stderr, flush=True)


class Plugin:
    def __init__(self, name, subscribe=(), caps=()):
        self.name = name
        self.subscribe = list(subscribe)
        self.caps = list(caps)
        self.host = os.environ.get("PET_HOST", "127.0.0.1")
        self.port = int(os.environ.get("PET_PORT", "23817"))
        self.token = os.environ.get("PET_TOKEN", "")
        self.data_dir = os.environ.get("PET_DATA_DIR", os.path.join(".", "data"))
        os.makedirs(self.data_dir, exist_ok=True)
        self._handlers = []  # (pattern, fn)
        self._sock = None
        self._lock = threading.Lock()
        self._stop = threading.Event()
        self._on_connect = []
        self._timers = []
        self._channels = {}  # id -> (label, group, enabled); re-sent on every reconnect

    # ---- registration ----------------------------------------------------
    def on(self, pattern):
        """Decorator: call fn(data) for events matching pattern (glob, e.g. 'time*')."""
        def deco(fn):
            self._handlers.append((pattern, fn))
            if pattern not in self.subscribe:
                self.subscribe.append(pattern)
            return fn
        return deco

    def on_connect(self, fn):
        self._on_connect.append(fn)
        return fn

    def every(self, seconds, fn):
        """Run fn() every `seconds` in a background thread (while running)."""
        self._timers.append((seconds, fn))

    # ---- sending ---------------------------------------------------------
    def _send(self, obj):
        line = (json.dumps(obj, ensure_ascii=False) + "\n").encode()
        with self._lock:
            if self._sock is None:
                return False
            try:
                self._sock.sendall(line)
                return True
            except OSError:
                return False

    def act(self, **kw):
        """act(say=..., anim=..., sound=..., move={...}, prio=..., hold=..., ttl=..., loop=..., channel=...)"""
        return self._send({"t": "act", **kw})

    def emit(self, name, data=None):
        """Emit a custom event; petd namespaces it as plugin.<name>.<event>."""
        return self._send({"t": "event", "name": name, "data": data or {}})

    def menu_add(self, id, label):
        return self._send({"t": "menu.add", "id": id, "label": label})

    def menu_remove(self, id):
        return self._send({"t": "menu.remove", "id": id})

    def channel(self, id, label, group="", enabled=True):
        """Declare a speech channel the user can switch on/off in the right-click
        menu. Pass channel=id to act(); acts on a disabled channel are dropped.
        `enabled` is only the default; the user's choice is remembered by petd."""
        self._channels[id] = (label, group, enabled)
        return self._send({"t": "channel.add", "id": id, "label": label,
                           "group": group, "enabled": enabled})

    def channel_remove(self, id):
        self._channels.pop(id, None)
        return self._send({"t": "channel.remove", "id": id})

    # ---- running ---------------------------------------------------------
    def stop(self):
        self._stop.set()
        with self._lock:
            if self._sock:
                try:
                    self._sock.shutdown(socket.SHUT_RDWR)
                except OSError:
                    pass

    def _connect(self):
        s = socket.create_connection((self.host, self.port), timeout=5)
        s.settimeout(None)
        hello = {"t": "hello", "role": "plugin", "name": self.name,
                 "subscribe": self.subscribe, "caps": self.caps}
        if self.token:
            hello["token"] = self.token
        s.sendall((json.dumps(hello) + "\n").encode())
        with self._lock:
            self._sock = s
        return s

    def _dispatch(self, msg):
        if msg.get("t") == "error":
            log(f"[{self.name}] server error: {msg.get('msg')}")
            return
        if msg.get("t") != "event":
            return
        name, data = msg.get("name", ""), msg.get("data") or {}
        for pat, fn in self._handlers:
            if fnmatch.fnmatchcase(name, pat) or pat == "*":
                try:
                    fn(data) if _arity(fn) else fn()
                except Exception as e:  # a bad handler must not kill the plugin
                    log(f"[{self.name}] handler error for {name}: {e!r}")

    def run(self):
        for seconds, fn in self._timers:
            threading.Thread(target=self._timer_loop, args=(seconds, fn), daemon=True).start()
        backoff = 1
        while not self._stop.is_set():
            try:
                s = self._connect()
            except OSError as e:
                log(f"[{self.name}] cannot connect to {self.host}:{self.port}: {e}; retry in {backoff}s")
                if self._stop.wait(backoff):
                    break
                backoff = min(backoff * 2, 15)
                continue
            backoff = 1
            for cid, (label, group, enabled) in list(self._channels.items()):
                self._send({"t": "channel.add", "id": cid, "label": label,
                            "group": group, "enabled": enabled})
            for fn in self._on_connect:
                try:
                    fn()
                except Exception as e:
                    log(f"[{self.name}] on_connect error: {e!r}")
            f = s.makefile("r", encoding="utf-8", newline="\n")
            try:
                for line in f:
                    line = line.strip()
                    if line:
                        try:
                            self._dispatch(json.loads(line))
                        except json.JSONDecodeError:
                            pass
            except OSError:
                pass
            with self._lock:
                self._sock = None
            if not self._stop.is_set():
                log(f"[{self.name}] disconnected; reconnecting")
                self._stop.wait(1)

    def _timer_loop(self, seconds, fn):
        while not self._stop.wait(seconds):
            try:
                fn()
            except Exception as e:
                log(f"[{self.name}] timer error: {e!r}")

    # ---- small helpers ----------------------------------------------------
    def load_json(self, filename, default):
        """Read <data_dir>/<filename>; create it from `default` on first use."""
        path = os.path.join(self.data_dir, filename)
        if not os.path.exists(path):
            with open(path, "w", encoding="utf-8") as f:
                json.dump(default, f, ensure_ascii=False, indent=2)
                f.write("\n")
            return json.loads(json.dumps(default))
        with open(path, encoding="utf-8") as f:
            return json.load(f)


def _arity(fn):
    try:
        return fn.__code__.co_argcount > 0
    except AttributeError:
        return True
