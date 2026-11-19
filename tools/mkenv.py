#!/usr/bin/env python3
"""Create .env (if missing) with a random free port pair; fill in missing ports otherwise.

Ports are 5-digit, picked from 20000-32000 (below Linux's ephemeral range, so
they do not collide with outgoing connections), and checked to be free right now.
An existing .env is never rewritten except to append a missing PET_PORT /
PET_HTTP_PORT line. Prints the resulting ports.
"""
import os
import random
import re
import socket
import sys

ROOT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..")
ENV = os.path.join(ROOT, ".env")
EXAMPLE = os.path.join(ROOT, ".env.example")


def free(port):
    for fam, addr in ((socket.AF_INET, "0.0.0.0"), (socket.AF_INET6, "::")):
        try:
            with socket.socket(fam, socket.SOCK_STREAM) as s:
                s.bind((addr, port))
        except OSError as e:
            if fam == socket.AF_INET6 and e.errno in (97, 99):  # no IPv6 here: fine
                continue
            return False
    return True


def pick(exclude=()):
    for _ in range(200):
        p = random.randint(20000, 32000)
        if p not in exclude and free(p):
            return p
    sys.exit("mkenv: could not find a free port")


def main():
    text = open(ENV).read() if os.path.exists(ENV) else open(EXAMPLE).read()
    fresh = not os.path.exists(ENV)
    if fresh:
        text = re.sub(r"(?m)^PET_UID=.*$", f"PET_UID={os.getuid()}", text)
        text = re.sub(r"(?m)^PET_GID=.*$", f"PET_GID={os.getgid()}", text)
    have = dict(re.findall(r"(?m)^(PET_PORT|PET_HTTP_PORT)=(\d*)\s*$", text))
    used = set()
    for key in ("PET_PORT", "PET_HTTP_PORT"):
        if have.get(key):  # already set (even to the fallback): keep it
            used.add(int(have[key]))
    for key in ("PET_PORT", "PET_HTTP_PORT"):
        if have.get(key):
            continue
        p = pick(used)
        used.add(p)
        if re.search(rf"(?m)^{key}=", text):
            text = re.sub(rf"(?m)^{key}=.*$", f"{key}={p}", text)
        else:
            text = text.rstrip("\n") + f"\n{key}={p}\n"
    if fresh:
        # the example ships with fallback ports; replace them by random ones
        p1, p2 = pick(), None
        p2 = pick({p1})
        text = re.sub(r"(?m)^PET_PORT=.*$", f"PET_PORT={p1}", text)
        text = re.sub(r"(?m)^PET_HTTP_PORT=.*$", f"PET_HTTP_PORT={p2}", text)
    if fresh or text != open(ENV).read():
        with open(ENV, "w") as f:
            f.write(text)
        print(f"mkenv: wrote {os.path.normpath(ENV)}")
    m = dict(re.findall(r"(?m)^(PET_PORT|PET_HTTP_PORT)=(\d+)", text))
    print(f"mkenv: PET_PORT={m.get('PET_PORT')} PET_HTTP_PORT={m.get('PET_HTTP_PORT')}")


if __name__ == "__main__":
    main()
