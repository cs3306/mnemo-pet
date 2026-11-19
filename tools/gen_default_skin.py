#!/usr/bin/env python3
"""Procedurally draws the bundled skins ("mochi" in several outfits) with Pillow.

Run:  python3 tools/gen_default_skin.py
Output: skins/<theme>/*.png + pet.json (128x128 RGBA frames, facing right) for
default, night, christmas and halloween. Everything is drawn at 4x and
downsampled for clean edges. Seasonal skins carry their own "when" schedule in
pet.json, so dropping the folder in is all it takes for them to wear themselves.
"""
import json
import math
import os

from PIL import Image, ImageDraw

W = H = 128
S = 4  # supersampling factor
SKINS = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "skins")
OUT = os.path.join(SKINS, "default")

BODY = (255, 228, 196, 255)
BODY_SHADE = (247, 205, 168, 255)
LINE = (107, 74, 58, 255)
PINK = (255, 170, 170, 255)
EYE = (60, 40, 40, 255)
WHITE = (255, 255, 255, 255)
YELLOW = (255, 214, 90, 255)
BLUE = (120, 190, 255, 255)


def s(v):
    return int(round(v * S))


def ell(d, cx, cy, rx, ry, fill, outline=LINE, width=1.6):
    d.ellipse([s(cx - rx), s(cy - ry), s(cx + rx), s(cy + ry)], fill=fill, outline=outline, width=s(width))


def poly(d, pts, fill, outline=LINE, width=1.6):
    P = [(s(x), s(y)) for x, y in pts]
    d.polygon(P, fill=fill)
    d.line(P + [P[0]], fill=outline, width=s(width), joint="curve")


def arc(d, cx, cy, rx, ry, a0, a1, width=1.8, color=EYE):
    d.arc([s(cx - rx), s(cy - ry), s(cx + rx), s(cy + ry)], a0, a1, fill=color, width=s(width))


def line(d, pts, width=1.8, color=EYE):
    d.line([(s(x), s(y)) for x, y in pts], fill=color, width=s(width), joint="curve")


def eyes(d, cx, cy, spread, kind="open", look=(0, 0), r=4.2):
    for sign in (-1, 1):
        ex = cx + sign * spread + look[0]
        ey = cy + look[1]
        if kind == "open":
            ell(d, ex, ey, r, r * 1.15, EYE, outline=EYE, width=0.1)
            ell(d, ex + 1.3, ey - 1.5, 1.4, 1.4, WHITE, outline=WHITE, width=0.1)
        elif kind == "wide":
            ell(d, ex, ey, r * 1.3, r * 1.45, WHITE, outline=EYE, width=1.2)
            ell(d, ex + look[0] * 0.3, ey, r * 0.55, r * 0.7, EYE, outline=EYE, width=0.1)
        elif kind == "half":
            ell(d, ex, ey + 1, r, r * 0.55, EYE, outline=EYE, width=0.1)
        elif kind == "closed":
            line(d, [(ex - r, ey + 0.5), (ex, ey + 2.2), (ex + r, ey + 0.5)], width=1.8)
        elif kind == "happy":
            arc(d, ex, ey + 2, r, r, 200, 340, width=2.0)
        elif kind == "sad":
            ell(d, ex, ey + 1, r * 0.9, r, EYE, outline=EYE, width=0.1)
            ell(d, ex + 1.2, ey - 0.4, 1.2, 1.2, WHITE, outline=WHITE, width=0.1)
        elif kind == "squint":  # > <
            if sign < 0:
                line(d, [(ex - r, ey - r * 0.8), (ex + r * 0.7, ey), (ex - r, ey + r * 0.8)], width=1.9)
            else:
                line(d, [(ex + r, ey - r * 0.8), (ex - r * 0.7, ey), (ex + r, ey + r * 0.8)], width=1.9)


def mouth(d, cx, cy, kind="smile"):
    if kind == "smile":
        arc(d, cx - 2.4, cy, 2.6, 2.2, 10, 170, width=1.5)
        arc(d, cx + 2.4, cy, 2.6, 2.2, 10, 170, width=1.5)
    elif kind == "open":
        ell(d, cx, cy + 1, 3.2, 3.6, (200, 90, 90, 255), outline=EYE, width=1.2)
    elif kind == "flat":
        line(d, [(cx - 3, cy + 1), (cx + 3, cy + 1)], width=1.5)
    elif kind == "wavy":
        line(d, [(cx - 4, cy + 1), (cx - 2, cy - 0.5), (cx, cy + 1), (cx + 2, cy - 0.5), (cx + 4, cy + 1)], width=1.4)
    elif kind == "tiny":
        ell(d, cx, cy + 0.5, 1.2, 1.0, EYE, outline=EYE, width=0.1)


def _pet_base(d, cx=64, by=82, bw=44, bh=40, eye="open", look=(0, 0), mth="smile", paw=None,
        feet=(0, 0), tail=0.0, ears=(0, 0), blush=True, tilt=0.0):
    """Draw the pet. (cx, by) is the body centre; feet=(left_dy, right_dy) lift in px."""
    top = by - bh / 2
    bot = by + bh / 2
    # tail
    tx, ty = cx + bw / 2 - 2, by + bh * 0.1
    pts = [(tx, ty)]
    for i in range(1, 8):
        a = i / 7
        pts.append((tx + 10 * math.sin(a * 2.4 + tail) * 0.8 + 9 * a, ty - 14 * a + 2 * math.sin(tail * 2 + a * 5)))
    d.line([(s(x), s(y)) for x, y in pts], fill=LINE, width=s(6.4), joint="curve")
    d.line([(s(x), s(y)) for x, y in pts], fill=BODY_SHADE, width=s(4.2), joint="curve")
    # feet
    for sign, dy in ((-1, feet[0]), (1, feet[1])):
        ell(d, cx + sign * bw * 0.24, bot - 1.5 - dy, 7.0, 4.6, BODY_SHADE)
    # ears
    for sign, dy in ((-1, ears[0]), (1, ears[1])):
        ex = cx + sign * bw * 0.30 + tilt
        poly(d, [(ex - 8 * 1, top + 7), (ex + sign * 3.5, top - 11 + dy), (ex + 9, top + 5)] if sign > 0 else
             [(ex - 9, top + 5), (ex + sign * 3.5, top - 11 + dy), (ex + 8, top + 7)], BODY)
        poly(d, [(ex - 4.2 + (3 if sign > 0 else 0), top + 5), (ex + sign * 2.6, top - 5 + dy * 0.7),
                 (ex + 4.2 + (0 if sign > 0 else -3), top + 4)], PINK, outline=PINK, width=0.1)
    # body
    ell(d, cx + tilt * 0.5, by, bw / 2, bh / 2, BODY)
    ell(d, cx + tilt * 0.5, by + bh * 0.22, bw * 0.34, bh * 0.2, (255, 241, 222, 255), outline=(255, 241, 222, 255), width=0.1)
    ey = by - bh * 0.06
    eyes(d, cx + tilt, ey, bw * 0.22, eye, look)
    if blush:
        for sign in (-1, 1):
            ell(d, cx + tilt + sign * bw * 0.34, ey + 6.2, 4.0, 2.4, PINK, outline=PINK, width=0.1)
    mouth(d, cx + tilt, ey + 6.5, mth)
    # paw
    if paw:
        side, ang = paw  # side: +1 right, -1 left; ang: degrees raised from hanging
        px = cx + side * (bw / 2 - 1)
        py = by + 2
        rad = math.radians(ang)
        ex = px + side * 10 * math.sin(rad)
        ey2 = py - 10 * math.cos(rad) + 2
        d.line([(s(px), s(py)), (s(ex), s(ey2))], fill=LINE, width=s(8.2), joint="curve")
        d.line([(s(px), s(py)), (s(ex), s(ey2))], fill=BODY, width=s(6.0), joint="curve")
        ell(d, ex, ey2, 4.0, 4.0, BODY)


ACCESSORY = None  # set per theme: fn(d, cx, top, bw, tilt)


def pet(d, cx=64, by=82, bw=44, bh=40, tilt=0.0, **kw):
    _pet_base(d, cx=cx, by=by, bw=bw, bh=bh, tilt=tilt, **kw)
    if ACCESSORY:
        ACCESSORY(d, cx + tilt * 0.5, by - bh / 2, bw, tilt)


def santa_hat(d, cx, top, bw, tilt):
    red, white = (214, 48, 49, 255), (255, 255, 255, 255)
    poly(d, [(cx - bw * 0.36, top + 5), (cx + bw * 0.36, top + 5), (cx + bw * 0.30 + 10, top - 12),
             (cx + bw * 0.12 + 16, top - 20), (cx - 6, top - 14)], red)
    ell(d, cx + bw * 0.12 + 16, top - 20, 4.2, 4.2, white)
    ell(d, cx, top + 4.5, bw * 0.40, 4.4, white)


def witch_hat(d, cx, top, bw, tilt):
    purple, band = (88, 52, 140, 255), (255, 150, 40, 255)
    ell(d, cx, top + 4, bw * 0.55, 4.6, purple)
    poly(d, [(cx - bw * 0.30, top + 3), (cx + bw * 0.30, top + 3), (cx + 6, top - 20), (cx + 12, top - 30), (cx - 8, top - 18)], purple)
    poly(d, [(cx - bw * 0.30, top + 3), (cx + bw * 0.30, top + 3), (cx + bw * 0.27, top - 3), (cx - bw * 0.27, top - 3)], band, outline=band, width=0.1)


def night_cap(d, cx, top, bw, tilt):
    blue, white = (70, 90, 170, 255), (255, 255, 255, 255)
    poly(d, [(cx - bw * 0.36, top + 5), (cx + bw * 0.36, top + 5), (cx + bw * 0.28, top - 8),
             (cx + bw * 0.45, top - 6), (cx + bw * 0.55, top + 10), (cx - 6, top - 14)], blue)
    ell(d, cx + bw * 0.55, top + 11, 4, 4, white)
    ell(d, cx, top + 4.5, bw * 0.40, 4.2, (230, 236, 255, 255))
    ell(d, cx - 4, top - 3, 1.6, 1.6, YELLOW, outline=YELLOW, width=0.1)


# name -> (palette overrides, accessory, label, when, priority, bubble_anchor_y)
THEMES = {
    "default": ({}, None, "麻糬", None, None, 0.27),
    "night": ({"BODY": (218, 214, 246, 255), "BODY_SHADE": (186, 180, 228, 255), "PINK": (240, 170, 200, 255)},
              night_cap, "夜晚", {"from": "22:00", "to": "07:00"}, 10, 0.2),
    "christmas": ({}, santa_hat, "圣诞", {"holidays": ["christmas"]}, 30, 0.2),
    "halloween": ({"BODY": (255, 214, 170, 255), "BODY_SHADE": (240, 178, 120, 255)},
                  witch_hat, "万圣节", {"holidays": ["halloween"]}, 30, 0.14),
}


def bubble_icon(d, x, y, kind):
    if kind == "!":
        poly(d, [(x - 3.4, y - 10), (x + 3.4, y - 10), (x + 1.6, y + 2), (x - 1.6, y + 2)], (255, 90, 90, 255), outline=LINE, width=1.0)
        ell(d, x, y + 6.2, 2.1, 2.1, (255, 90, 90, 255), outline=LINE, width=1.0)
    elif kind == "?":
        arc(d, x, y - 4, 4.4, 4.4, 180, 40, width=2.2, color=LINE)
        line(d, [(x + 3.6, y - 1.5), (x, y + 2.5), (x, y + 4)], width=2.2, color=LINE)
        ell(d, x, y + 8, 1.5, 1.5, LINE, outline=LINE, width=0.1)
    elif kind == "z":
        line(d, [(x - 4, y - 4), (x + 4, y - 4), (x - 4, y + 4), (x + 4, y + 4)], width=2.0, color=(90, 130, 200, 255))
    elif kind == "sweat":
        poly(d, [(x, y - 7), (x + 4.5, y + 1), (x, y + 5), (x - 4.5, y + 1)], BLUE, outline=(70, 130, 200, 255), width=1.0)
    elif kind == "star":
        pts = []
        for i in range(10):
            r = 5.2 if i % 2 == 0 else 2.2
            a = math.radians(-90 + i * 36)
            pts.append((x + r * math.cos(a), y + r * math.sin(a)))
        poly(d, pts, YELLOW, outline=(220, 160, 40, 255), width=0.9)
    elif kind == "mail":
        d.rectangle([s(x - 7), s(y - 5), s(x + 7), s(y + 5)], fill=WHITE, outline=LINE, width=s(1.2))
        line(d, [(x - 7, y - 5), (x, y + 1), (x + 7, y - 5)], width=1.2, color=LINE)


def frame(draw_fn):
    img = Image.new("RGBA", (W * S, H * S), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    # soft ground shadow
    d.ellipse([s(34), s(100), s(94), s(108)], fill=(0, 0, 0, 38))
    draw_fn(d)
    return img.resize((W, H), Image.LANCZOS)


ANIMS = {}


def add(name, fps, loop, fns):
    ANIMS[name] = (fps, loop, fns)


# idle: gentle breathing, 4 frames
add("idle", 3, True, [
    lambda d, k=k: pet(d, by=82 - k * 0.8, bh=40 + k * 1.6, bw=44 - k * 0.6, tail=k * 0.4)
    for k in (0, 1, 2, 1)
])
# blink
add("blink", 10, False, [
    lambda d: pet(d, eye="open"),
    lambda d: pet(d, eye="half"),
    lambda d: pet(d, eye="closed"),
    lambda d: pet(d, eye="half"),
    lambda d: pet(d, eye="open"),
])
# walk (facing right)
walk = []
for i in range(6):
    ph = i / 6 * 2 * math.pi
    walk.append(lambda d, ph=ph: pet(d, by=82 - abs(math.sin(ph)) * 3.2, look=(2, 0),
                                     feet=(max(0, math.sin(ph)) * 5, max(0, -math.sin(ph)) * 5),
                                     tail=ph, tilt=1.2))
add("walk", 10, True, walk)
# sit
add("sit", 2, True, [
    lambda d, k=k: pet(d, by=86, bh=36, bw=46, feet=(0, 0), tail=k, mth="smile", eye="open", look=(1, 0))
    for k in (0, 1.6)
])
# sleep
add("sleep", 2, True, [
    lambda d, k=k: (pet(d, by=88, bh=30, bw=50, eye="closed", mth="tiny", blush=True, ears=(2, 2)),
                    bubble_icon(d, 92 + k * 3, 52 - k * 6, "z"))
    for k in (0, 1, 2)
])
# wave
add("wave", 6, False, [
    lambda d, a=a: pet(d, eye="happy", mth="open", paw=(1, a), look=(1, 0))
    for a in (110, 150, 110, 150, 110, 150, 90)
])
# happy: little jumps with sparkles
jump = []
for i, (dy, sp) in enumerate(((0, 0), (-6, 0), (-13, 1), (-16, 1), (-13, 1), (-6, 0), (0, 0), (-8, 1), (0, 0))):
    jump.append(lambda d, dy=dy, sp=sp, i=i: (pet(d, by=82 + dy, bh=40 - (3 if dy == 0 and i else 0), eye="happy", mth="open",
                                                   feet=(-dy * 0.3, -dy * 0.3), ears=(-2, -2)),
                                              (bubble_icon(d, 94, 56 + dy * 0.5, "star"), bubble_icon(d, 36, 52 + dy * 0.5, "star")) if sp else None))
add("happy", 10, False, jump)
# think
add("think", 3, True, [
    lambda d, k=k: (pet(d, eye="open", look=(3, -3), mth="flat", tilt=-1.5),
                    ell(d, 86, 44 + k, 1.6, 1.6, WHITE), ell(d, 92, 36 + k, 2.4, 2.4, WHITE),
                    bubble_icon(d, 100, 22 + k, "?"))
    for k in (0, 1)
])
# apologize: bow + sweat
add("apologize", 5, False, [
    lambda d: pet(d, by=82, eye="sad", mth="wavy", ears=(3, 3)),
    lambda d: (pet(d, by=86, bh=36, bw=46, eye="squint", mth="wavy", ears=(5, 5), tilt=-2), bubble_icon(d, 90, 60, "sweat")),
    lambda d: (pet(d, by=90, bh=32, bw=48, eye="squint", mth="wavy", ears=(6, 6), tilt=-3), bubble_icon(d, 90, 64, "sweat")),
    lambda d: (pet(d, by=90, bh=32, bw=48, eye="squint", mth="wavy", ears=(6, 6), tilt=-3), bubble_icon(d, 91, 68, "sweat")),
    lambda d: (pet(d, by=86, bh=36, bw=46, eye="sad", mth="wavy", ears=(4, 4)), bubble_icon(d, 90, 60, "sweat")),
    lambda d: pet(d, by=82, eye="sad", mth="wavy", ears=(3, 3)),
])
# drag: held by the scruff
add("drag", 4, True, [
    lambda d, k=k: pet(d, by=70 + k, bh=48, bw=40, eye="wide", mth="open", feet=(-5 - k, -5 + k), tail=k, ears=(2, 2))
    for k in (0, 2)
])
# react: startled hop + "!"
add("react", 9, False, [
    lambda d: pet(d, eye="open", mth="tiny"),
    lambda d: (pet(d, by=76, bh=44, bw=42, eye="wide", mth="open", feet=(-4, -4), ears=(-3, -3)), bubble_icon(d, 96, 40, "!")),
    lambda d: (pet(d, by=72, bh=46, bw=40, eye="wide", mth="open", feet=(-6, -6), ears=(-4, -4)), bubble_icon(d, 96, 38, "!")),
    lambda d: (pet(d, by=78, bh=42, bw=43, eye="wide", mth="open", feet=(-2, -2)), bubble_icon(d, 96, 40, "!")),
    lambda d: pet(d, eye="open", mth="smile"),
])
# notify: mail bounces above head
add("notify", 6, False, [
    lambda d, k=k: (pet(d, eye="open", look=(0, -2), mth="smile", ears=(-3, -3)), bubble_icon(d, 64, 22 + k, "mail"))
    for k in (0, -3, 0, -3, 0, -2, 0)
])


def main():
    global ACCESSORY, BODY, BODY_SHADE, PINK
    base = (BODY, BODY_SHADE, PINK)
    total = 0
    for theme, (pal, acc, label, when, prio, anchor_y) in THEMES.items():
        BODY, BODY_SHADE, PINK = base
        for k, v in pal.items():
            globals()[k] = v
        ACCESSORY = acc
        out = os.path.join(SKINS, theme)
        os.makedirs(out, exist_ok=True)
        for f in os.listdir(out):
            if f.endswith(".png"):
                os.remove(os.path.join(out, f))
        manifest = {"name": theme, "label": label, "size": [W, H], "bubble_anchor": [0.5, anchor_y]}
        if when:
            manifest["when"] = when
            manifest["priority"] = prio
        manifest["anims"] = {}
        for name, (fps, loop, fns) in ANIMS.items():
            files = []
            for i, fn in enumerate(fns):
                fname = f"{name}_{i}.png"
                frame(fn).save(os.path.join(out, fname), optimize=True)
                files.append(fname)
            manifest["anims"][name] = {"frames": files, "fps": fps, "loop": loop}
            total += len(fns)
        with open(os.path.join(out, "pet.json"), "w", encoding="utf-8") as f:
            json.dump(manifest, f, ensure_ascii=False, indent=2)
            f.write("\n")
    BODY, BODY_SHADE, PINK = base
    print(f"wrote {total} frames, {len(ANIMS)} animations x {len(THEMES)} skins -> {os.path.normpath(SKINS)}")


if __name__ == "__main__":
    main()
