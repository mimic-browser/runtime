#!/usr/bin/env python3
"""Side-by-side + pixel-diff of go-webengine vs Chromium for one HTML page.

This is the visual-fidelity protocol: render the SAME local page at the SAME
viewport width and device scale in both engines, then compare. It is a local
developer tool (it shells out to Chrome), NOT a CI gate — CI has no browser.

Usage:
    compare.py OURS.png CHROME.png OUT_DIR NAME

Writes OUT_DIR/NAME.sidebyside.png (labelled OURS | CHROMIUM) and prints a
mean-absolute per-pixel difference in [0,1] (0 = identical) over the overlap.
The score is a coarse guide — fonts differ between engines, so it is never 0 —
useful for tracking regressions/improvements run to run.
"""
import sys
from PIL import Image, ImageChops, ImageDraw


def main() -> int:
    ours_p, chrome_p, out_dir, name = sys.argv[1:5]
    ours = Image.open(ours_p).convert("RGB")
    chrome = Image.open(chrome_p).convert("RGB")

    # Mean absolute difference over the common region (coarse fidelity metric).
    w = min(ours.width, chrome.width)
    h = min(ours.height, chrome.height)
    diff = ImageChops.difference(ours.crop((0, 0, w, h)), chrome.crop((0, 0, w, h)))
    total = sum(sum(px) for px in diff.getdata())
    mad = total / (w * h * 3 * 255) if w and h else 0.0

    gap = 24
    W = ours.width + chrome.width + gap
    H = max(ours.height, chrome.height) + 28
    board = Image.new("RGB", (W, H), (235, 235, 235))
    d = ImageDraw.Draw(board)
    d.text((8, 8), "OURS (go-webengine)", fill=(0, 0, 0))
    d.text((ours.width + gap + 8, 8), "CHROMIUM (reference)", fill=(0, 0, 0))
    board.paste(ours, (0, 28))
    board.paste(chrome, (ours.width + gap, 28))
    board.save(f"{out_dir}/{name}.sidebyside.png")
    print(f"{name}: MAD={mad:.4f} (0=identical) -> {out_dir}/{name}.sidebyside.png")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
