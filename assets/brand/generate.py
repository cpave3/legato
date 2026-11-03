#!/usr/bin/env python3
"""Draws the Legato logo and every file made from it.

    python3 assets/brand/generate.py            # rewrite the files in this repository
    python3 assets/brand/generate.py --out DIR  # write them under DIR instead

Writes the SVGs and PNGs in assets/brand/ and the app icons of the web app,
the docs site, the browser extension, the Stream Deck plugin and the Android
app, each at its own path and size.

Needs fontTools (pip install fonttools), rsvg-convert (librsvg) and magick
(ImageMagick 7). The wordmark is Chakra Petch SemiBold, kept in font/ under
the SIL Open Font License and written into the SVGs as outlines.

The mark is drawn on an 800-unit grid, where it spans x 191..603, y 140..570:
- two strands from one centreline: a wide stroke with a narrow gap stroke
  masked out of it;
- the foot, which leaves from under the inner strand (a mask along the
  strand's lower edge hides its start);
- corners rounded with quadratic curves whose control point is the corner;
- flush ends: everything below y=570 is cut away.
"""
import argparse
import math
import os
import subprocess
import tempfile

from fontTools.pens.svgPathPen import SVGPathPen
from fontTools.pens.transformPen import TransformPen
from fontTools.ttLib import TTFont

HERE = os.path.dirname(os.path.abspath(__file__))
REPO = os.path.dirname(os.path.dirname(HERE))

# The mark's proportions: strand pair width, gap between strands, foot width,
# corner rounding.
W, G, F, CORNER = 76, 14, 38, 40
FOOT_CORNERS = (18, 42, 30)
TOP, BOTTOM = 140, 570

# Unit vector along the strands' diagonal, and the normal to it.
U = (-0.8203, 0.5719)
N = (0.5719, 0.8203)

INK = '#141425'      # one-colour mark and dark text
DARK = '#0E0F1A'     # dark tile, Apple touch and PWA icons
ANDROID = '#0A0A0F'  # mobile/app/src/main/res/values/colors.xml iconBackground
DECK = '#18181B'     # Stream Deck tiles


def rounded(pts, radius):
    """A polyline through pts with each inner corner rounded."""
    out = f'M{pts[0][0]:.1f} {pts[0][1]:.1f}'
    for i in range(1, len(pts) - 1):
        p, c, n = pts[i - 1], pts[i], pts[i + 1]
        d = radius[i - 1] if isinstance(radius, list) else radius

        def towards(a, b, dist):
            length = math.dist(a, b)
            dist = min(dist, length / 2)
            return (a[0] + (b[0] - a[0]) * dist / length, a[1] + (b[1] - a[1]) * dist / length)

        b, a = towards(c, p, d), towards(c, n, d)
        out += f'L{b[0]:.1f} {b[1]:.1f}Q{c[0]} {c[1]} {a[0]:.1f} {a[1]:.1f}'
    return out + f'L{pts[-1][0]:.1f} {pts[-1][1]:.1f}'


def strand_edge():
    """A polygon covering everything above the inner strand's lower edge."""
    p = (480 + N[0] * W / 2, 235 + N[1] * W / 2)
    a = (p[0] - U[0] * 700, p[1] - U[1] * 700)
    b = (p[0] + U[0] * 700, p[1] + U[1] * 700)
    return f'{a[0]:.1f},{a[1]:.1f} {b[0]:.1f},{b[1]:.1f} {b[0]:.1f},-200 {a[0]:.1f},-200'


def mark(p, mono=False):
    """The mark's SVG elements. p prefixes its ids so several fit in one file;
    mono draws it in currentColor, otherwise it needs gradients(p)."""
    centre = rounded([(480, TOP), (480, 235), (229, 410), (229, BOTTOM)], CORNER)
    corner, last = (530, 482), (0.5, 0.866)
    end = (corner[0] + last[0] * 140, corner[1] + last[1] * 140)
    foot = rounded([(350, 318), (318, 380), (318, 482), corner, end], list(FOOT_CORNERS))
    foot_paint = 'currentColor' if mono else f'url(#{p}f)'
    strand_paint = 'currentColor' if mono else f'url(#{p}s)'
    cut = f'<rect x="0" y="{BOTTOM}" width="800" height="200" fill="#000"/>'
    return (
        '<defs>'
        f'<mask id="{p}m" maskUnits="userSpaceOnUse" x="0" y="0" width="800" height="800">'
        f'<g fill="none" stroke-linejoin="round"><path d="{centre}" stroke="#fff" stroke-width="{W}"/>'
        f'<path d="{centre}" stroke="#000" stroke-width="{G}"/></g>{cut}</mask>'
        f'<mask id="{p}k" maskUnits="userSpaceOnUse" x="0" y="0" width="800" height="800">'
        f'<rect width="800" height="800" fill="#fff"/><polygon points="{strand_edge()}" fill="#000"/>{cut}</mask>'
        '</defs>'
        f'<g mask="url(#{p}k)"><path d="{foot}" fill="none" stroke="{foot_paint}" stroke-width="{F}" stroke-linejoin="round"/></g>'
        f'<rect x="0" y="0" width="800" height="800" fill="{strand_paint}" mask="url(#{p}m)"/>'
    )


def gradients(p):
    """The strands' cyan-to-violet and the foot's violet-to-amber gradients."""
    return (
        f'<linearGradient id="{p}s" gradientUnits="userSpaceOnUse" x1="520" y1="150" x2="200" y2="540">'
        '<stop offset="0" stop-color="#2BDCF2"/><stop offset="0.3" stop-color="#4FB8F7"/>'
        '<stop offset="0.5" stop-color="#8A7CF6"/><stop offset="0.72" stop-color="#6544DF"/>'
        '<stop offset="1" stop-color="#3A1E9C"/></linearGradient>'
        f'<linearGradient id="{p}f" gradientUnits="userSpaceOnUse" x1="300" y1="380" x2="600" y2="560">'
        '<stop offset="0" stop-color="#9B3FD9"/><stop offset="0.3" stop-color="#F0507F"/>'
        '<stop offset="0.65" stop-color="#FB923C"/><stop offset="1" stop-color="#FBCB2E"/></linearGradient>'
    )


def coloured(p):
    return f'<defs>{gradients(p)}</defs>' + mark(p)


def svg(viewbox, body, extra=''):
    return f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="{viewbox}"{extra}>{body}</svg>\n'


def wordmark(text, size, x, baseline, track):
    """text in Chakra Petch SemiBold as one path, and where it ends."""
    font = TTFont(os.path.join(HERE, 'font', 'ChakraPetch-SemiBold.ttf'))
    glyphs, cmap, scale, d = font.getGlyphSet(), font.getBestCmap(), size / 1000, ''
    for ch in text:
        name = cmap[ord(ch)]
        pen = SVGPathPen(glyphs)
        glyphs[name].draw(TransformPen(pen, (scale, 0, 0, -scale, x, baseline)))
        d += pen.getCommands()
        x += font['hmtx'][name][0] * scale + track
    return d, x


def tile(p, rx, background, mono=False):
    """The mark on a filled 800-unit tile."""
    if mono:
        # The white mark on the logo's diagonal gradient.
        fill = f'url(#{p}bg)'
        defs = (f'<defs><linearGradient id="{p}bg" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="800" y2="800">'
                '<stop offset="0" stop-color="#22D3EE"/><stop offset="0.3" stop-color="#3B82F6"/>'
                '<stop offset="0.55" stop-color="#7C3AED"/><stop offset="0.8" stop-color="#F0507F"/>'
                '<stop offset="1" stop-color="#FBBF24"/></linearGradient></defs>')
        art = f'<g color="#FFFFFF" transform="translate(400 400) scale(1.2) translate(-397 -367)">{mark(p, mono=True)}</g>'
        return svg('0 0 800 800', defs + f'<rect width="800" height="800" rx="{rx}" fill="{fill}"/>' + art)
    art = f'<g transform="translate(400 400) scale(1.38) translate(-397 -355)">{coloured(p)}</g>'
    return svg('0 0 800 800', f'<rect width="800" height="800" rx="{rx}" fill="{background}"/>' + art)


def brand_svgs():
    """assets/brand/*.svg, by file name."""
    files = {
        'legato-mark.svg': svg('172 125 460 460', coloured('lg')),
        'legato-mark-icon.svg': svg('180 133 444 444', coloured('lg')),  # tighter, for favicons
        'legato-mark-mono.svg': svg('172 125 460 460', mark('lgm', mono=True), f' color="{INK}"'),
        'legato-tile.svg': tile('lt', 96, DARK),
        'legato-tile-square.svg': tile('lts', 0, DARK),
        'legato-tile-gradient.svg': tile('ltg', 176, None, mono=True),
        'legato-tile-gradient-square.svg': tile('ltgs', 0, None, mono=True),
    }
    # The wordmark's x-height sits a little below the mark's centre.
    size, gap = 250, 58
    d, end = wordmark('legato', size, 603 + gap, 355 + 498 * size / 1000 / 2 + 30, 6)
    lockup = f'171 120 {end + 20 - 171:.0f} 470'
    for name, text in (('dark', '#FFFFFF'), ('light', INK)):
        files[f'legato-lockup-{name}.svg'] = svg(lockup, coloured('lw' + name) + f'<path d="{d}" fill="{text}"/>')
    files['legato-lockup-mono.svg'] = svg(
        lockup, mark('lwm', mono=True) + f'<path d="{d}" fill="currentColor"/>', f' color="{INK}"')
    files['legato-wordmark.svg'] = svg(
        f'{603 + gap - 10} 120 {end - 603 - gap + 30:.0f} 470', f'<path d="{d}" fill="currentColor"/>', f' color="{INK}"')
    return files


def icon(fraction, background=None, shape='square', p='m'):
    """An 800-unit square with the mark at fraction of its height, centred."""
    s = 800 * fraction / 430
    back = {
        'square': f'<rect width="800" height="800" fill="{background}"/>',
        'rounded': f'<rect width="800" height="800" rx="176" fill="{background}"/>',
        'circle': f'<circle cx="400" cy="400" r="400" fill="{background}"/>',
    }[shape] if background else ''
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 800">{back}<defs>{gradients(p)}</defs>'
            f'<g transform="translate(400 400) scale({s:.4f}) translate(-397 -355)">{mark(p)}</g></svg>')


# PNG chunks that hold the time a file was written, left out so a rerun
# changes nothing.
NO_DATES = ['-define', 'png:exclude-chunks=date,time']


def rasterise(svg_text, path, width, height=None):
    """Renders svg_text to path; .webp and .ico go through ImageMagick."""
    os.makedirs(os.path.dirname(path), exist_ok=True)
    with tempfile.TemporaryDirectory() as tmp:
        src, png = os.path.join(tmp, 'a.svg'), os.path.join(tmp, 'a.png')
        with open(src, 'w') as f:
            f.write(svg_text)
        size = (['-w', str(width)] if width else []) + (['-h', str(height)] if height else [])
        if path.endswith('.webp'):
            subprocess.run(['rsvg-convert', *size, src, '-o', png], check=True)
            subprocess.run(['magick', png, '-define', 'webp:lossless=true', path], check=True)
        else:
            subprocess.run(['rsvg-convert', *size, src, '-o', path], check=True)


def favicon_ico(svg_text, path):
    with tempfile.TemporaryDirectory() as tmp:
        sizes = []
        for n in (16, 32, 48):
            png = os.path.join(tmp, f'{n}.png')
            rasterise(svg_text, png, n, n)
            sizes.append(png)
        subprocess.run(['magick', *sizes, *NO_DATES, path], check=True)


def app_icons(out):
    bare = icon(0.94)               # transparent, nearly edge to edge: favicons, logos
    tile_ = icon(0.62, DARK)        # opaque: iOS rounds it, and the PWA's maskable safe zone holds it
    deck = icon(0.62, DECK)
    at = lambda rel: os.path.join(out, rel)
    for n in (16, 32, 48, 128):
        rasterise(bare, at(f'browser-extension/public/icons/icon-{n}.png'), n, n)
    for root in ('web/public', 'site/public'):
        rasterise(bare, at(f'{root}/favicon-32x32.png'), 32, 32)
        rasterise(tile_, at(f'{root}/apple-touch-icon.png'), 180, 180)
        favicon_ico(bare, at(f'{root}/favicon.ico'))
    rasterise(bare, at('web/public/favicon-16x16.png'), 16, 16)
    rasterise(tile_, at('web/public/pwa-192x192.png'), 192, 192)
    rasterise(tile_, at('web/public/pwa-512x512.png'), 512, 512)
    rasterise(bare, at('site/src/assets/logo.png'), 192, 192)
    for name in ('plugin', 'tile'):
        rasterise(deck, at(f'streamdeck/plugin/icons/{name}.png'), 144, 144)
    res = 'mobile/app/src/main/res'
    for density, n in (('mdpi', 48), ('hdpi', 72), ('xhdpi', 96), ('xxhdpi', 144), ('xxxhdpi', 192)):
        # The adaptive icon's foreground is 108dp with a 66dp safe circle.
        rasterise(icon(0.46, p='f'), at(f'{res}/mipmap-{density}/ic_launcher_foreground.webp'), n * 108 // 48, n * 108 // 48)
        rasterise(icon(0.6, ANDROID, 'rounded'), at(f'{res}/mipmap-{density}/ic_launcher.webp'), n, n)
        rasterise(icon(0.56, ANDROID, 'circle'), at(f'{res}/mipmap-{density}/ic_launcher_round.webp'), n, n)


def main():
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    parser.add_argument('--out', default=REPO, help='repository root to write into (default: this repository)')
    out = parser.parse_args().out
    brand = os.path.join(out, 'assets', 'brand')
    os.makedirs(brand, exist_ok=True)
    for name, text in brand_svgs().items():
        path = os.path.join(brand, name)
        with open(path, 'w') as f:
            f.write(text)
        # PNGs beside the SVGs: 1024px square marks and tiles, 512px tall lockups.
        wide = name.startswith(('legato-lockup-', 'legato-wordmark'))
        rasterise(text, path[:-4] + '.png', *((None, 512) if wide else (1024, 1024)))
    app_icons(out)


if __name__ == '__main__':
    main()
