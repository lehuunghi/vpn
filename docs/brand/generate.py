#!/usr/bin/env python3
"""Generate the VPN20 brand set for Công ty TNHH TN20.

Requires Pillow: python -m pip install Pillow
Run: python docs/brand/generate.py

The shield and monogram match web/src/components/Mark.tsx. Outputs include
SVG/PNG brand assets, browser favicons, PWA icons and the iOS touch icon.
"""
from pathlib import Path
from PIL import Image, ImageDraw, ImageFont

REPO = Path(__file__).resolve().parents[2]
OUT = REPO / "docs" / "brand"
PUB = REPO / "web" / "public"
NAVY, TEAL = "#17404f", "#46cac3"
SHIELD = [(64, 6), (114, 24), (114, 62), (102, 88),
          (64, 122), (26, 88), (14, 62), (14, 24)]
TWO = [(36, 50), (36, 42), (58, 42), (58, 58), (36, 80), (58, 80)]
ZERO = [(73, 42), (94, 42), (94, 80), (73, 80), (73, 42)]
SHIELD_PATH = "M64 6 L114 24 V62 L102 88 L64 122 L26 88 L14 62 V24 Z"
DIGIT_PATH = "M36 50 V42 H58 V58 L36 80 H58 M73 42 H94 V80 H73 Z"
COMPANY = "Công ty TNHH TN20"


def svg(mono=False):
    fill, stroke = ("none", "currentColor") if mono else (TEAL, NAVY)
    return (
        '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 128 128" '
        'width="128" height="128" role="img" aria-label="VPN20">\n'
        f'  <path d="{SHIELD_PATH}" fill="{fill}" stroke="{stroke}" '
        'stroke-width="6" stroke-linejoin="round"/>\n'
        f'  <path d="{DIGIT_PATH}" fill="none" stroke="{stroke}" '
        'stroke-width="8" stroke-linecap="round" stroke-linejoin="round"/>\n'
        '</svg>\n'
    )


def mark(size, mono=False):
    scale = max(4, size // 128 + 1)
    canvas = Image.new("RGBA", (128 * scale, 128 * scale))
    draw = ImageDraw.Draw(canvas)
    pts = lambda values: [(x * scale, y * scale) for x, y in values]
    draw.polygon(pts(SHIELD), fill=None if mono else TEAL)
    draw.line(pts(SHIELD + [SHIELD[0]]), fill=NAVY, width=6 * scale, joint="curve")
    for x, y in SHIELD:
        draw.ellipse(((x - 3) * scale, (y - 3) * scale,
                      (x + 3) * scale, (y + 3) * scale), fill=NAVY)
    for line in (TWO, ZERO):
        draw.line(pts(line), fill=NAVY, width=8 * scale, joint="curve")
        for x, y in line:
            draw.ellipse(((x - 4) * scale, (y - 4) * scale,
                          (x + 4) * scale, (y + 4) * scale), fill=NAVY)
    return canvas.resize((size, size), Image.Resampling.LANCZOS)


def font(size, bold=False):
    names = (["segoeuib.ttf", "DejaVuSans-Bold.ttf"] if bold
             else ["segoeui.ttf", "DejaVuSans.ttf"])
    for name in names:
        for candidate in (name, str(Path("C:/Windows/Fonts") / name),
                          "/usr/share/fonts/truetype/dejavu/" + name):
            try:
                return ImageFont.truetype(candidate, size)
            except OSError:
                pass
    return ImageFont.load_default(size=size)


def wordmark(dark=False):
    canvas = Image.new("RGBA", (936, 346))
    canvas.alpha_composite(mark(288), (24, 29))
    draw = ImageDraw.Draw(canvas)
    color = "#eaf6f6" if dark else NAVY
    draw.text((342, 65), "VPN20", font=font(126, True), fill=color)
    draw.text((347, 227), COMPANY, font=font(34), fill=color)
    return canvas


def main():
    OUT.mkdir(parents=True, exist_ok=True)
    PUB.mkdir(parents=True, exist_ok=True)
    (OUT / "vpn20-mark.svg").write_text(svg(), encoding="utf-8", newline="\n")
    (OUT / "vpn20-mark-mono.svg").write_text(svg(True), encoding="utf-8", newline="\n")
    (PUB / "favicon.svg").write_text(svg(), encoding="utf-8", newline="\n")
    for size in (256, 1024):
        mark(size).save(OUT / f"vpn20-mark-{size}.png")
    mark(256, True).save(OUT / "vpn20-mark-mono-256.png")
    wordmark().save(OUT / "vpn20-wordmark.png")
    wordmark(True).save(OUT / "vpn20-wordmark-dark.png")

    social = Image.new("RGBA", (1280, 640), "#0d2430")
    social.alpha_composite(mark(340), (96, 110))
    draw = ImageDraw.Draw(social)
    draw.text((494, 118), "VPN20", font=font(112, True), fill="#eaf6f6")
    draw.text((500, 269), COMPANY, font=font(40), fill=TEAL)
    draw.text((500, 342), "Secure VPN management", font=font(36), fill="#a3c3cb")
    draw.text((500, 398), "One container · 2FA · live dashboard",
              font=font(28), fill="#a3c3cb")
    draw.line((96, 520, 1184, 520), fill="#21505f", width=2)
    draw.text((96, 548), "github.com/lehuunghi/vpn", font=font(30), fill="#eaf6f6")
    social.convert("RGB").save(OUT / "vpn20-social.png")

    mark(48).save(PUB / "favicon.ico", sizes=[(16, 16), (32, 32), (48, 48)])
    for filename, size in (("favicon-32.png", 32), ("icon-192.png", 192),
                           ("icon-512.png", 512)):
        mark(size).save(PUB / filename)
    touch = Image.new("RGBA", (180, 180), "#0d2430")
    touch.alpha_composite(mark(144), (18, 18))
    touch.convert("RGB").save(PUB / "apple-touch-icon.png")
    print("VPN20 brand assets generated")


if __name__ == "__main__":
    main()
