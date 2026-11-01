#!/usr/bin/env python3
"""Draws the orOS boot screen images (Pillow). The PNGs are kept in the
repository, so the build needs no Python; run this after changing the look.
  python3 scripts/boot-art.py
- assets/splash.png      GRUB (UEFI) and isolinux (BIOS) background, 640x480
- assets/splash-hd.png   GRUB background on screens that take a larger mode
- config/includes.chroot/usr/share/plymouth/themes/oros/watermark.png
                         the logo on the Plymouth screen (start and shut down)
- .../throbber-0001.png ... throbber-0036.png
                         three gold dots that breathe one after the other
The logo is orOS's icon.svg (koulaxizis/oros): the mountain in #d4af37 on
#1b1a18, the same colour Chromium shows before orOS paints. No words other
than the name: the language is not known yet while the computer starts.
"""
import math
import os
from PIL import Image, ImageDraw, ImageFont

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
THEME = os.path.join(ROOT, "config/includes.chroot/usr/share/plymouth/themes/oros")
BG = (0x1B, 0x1A, 0x18)
GOLD = (0xD4, 0xAF, 0x37)
NAME = (0xE8, 0xE2, 0xD9)
FONT = "/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf"
SS = 4  # supersampling for smooth edges

# icon.svg: viewBox 512, stroke 20.
OUTLINE = [(256, 104), (460, 408), (52, 408), (256, 104)]
RIDGE = [(256, 104), (342, 246), (388, 176), (460, 408)]


def mountain(draw, x, y, size, color):
    """The icon's mountain in a size x size box at x, y (box of the 512 viewBox)."""
    k = size / 512
    w = max(1, round(20 * k))
    for path in (OUTLINE, RIDGE):
        pts = [(x + px * k, y + py * k) for px, py in path]
        draw.line(pts, fill=color, width=w, joint="curve")
        for px, py in pts:
            r = w / 2
            draw.ellipse([px - r, py - r, px + r, py + r], fill=color)


def logo(width, height, size, name_px, bg=None):
    """Mountain with the name below it, centred, at SS times the size."""
    img = Image.new("RGBA", (width * SS, height * SS), bg + (255,) if bg else (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    font = ImageFont.truetype(FONT, name_px * SS)
    # The mountain fills y 104..408 of its box; the name sits below it.
    gap = name_px * 0.35
    art_top, art_h = 104 / 512 * size, (408 - 104) / 512 * size
    tb = d.textbbox((0, 0), "orOS", font=font)
    text_h = (tb[3] - tb[1]) / SS
    total = art_h + gap + text_h
    top = (height - total) / 2
    mountain(d, (width - size) / 2 * SS, (top - art_top) * SS, size * SS, GOLD)
    tw = (tb[2] - tb[0]) / SS
    d.text(((width - tw) / 2 * SS - tb[0], (top + art_h + gap) * SS - tb[1]), "orOS", font=font, fill=NAME)
    return img.resize((width, height), Image.LANCZOS)


def throbber(frames=36, w=104, h=24, r=6):
    """Three dots; each grows and brightens in turn (one cycle = frames)."""
    out = []
    for i in range(frames):
        img = Image.new("RGBA", (w * SS, h * SS), (0, 0, 0, 0))
        d = ImageDraw.Draw(img)
        for n in range(3):
            phase = (i / frames - n / 6) * 2 * math.pi
            t = (1 - math.cos(phase)) / 2  # 0..1
            rr = (r * (0.7 + 0.3 * t)) * SS
            a = int(255 * (0.25 + 0.75 * t))
            cx, cy = (w / 2 + (n - 1) * 26) * SS, h / 2 * SS
            d.ellipse([cx - rr, cy - rr, cx + rr, cy + rr], fill=GOLD + (a,))
        out.append(img.resize((w, h), Image.LANCZOS))
    return out


def main():
    logo(640, 480, 220, 44, BG).convert("RGB").save(os.path.join(ROOT, "assets/splash.png"), optimize=True)
    logo(1920, 1080, 440, 88, BG).convert("RGB").save(os.path.join(ROOT, "assets/splash-hd.png"), optimize=True)
    logo(320, 300, 240, 48).save(os.path.join(THEME, "watermark.png"), optimize=True)
    for f in os.listdir(THEME):
        if f.startswith("throbber-"):
            os.remove(os.path.join(THEME, f))
    for i, img in enumerate(throbber(), 1):
        img.save(os.path.join(THEME, "throbber-%04d.png" % i), optimize=True)


if __name__ == "__main__":
    main()
