#!/usr/bin/env python3
"""Build the "whale" skin from a character reference sheet.

    python3 tools/gen_whale_skin.py [sheet.png] [skins_dir]

The sheet (default: tools/assets/whale_sheet.png) holds a front (and side and back)
view of a chibi character on a white background; only the front view is used. The front view is cut out, then
every animation is made by transforming that picture (bob, squash, sway,
bow, hop, and a foot-by-foot walk) plus a few hand-drawn touches: closed / happy eyes and little marks
("!", "z", "...", a sweat drop). Frame names and counts follow
skins/default/pet.json, so any animation the pet asks for exists.

It writes the plain skin ("whale") and a set of themed ones: the same character
wearing a nightcap, a Santa hat, a witch hat, bunny ears, a New Year party hat
or spring-festival ornaments. Each themed skin carries its own `when` (night,
Christmas, Halloween, Mid-Autumn, New Year, Lunar New Year) so the wardrobe
puts it on by itself; the plain one is the all-year default.

Needs Pillow and numpy.
"""
import json
import math
import os
import sys

import numpy as np
from PIL import Image, ImageChops, ImageDraw, ImageFilter

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)

SIZE = 128          # final frame size
SS = 4              # supersampling for smooth transforms
SPRITE_H = 106      # final height of the character
FEET = 123          # y of the feet in the final frame
PAD = 24            # headroom above the character for hats and ears (final px)
CX = SIZE // 2

# Where things are on the sheet (pixels of the 516x387 reference).
FRONT_BOX = (4, 0, 193, 222)
# Eyes in front-view crop coordinates: (cx, cy, rx, ry)
EYES = [(65, 92, 11, 10), (101, 90, 10, 10)]
# Left and right foot (socks + shoe), below the skirt hem, in cut-out coordinates:
# (x0, y0, x1, y1). Walking lifts and swings them alternately.
LEGS = [(56, 190, 80, 216), (94, 190, 122, 216)]
STEP_LIFT = 7.0     # how high a foot comes up (cut-out pixels)
STEP_SWING = 4.5    # how far it swings forward/back
SKIN = (254, 233, 225)
INK = (28, 32, 92)


# --------------------------------------------------------------- cut-out
def cut(sheet, box, thresh=60):
    """Crop a view and remove the white background by flood fill from the edges."""
    from PIL import ImageDraw as D
    im = sheet.crop(box).convert("RGB")
    w, h = im.size
    fl = im.copy()
    mark = (255, 0, 255)

    def near_white(p):
        return sum(abs(c - 255) for c in p) < 70
    for x in range(w):
        for y in (0, h - 1):
            if fl.getpixel((x, y)) != mark and near_white(fl.getpixel((x, y))):
                D.floodfill(fl, (x, y), mark, thresh=thresh)
    for y in range(h):
        for x in (0, w - 1):
            if fl.getpixel((x, y)) != mark and near_white(fl.getpixel((x, y))):
                D.floodfill(fl, (x, y), mark, thresh=thresh)
    a = np.asarray(fl)
    bg = (a[:, :, 0] == 255) & (a[:, :, 1] == 0) & (a[:, :, 2] == 255)
    alpha = Image.fromarray(((~bg) * 255).astype("uint8"))
    alpha = alpha.filter(ImageFilter.MinFilter(3))       # drop the white fringe
    alpha = alpha.filter(ImageFilter.GaussianBlur(0.6))
    out = im.convert("RGBA")
    out.putalpha(alpha)
    return out.crop(out.getbbox())


def fit(sprite):
    """Scale to SPRITE_H*SS tall. Returns (image, factor relative to the crop)."""
    f = SPRITE_H * SS / sprite.height
    return sprite.resize((round(sprite.width * f), SPRITE_H * SS), Image.LANCZOS), f


# --------------------------------------------------------------- face edits
def edit_eyes(front_full, crop_origin, scale, mode):
    """Redraw the eyes on the big front sprite: 'closed' (soft arc) or 'happy' (^ ^)."""
    im = front_full.copy()
    ox, oy = crop_origin
    for cx, cy, rx, ry in EYES:
        x = (cx - ox) * scale
        y = (cy - oy) * scale
        mask = Image.new("L", im.size, 0)
        md = ImageDraw.Draw(mask)
        md.ellipse([x - (rx + 1) * scale, y - (ry + 1) * scale, x + (rx + 1) * scale, y + (ry + 1) * scale], fill=255)
        mask = mask.filter(ImageFilter.GaussianBlur(1.2 * scale))
        patch = Image.new("RGBA", im.size, SKIN + (255,))
        patch.putalpha(ImageChops.multiply(mask, im.getchannel("A")))
        im = Image.alpha_composite(im, patch)
        d = ImageDraw.Draw(im)
        w = max(2, round(1.7 * scale))
        pts = []
        for i in range(21):
            t = i / 20
            px = x + (t - 0.5) * 2 * rx * scale * 0.95
            bend = math.sin(math.pi * t) * 3.2 * scale
            py = y + (-bend if mode == "happy" else bend) + (1.0 * scale if mode == "closed" else 2.2 * scale)
            pts.append((px, py))
        d.line(pts, fill=INK + (255,), width=w, joint="curve")
        for p in (pts[0], pts[-1]):
            d.ellipse([p[0] - w / 2, p[1] - w / 2, p[0] + w / 2, p[1] + w / 2], fill=INK + (255,))
    return im


# --------------------------------------------------------------- marks
def _rr(d, box, fill, outline, w):
    d.rounded_rectangle(box, radius=(box[2] - box[0]) / 2, fill=fill, outline=outline, width=w)


def mark_bang(d, x, y, s=1.0):
    """'!' at (x, y) in final-frame pixels."""
    k = SS * s * 1.35
    w = 4.2 * k
    ow = max(2, round(1.1 * k))
    _rr(d, [x * SS - w / 2, y * SS, x * SS + w / 2, y * SS + 13 * k], (255, 190, 60, 255), (60, 40, 110, 255), ow)
    d.ellipse([x * SS - w / 2, y * SS + 16 * k, x * SS + w / 2, y * SS + 16 * k + w], fill=(255, 190, 60, 255), outline=(60, 40, 110, 255), width=ow)


def mark_z(d, x, y, size):
    k = SS * size
    w = max(2, round(1.3 * SS * size / 6))
    c = (150, 190, 255, 255)
    pts = [(x * SS, y * SS), (x * SS + k, y * SS), (x * SS, y * SS + k), (x * SS + k, y * SS + k)]
    d.line(pts, fill=(40, 50, 120, 255), width=w + 2 * SS // 2, joint="curve")
    d.line(pts, fill=c, width=w, joint="curve")


def mark_dots(d, x, y, n):
    for i in range(n):
        cx, cy, r = (x + i * 6) * SS, y * SS, 2.3 * SS
        d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=(255, 255, 255, 255), outline=(60, 40, 110, 255), width=max(2, SS // 2))


def mark_sweat(d, x, y):
    k = SS * 1.5
    ox, oy = x * SS, y * SS
    pts = [(ox, oy), (ox - 3.2 * k, oy + 6 * k), (ox, oy + 9.2 * k), (ox + 3.2 * k, oy + 6 * k)]
    d.polygon(pts, fill=(150, 210, 255, 255), outline=(40, 80, 160, 255))


# --------------------------------------------------------------- compose
def place(sprite, dx=0.0, dy=0.0, sx=1.0, sy=1.0, rot=0.0, pivot="bottom", flip=False, marks=None):
    """Draw `sprite` on a SIZE*SS canvas: scale about the pivot, rotate, shift."""
    if flip:
        sprite = sprite.transpose(Image.FLIP_LEFT_RIGHT)
    w, h = sprite.size
    s = sprite.resize((max(1, round(w * sx)), max(1, round(h * sy))), Image.LANCZOS)
    layer = Image.new("RGBA", (SIZE * SS, SIZE * SS), (0, 0, 0, 0))
    px = SIZE * SS // 2 - s.width // 2
    if pivot == "bottom":
        py = FEET * SS - s.height
        center = (SIZE * SS // 2, FEET * SS)
    else:  # hang from the top of the character (below the headroom)
        py = FEET * SS - s.height
        center = (SIZE * SS // 2, (FEET - SPRITE_H) * SS)
    layer.paste(s, (px, py), s)
    layer = layer.rotate(rot, center=center, resample=Image.BICUBIC, translate=(dx * SS, dy * SS))
    if marks:
        d = ImageDraw.Draw(layer)
        for fn, args in marks:
            fn(d, *args)
    return layer.resize((SIZE, SIZE), Image.LANCZOS)


# --------------------------------------------------------------- accessories
OUT = (52, 38, 84, 255)


def bez(p0, p1, p2, p3, n=18):
    pts = []
    for i in range(n + 1):
        t = i / n
        a = (1 - t) ** 3
        b = 3 * (1 - t) ** 2 * t
        c = 3 * (1 - t) * t ** 2
        e = t ** 3
        pts.append((a * p0[0] + b * p1[0] + c * p2[0] + e * p3[0], a * p0[1] + b * p1[1] + c * p2[1] + e * p3[1]))
    return pts


class Pen:
    """Draws on the big (padded) sprite using head-local coordinates.

    Local units: the hat is ~100 wide, y grows downwards, (0, 0) is the middle of
    its base. `tilt` is degrees, clockwise on screen.
    """

    def __init__(self, img, f, cx, cy, w, tilt):
        self.d = ImageDraw.Draw(img)
        self.f = f
        self.cx, self.cy = cx, cy
        self.k = w / 100.0
        self.th = math.radians(tilt)
        self.vs = 0.82  # hats are squatter than drawn, to stay inside the frame

    def at(self, u, v):
        k = self.k
        v = v * self.vs
        x = (u * math.cos(self.th) - v * math.sin(self.th)) * k
        y = (u * math.sin(self.th) + v * math.cos(self.th)) * k
        return ((self.cx + x) * self.f, (self.cy + y) * self.f + PAD * SS)

    def px(self, n=1.0):
        return max(2, round(n * self.f * self.k))

    def poly(self, pts, fill, outline=OUT, w=1.2):
        P = [self.at(*p) for p in pts]
        self.d.polygon(P, fill=fill)
        if outline:
            self.d.line(P + [P[0]], fill=outline, width=self.px(w), joint="curve")

    def circle(self, u, v, r, fill, outline=OUT, w=1.2):
        x, y = self.at(u, v)
        R = r * self.k * self.f
        self.d.ellipse([x - R, y - R, x + R, y + R], fill=fill, outline=outline, width=self.px(w) if outline else 0)

    def ellipse_poly(self, u, v, rx, ry, rot, fill, outline=OUT, w=1.2):
        pts = []
        c, s_ = math.cos(math.radians(rot)), math.sin(math.radians(rot))
        for i in range(36):
            a = 2 * math.pi * i / 36
            ex, ey = rx * math.cos(a), ry * math.sin(a)
            pts.append((u + ex * c - ey * s_, v + ex * s_ + ey * c))
        self.poly(pts, fill, outline, w)

    def line(self, pts, fill, w=1.2):
        self.d.line([self.at(*p) for p in pts], fill=fill, width=self.px(w), joint="curve")

    def rrect(self, u0, v0, u1, v1, fill, outline=OUT, w=1.0):
        a, b = self.at(u0, v0), self.at(u1, v1)
        self.d.rounded_rectangle([min(a[0], b[0]), min(a[1], b[1]), max(a[0], b[0]), max(a[1], b[1])],
                                 radius=min(abs(b[1] - a[1]), abs(b[0] - a[0])) / 2.2, fill=fill, outline=outline, width=self.px(w))


def drooping_cap(P, body, shade, band, pom, band_out=(150, 155, 175, 255)):
    pts = bez((-46, 2), (-44, -40), (-10, -66), (26, -56)) + bez((26, -56), (44, -50), (54, -30), (50, -12))[1:]
    pts += [(46, 2)]
    P.poly(pts, body, tuple(max(0, c - 70) for c in body[:3]) + (255,))
    P.poly(bez((-40, -6), (-38, -34), (-14, -56), (18, -52), 10), shade, None)
    P.rrect(-52, -9, 52, 9, band, band_out, 1.0)
    P.circle(52, -12, 9, pom, band_out, 1.0)


def paint_santa(P, view, f, pad_fn):
    drooping_cap(P, (212, 38, 52, 255), (232, 70, 80, 90), (252, 252, 252, 255), (252, 252, 252, 255))


def paint_night(P, view, f, pad_fn):
    drooping_cap(P, (60, 72, 156, 255), (92, 108, 190, 90), (150, 176, 232, 255), (255, 232, 130, 255), (110, 130, 190, 255))
    P.circle(-14, -34, 4.2, (255, 232, 130, 255), None)
    P.circle(14, -22, 2.8, (255, 232, 130, 255), None)
    P.circle(2, -46, 2.2, (255, 232, 130, 255), None)


def paint_witch(P, view, f, pad_fn):
    P.ellipse_poly(0, 0, 58, 9, 0, (74, 48, 118, 255))
    P.poly([(-30, -2), (-21, -38), (-8, -66), (8, -86), (24, -82), (15, -62), (25, -36), (32, -2)], (84, 54, 134, 255), (40, 24, 72, 255))
    P.poly([(-28, -14), (29, -14), (31, -4), (-31, -4)], (255, 150, 50, 255), (150, 80, 20, 255), 1.0)
    P.rrect(-6, -15, 6, -3, (255, 214, 90, 255), (150, 110, 30, 255), 0.9)


def paint_bunny(P, view, f, pad_fn):
    for u, tilt in ((-24, -13), (24, 11)):
        P.ellipse_poly(u + tilt * 0.7, -38, 11.5, 36, tilt, (253, 251, 253, 255), (150, 150, 180, 255))
        P.ellipse_poly(u + tilt * 0.7, -38, 6, 28, tilt, (255, 192, 208, 255), None)
    # the moon floats in a corner of the frame
    mx, my = (24, 28) if view == "front" else (120, 26)
    cx, cy = pad_fn(mx, my)
    R = 13 * f
    glow = Image.new("RGBA", P.d.im.size if hasattr(P.d, "im") else (1, 1), (0, 0, 0, 0))
    P.d.ellipse([cx - R * 1.35, cy - R * 1.35, cx + R * 1.35, cy + R * 1.35], fill=(255, 240, 170, 70))
    P.d.ellipse([cx - R, cy - R, cx + R, cy + R], fill=(255, 238, 150, 255), outline=(214, 172, 62, 255), width=max(2, round(f * 0.9)))
    for dx, dy, r in ((-4, -3, 3.2), (4, 4, 2.4), (3, -6, 1.8)):
        P.d.ellipse([cx + (dx - r) * f, cy + (dy - r) * f, cx + (dx + r) * f, cy + (dy + r) * f], fill=(240, 214, 118, 255))


def paint_spring(P, view, f, pad_fn):
    for u in (-40, 40):
        P.line([(u, 4), (u + (2 if u > 0 else -2), 26)], (240, 190, 70, 255), 1.6)
        P.circle(u, -2, 10, (214, 30, 46, 255), (110, 16, 28, 255), 1.1)
        P.circle(u - 3, -6, 3, (250, 110, 120, 255), None)
        P.circle(u, -2, 4.4, (250, 205, 80, 255), (170, 120, 30, 255), 0.8)
    lx, ly = pad_fn(*((24, 30) if view == "front" else (120, 28)))
    w, h = 13 * f, 10.5 * f
    d = P.d
    d.line([(lx, ly - h - 14 * f), (lx, ly - h)], fill=(120, 60, 30, 255), width=max(2, round(f * 0.7)))
    d.ellipse([lx - w, ly - h, lx + w, ly + h], fill=(220, 36, 46, 255), outline=(120, 20, 30, 255), width=max(2, round(f * 0.9)))
    for dx in (-0.5, 0.0, 0.5):
        d.arc([lx - w * abs(dx) * 2 - 0.1, ly - h, lx + w * abs(dx) * 2 + 0.1, ly + h], 90, 270, fill=(246, 100, 100, 255), width=max(1, round(f * 0.4)))
    d.rectangle([lx - 6 * f, ly - h - 3 * f, lx + 6 * f, ly - h + 1 * f], fill=(240, 196, 80, 255), outline=(150, 110, 30, 255))
    d.rectangle([lx - 6 * f, ly + h - 1 * f, lx + 6 * f, ly + h + 3 * f], fill=(240, 196, 80, 255), outline=(150, 110, 30, 255))
    for dx in (-3, 0, 3):
        d.line([(lx + dx * f, ly + h + 3 * f), (lx + dx * f * 1.3, ly + h + 14 * f)], fill=(240, 196, 80, 255), width=max(2, round(f * 0.6)))
    font = _cjk_font(int(13 * f))
    if font:
        d.text((lx, ly - 0.5 * f), "福", font=font, fill=(255, 218, 100, 255), anchor="mm")


def paint_party(P, view, f, pad_fn):
    P.poly([(-26, 0), (0, -74), (26, 0)], (255, 120, 164, 255), (150, 40, 90, 255))
    for v0, v1, col in ((-14, -24, (255, 226, 90, 255)), (-34, -44, (120, 200, 255, 255)), (-54, -62, (255, 226, 90, 255))):
        hw0, hw1 = 26 * (1 + v0 / 74), 26 * (1 + v1 / 74)
        P.poly([(-hw0, v0), (hw0, v0), (hw1, v1), (-hw1, v1)], col, None)
    P.circle(0, -76, 7.5, (255, 236, 130, 255), (170, 130, 40, 255), 1.0)
    P.rrect(-30, -3, 30, 5, (255, 236, 130, 255), (170, 130, 40, 255), 0.9)
    import random
    rnd = random.Random(7)
    cols = [(255, 120, 164), (120, 200, 255), (255, 226, 90), (160, 230, 150), (200, 150, 255)]
    for _ in range(26):
        x = rnd.uniform(4, 185 if view == "front" else 140)
        y = rnd.uniform(-20, 70)
        cx, cy = pad_fn(x, y)
        r = rnd.uniform(1.4, 2.4) * f
        P.d.rectangle([cx - r, cy - r * 0.6, cx + r, cy + r * 0.6], fill=rnd.choice(cols) + (255,))


def paint_bat(P, view, f, pad_fn):
    cx, cy = pad_fn(*((22, 30) if view == "front" else (122, 30)))
    k = 2.0 * f
    pts = [(-11, 0), (-7, -6), (-3.5, -2.5), (0, -5.5), (3.5, -2.5), (7, -6), (11, 0), (6, -0.5), (3.5, 4), (0, 1.5), (-3.5, 4), (-6, -0.5)]
    P.d.polygon([(cx + x * k, cy + y * k) for x, y in pts], fill=(48, 34, 72, 255), outline=(20, 12, 36, 255))
    for ex in (-1.1, 1.1):
        P.d.ellipse([cx + (ex - 0.5) * k, cy - 3.6 * k, cx + (ex + 0.5) * k, cy - 2.6 * k], fill=(255, 200, 60, 255))


def paint_witch_bat(P, view, f, pad_fn):
    paint_witch(P, view, f, pad_fn)
    paint_bat(P, view, f, pad_fn)


def _cjk_font(size):
    from PIL import ImageFont
    for path in ("/usr/share/fonts/opentype/noto/NotoSansCJK-Bold.ttc", "/usr/share/fonts/opentype/noto/NotoSerifCJK-Bold.ttc",
                 "/usr/share/fonts/truetype/noto/NotoSansCJK-Bold.ttc", "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc"):
        if os.path.exists(path):
            try:
                return ImageFont.truetype(path, size)
            except OSError:
                pass
    return None


# head anchors in cut-out coordinates: (cx, cy, hat width, tilt)
HEAD = {"front": (90, 27, 88, 0), "side": (62, 24, 68, 9)}
ALL_MONTHS = list(range(1, 13))

THEMES = [
    # name, label, paint, tint, when, priority
    ("whale", "鲸鱼女仆", None, None, {"months": ALL_MONTHS}, 15),
    ("whale-night", "鲸鱼 · 夜晚", paint_night, (0.8, 0.86, 1.0), {"from": "22:00", "to": "07:00"}, 20),
    ("whale-christmas", "鲸鱼 · 圣诞", paint_santa, None, {"holidays": ["christmas"]}, 40),
    ("whale-halloween", "鲸鱼 · 万圣节", paint_witch_bat, None, {"holidays": ["halloween"]}, 40),
    ("whale-midautumn", "鲸鱼 · 中秋", paint_bunny, None, {"holidays": ["mid_autumn"]}, 40),
    ("whale-springfestival", "鲸鱼 · 春节", paint_spring, None, {"holidays": ["lunar_new_year"]}, 40),
    ("whale-newyear", "鲸鱼 · 新年", paint_party, None, {"holidays": ["new_year"]}, 41),
]


def pad_top(im):
    out = Image.new("RGBA", (im.width, im.height + PAD * SS), (0, 0, 0, 0))
    out.paste(im, (0, PAD * SS), im)
    return out


def tinted(im, mul):
    if not mul:
        return im
    a = np.asarray(im).astype(float)
    for c in range(3):
        a[:, :, c] *= mul[c]
    return Image.fromarray(a.clip(0, 255).astype("uint8"), "RGBA")


def walk_sprite(base, f, i, n):
    """The front sprite with its feet taken apart: one foot swings while the other
    stays on the ground, half a cycle apart. Facing right, so forward is +x."""
    body = base.copy()
    layer = Image.new("RGBA", base.size, (0, 0, 0, 0))
    for k, (x0, y0, x1, y1) in enumerate(LEGS):
        bb = (round(x0 * f), round(y0 * f + PAD * SS), min(base.width, round(x1 * f)), min(base.height, round(y1 * f + PAD * SS)))
        piece = base.crop(bb)
        body.paste(Image.new("RGBA", piece.size, (0, 0, 0, 0)), bb[:2])
        phase = (i / n + 0.5 * k) % 1.0
        if phase < 0.5:                      # swinging: up and forward
            s_ = phase / 0.5
            lift, dx = math.sin(math.pi * s_) * STEP_LIFT, -STEP_SWING + 2 * STEP_SWING * s_
        else:                                # planted: slides back under the body
            s_ = (phase - 0.5) / 0.5
            lift, dx = 0.0, STEP_SWING - 2 * STEP_SWING * s_
        layer.paste(piece, (round(bb[0] + dx * f), round(bb[1] - lift * f)), piece)
    return Image.alpha_composite(layer, body)


def build(sheet_path, out_root):
    sheet = Image.open(sheet_path).convert("RGB")
    front_crop = cut(sheet, FRONT_BOX)
    bx, by = cut_full_alpha(sheet, FRONT_BOX).getbbox()[:2]

    front, f = fit(front_crop)
    variants = {
        "open": front,
        "closed": edit_eyes(front, (bx, by), f, "closed"),
        "happy": edit_eyes(front, (bx, by), f, "happy"),
    }
    with open(os.path.join(ROOT, "skins", "default", "pet.json"), encoding="utf-8") as fh:
        base = json.load(fh)

    for name, label, paint, tint, when, prio in THEMES:
        sprites = {k: pad_top(v) for k, v in variants.items()}
        if paint:
            cx, cy, w, tilt = HEAD["front"]
            for im in sprites.values():
                P = Pen(im, f, cx, cy, w, tilt)
                paint(P, "front", f, lambda x, y: (x * f, y * f + PAD * SS))
        sprites = {k: tinted(v, tint) for k, v in sprites.items()}
        write_skin(os.path.join(out_root, name), name, label, sprites, f, base, when, prio, hat=paint is not None)


def write_skin(out_dir, name, label, S, f, base, when, prio, hat):
    jump = 0.4 if hat else 1.0  # hats would leave the frame on a big jump
    def wave(i, n, k=1):
        return math.sin(2 * math.pi * k * i / n)

    def gen(anim, i, n):
        t = i / max(1, n - 1)
        if anim == "idle":
            b = (1 - math.cos(2 * math.pi * i / n)) / 2
            return place(S["open"], sy=1 + 0.026 * b, sx=1 - 0.008 * b)
        if anim == "blink":
            return place(S["open" if i in (0, n - 1) else "closed"])
        if anim == "walk":
            bob = abs(math.sin(2 * math.pi * i / n))
            return place(walk_sprite(S["open"], f, i, n), dy=-1.6 * bob, rot=-2.2 + 1.2 * wave(i, n))
        if anim == "sit":
            return place(S["open"], sy=0.9 - 0.012 * (i % 2), sx=1.04 + 0.01 * (i % 2), dy=1)
        if anim == "sleep":
            b = i / max(1, n - 1)
            marks = [(mark_z, (88, 24, 5))]
            if i >= 1:
                marks.append((mark_z, (98, 14, 6.5)))
            if i >= 2:
                marks.append((mark_z, (107, 4, 8)))
            return place(S["closed"], sy=0.9 + 0.03 * b, sx=1.03 - 0.01 * b, rot=-5, dy=2, marks=marks)
        if anim == "wave":
            eyes = "happy" if 1 <= i <= n - 2 else "open"
            mid = 1 if 0 < i < n - 1 else 0
            return place(S[eyes], rot=5.5 * wave(i, n, 2) * (1 if mid else 0.3), dy=-3.5 * abs(math.sin(math.pi * i / n * 2)) * mid)
        if anim == "happy":
            lift = math.sin(math.pi * t)
            squash = 1 - 0.09 * (1 - lift) if i in (0, n - 1) else 1 + 0.05 * lift
            eyes = "happy" if 1 <= i <= n - 2 else "open"
            return place(S[eyes], dy=-14 * jump * lift, sy=squash, sx=2 - squash if squash < 1 else 1 - 0.03 * lift)
        if anim == "think":
            return place(S["open"], rot=-3 + 1.2 * wave(i, n), marks=[(mark_dots, (92, 12, 2 + i % 2 + (1 if i == n - 1 else 0)))])
        if anim == "apologize":
            bow = math.sin(math.pi * min(1.0, t))
            eyes = "closed" if 1 <= i <= n - 2 else "open"
            marks = [(mark_sweat, (100, 14))] if 2 <= i <= n - 2 else None
            return place(S[eyes], sy=1 - 0.1 * bow, sx=1 + 0.03 * bow, rot=-9 * bow, dy=1.5 * bow, marks=marks)
        if anim == "drag":
            return place(S["open"], sy=1.05, sx=0.96, rot=4.5 * (1 if i % 2 == 0 else -1), pivot="top", dy=2)
        if anim == "react":
            lift = [0, 1, 0.65, 0.2, 0][min(i, 4)] if n == 5 else math.sin(math.pi * t)
            marks = [(mark_bang, (104, 8))] if 1 <= i <= n - 2 else None
            return place(S["open"], dy=-9 * jump * lift, sy=1 + 0.06 * lift, sx=1 - 0.03 * lift, marks=marks)
        if anim == "notify":
            hop = abs(math.sin(math.pi * i / n * 2)) if 0 < i < n - 1 else 0
            eyes = "happy" if 2 <= i <= n - 3 else "open"
            marks = [(mark_bang, (104, 8))] if 1 <= i <= n - 1 else None
            return place(S[eyes], dy=-6 * jump * hop, rot=3 * wave(i, n, 2) * (1 if 0 < i < n - 1 else 0), marks=marks)
        return place(S["open"])

    os.makedirs(out_dir, exist_ok=True)
    for stale in os.listdir(out_dir):
        if stale.endswith(".png"):
            os.remove(os.path.join(out_dir, stale))
    anims = {}
    for anim, spec in base["anims"].items():
        n = len(spec["frames"])
        frames = []
        for i in range(n):
            fn = f"{anim}_{i}.png"
            gen(anim, i, n).save(os.path.join(out_dir, fn), optimize=True)
            frames.append(fn)
        anims[anim] = {"frames": frames, "fps": spec["fps"], "loop": spec["loop"]}
    manifest = {
        "name": name,
        "label": label,
        "size": [SIZE, SIZE],
        "bubble_anchor": [0.5, 0.06 if hat else round((FEET - SPRITE_H) / SIZE + 0.02, 2)],
        "when": when,
        "priority": prio,
        "anims": anims,
    }
    with open(os.path.join(out_dir, "pet.json"), "w", encoding="utf-8") as fh:
        json.dump(manifest, fh, ensure_ascii=False, indent=2)
        fh.write("\n")
    print(f"{name}: {sum(len(a['frames']) for a in anims.values())} frames")


def cut_full_alpha(sheet, box):
    """Same cut-out as cut() but without the final bbox crop (to find its offset)."""
    from PIL import ImageDraw as D
    im = sheet.crop(box).convert("RGB")
    w, h = im.size
    fl = im.copy()
    mark = (255, 0, 255)

    def near_white(p):
        return sum(abs(c - 255) for c in p) < 70
    for x in range(w):
        for y in (0, h - 1):
            if fl.getpixel((x, y)) != mark and near_white(fl.getpixel((x, y))):
                D.floodfill(fl, (x, y), mark, thresh=60)
    for y in range(h):
        for x in (0, w - 1):
            if fl.getpixel((x, y)) != mark and near_white(fl.getpixel((x, y))):
                D.floodfill(fl, (x, y), mark, thresh=60)
    a = np.asarray(fl)
    bg = (a[:, :, 0] == 255) & (a[:, :, 1] == 0) & (a[:, :, 2] == 255)
    alpha = Image.fromarray(((~bg) * 255).astype("uint8")).filter(ImageFilter.MinFilter(3))
    return alpha


if __name__ == "__main__":
    sheet = sys.argv[1] if len(sys.argv) > 1 else os.path.join(HERE, "assets", "whale_sheet.png")
    out = sys.argv[2] if len(sys.argv) > 2 else os.path.join(ROOT, "skins")
    build(sheet, out)
