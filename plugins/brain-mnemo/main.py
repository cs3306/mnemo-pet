#!/usr/bin/env python3
"""brain-mnemo: optional LLM brain.

Talks to mnemo or any OpenAI-compatible /chat/completions endpoint.

Environment:
  BRAIN_URL          base URL incl. /v1, e.g. http://mnemo:8080/v1   (unset = plugin disabled)
  BRAIN_KEY          bearer token (optional)
  BRAIN_MODEL        model name (default: "default")
  BRAIN_SEND_SYSTEM  "1" (default) sends a short system prompt with action tags;
                     "0" sends none (use this when the backend, e.g. mnemo, owns the persona)
  BRAIN_SYSTEM       override the system prompt text
  BRAIN_TIMEOUT      seconds (default 120)
  BRAIN_HISTORY      number of past messages to resend (default 12; 0 = stateless)

Replies may start with an action tag such as "[happy] ..."; the tag selects the
pet's animation and is stripped from the bubble.
"""
import json
import os
import re
import sys
import threading
import urllib.error
import urllib.request

sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "_sdk"))
from petplugin import Plugin, log  # noqa: E402

URL = os.environ.get("BRAIN_URL", "").rstrip("/")
if not URL:
    print("[brain-mnemo] BRAIN_URL not set - brain disabled (this is fine).", file=sys.stderr, flush=True)
    sys.exit(0)

KEY = os.environ.get("BRAIN_KEY", "")
MODEL = os.environ.get("BRAIN_MODEL", "default")
TIMEOUT = float(os.environ.get("BRAIN_TIMEOUT", "120"))
HISTORY = int(os.environ.get("BRAIN_HISTORY", "12"))
SEND_SYSTEM = os.environ.get("BRAIN_SEND_SYSTEM", "1") not in ("0", "false", "no")
TAGS = ("happy", "think", "wave", "apologize", "react", "sit", "sleep", "notify")
SYSTEM = os.environ.get("BRAIN_SYSTEM") or (
    "你是一只住在用户桌面上的小宠物。回复要简短（一两句话），语气可爱。"
    "可以在回复开头加一个动作标签来表达当前情绪，可选标签：" + " ".join(f"[{t}]" for t in TAGS) + "。"
)
TAG_RE = re.compile(r"^\s*\[(" + "|".join(TAGS) + r")\]\s*", re.I)

p = Plugin("brain-mnemo", subscribe=["user_text"], caps=["brain"])
history = []
lock = threading.Lock()
busy = threading.Lock()


def chat(text):
    with lock:
        msgs = []
        if SEND_SYSTEM:
            msgs.append({"role": "system", "content": SYSTEM})
        if HISTORY > 0:
            msgs += history[-HISTORY:]
        msgs.append({"role": "user", "content": text})
    body = json.dumps({"model": MODEL, "messages": msgs, "stream": False}).encode()
    req = urllib.request.Request(URL + "/chat/completions", data=body, method="POST",
                                 headers={"Content-Type": "application/json"})
    if KEY:
        req.add_header("Authorization", "Bearer " + KEY)
    with urllib.request.urlopen(req, timeout=TIMEOUT) as r:
        data = json.load(r)
    return data["choices"][0]["message"]["content"].strip()


def handle(text):
    if not busy.acquire(blocking=False):
        p.act(say="等一下，我还在想上一句~", anim="think", prio=55, hold=2000)
        return
    try:
        p.act(anim="think", hold=120000, prio=55)  # thinking until the reply arrives
        try:
            reply = chat(text)
        except (urllib.error.URLError, OSError, KeyError, ValueError, IndexError) as e:
            log(f"[brain-mnemo] request failed: {e!r}")
            p.act(say="（连不上大脑……）", anim="apologize", prio=55)
            return
        with lock:
            history.append({"role": "user", "content": text})
            history.append({"role": "assistant", "content": reply})
            del history[:-max(HISTORY, 2) * 2]
        m = TAG_RE.match(reply)
        anim = m.group(1).lower() if m else "idle"
        clean = TAG_RE.sub("", reply, count=1).strip() or "…"
        p.act(say=clean, anim=anim, prio=55, ttl=min(20000, max(6000, len(clean) * 150)))
    finally:
        busy.release()


@p.on("user_text")
def _user_text(data):
    text = (data.get("text") or "").strip()
    if text:
        threading.Thread(target=handle, args=(text,), daemon=True).start()


if __name__ == "__main__":
    log(f"[brain-mnemo] using {URL} model={MODEL} system_prompt={'on' if SEND_SYSTEM else 'off'}")
    p.run()
