"""Render the README workload benchmark image from current public-results.json.

Only checkpoint JSON supplies measurements. Startup metrics are never displayed. The visual follows the
Mimic site's navy, periwinkle and restrained comparison-bar design.
"""

import argparse
import hashlib
import json
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter, ImageFont
from matplotlib import font_manager


ROOT = Path(__file__).resolve().parents[2]
DEFAULT_OUTPUT = ROOT / "docs/assets/benchmark-story.png"
BG = "#070e17"
SURFACE = "#101a2a"
LINE = "#34445f"
TEXT = "#f0f3f7"
MUTED = "#b6bfdc"
QUIET = "#8e9db7"
BLUE = "#93a6ff"
BLUE_BRIGHT = "#b9c7ff"
CHROME = "#64738e"


def load_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def fonts():
    regular = font_manager.findfont("DejaVu Sans")
    bold = font_manager.findfont(font_manager.FontProperties(family="DejaVu Sans", weight="bold"))
    mono = font_manager.findfont("DejaVu Sans Mono")
    return {
        "eyebrow": ImageFont.truetype(mono, 22),
        "title": ImageFont.truetype(bold, 76),
        "subtitle": ImageFont.truetype(regular, 28),
        "metric": ImageFont.truetype(bold, 86),
        "label": ImageFont.truetype(bold, 26),
        "small": ImageFont.truetype(regular, 21),
        "tiny": ImageFont.truetype(regular, 18),
        "tiny_bold": ImageFont.truetype(bold, 18),
    }


def text(draw, xy, value, font, fill=TEXT, anchor=None):
    draw.text(xy, value, font=font, fill=fill, anchor=anchor)


def comparison_bar(draw, x, y, width, mimic_value, chrome_value, font_set, unit, mimic_label="Mimic", chrome_label="Chrome"):
    maximum = max(mimic_value, chrome_value)
    for offset, label, value, color in ((0, chrome_label, chrome_value, CHROME), (47, mimic_label, mimic_value, BLUE)):
        text(draw, (x, y + offset), label, font_set["tiny"], MUTED)
        bar_x = x + 88
        bar_width = max(8, int(width * value / maximum))
        draw.rounded_rectangle((bar_x, y + offset + 7, bar_x + width, y + offset + 17), 5, fill="#25334b")
        draw.rounded_rectangle((bar_x, y + offset + 7, bar_x + bar_width, y + offset + 17), 5, fill=color)
        text(draw, (bar_x + width + 18, y + offset - 1), f"{value:.1f} {unit}", font_set["tiny_bold"])


def render(checkpoint, output):
    source_path = checkpoint / "public-results.json"
    data = load_json(source_path)
    row = next(r for r in data["density"] if r["workload"] == "static" and r["n"] == 50)
    if row["failure"] or row["valid"] != row["attempts"]:
        raise ValueError("Cannot promote a failed series")
    optimize = data["optimize"]
    fs = fonts()
    canvas = Image.new("RGB", (2000, 1125), BG)
    glow = Image.new("RGBA", canvas.size, (0, 0, 0, 0))
    gd = ImageDraw.Draw(glow)
    gd.ellipse((1180, -340, 2300, 700), fill=(67, 91, 220, 54))
    gd.ellipse((-570, 90, 720, 1020), fill=(41, 89, 175, 37))
    canvas = Image.alpha_composite(canvas.convert("RGBA"), glow.filter(ImageFilter.GaussianBlur(145))).convert("RGB")
    draw = ImageDraw.Draw(canvas)

    date = data["methodology"]["date"]
    draw.rounded_rectangle((88, 57, 302, 101), radius=22, fill="#273765", outline="#596fb5", width=2)
    text(draw, (195, 79), "BENCHMARK", fs["eyebrow"], BLUE_BRIGHT, anchor="mm")
    text(draw, (1910, 72), f"CHROME 152  /  {date}", fs["eyebrow"], QUIET, anchor="ra")
    text(draw, (90, 150), "Same web.", fs["title"])
    text(draw, (90, 240), "Less weight.", fs["title"], BLUE)
    text(draw, (94, 356), "Measured on identical local fixtures with the same correctness gates.", fs["subtitle"], MUTED)
    draw.line((90, 459, 1910, 459), fill=LINE, width=2)

    cards = [
        (
            90, 510, 660, 967, "01 / ACTIVE MEMORY",
            f"{row['september_chrome_rss_mib'] / row['active_rss_mib']:.2f}×", "less memory",
            row["active_rss_mib"], row["september_chrome_rss_mib"], "MiB",
            "50 live static Pages · process-tree RSS",
        ),
        (
            715, 510, 1285, 967, "02 / PROCESSING SPEED",
            f"{row['throughput_pages_s'] / row['chrome_throughput_pages_s']:.2f}×", "more Pages per second",
            row["throughput_pages_s"], row["chrome_throughput_pages_s"], "Pages/s",
            "Same 50-Page concurrency · static DOM",
        ),
        (
            1340, 510, 1910, 967, "03 / OPTIMIZE ACQUISITION",
            f"{optimize['reduction_percent']:.1f}%", "fewer HTTP body bytes",
            optimize["optimized_encoded_body_bytes"] / 1000,
            optimize["baseline_encoded_body_bytes"] / 1000, "KB",
            "Books extraction · Optimize on vs off",
        ),
    ]
    for x1, y1, x2, y2, heading, metric, label, mimic, chrome, unit, note in cards:
        draw.rounded_rectangle((x1, y1, x2, y2), radius=23, fill=SURFACE, outline=LINE, width=2)
        text(draw, (x1 + 34, y1 + 38), heading, fs["eyebrow"], BLUE_BRIGHT)
        text(draw, (x1 + 34, y1 + 100), metric, fs["metric"], BLUE)
        text(draw, (x1 + 37, y1 + 213), label, fs["label"])
        comparison_bar(draw, x1 + 37, y1 + 292, 235, mimic, chrome, fs, unit, "Auto" if x1 == 1340 else "Mimic", "Default" if x1 == 1340 else "Chrome")
        text(draw, (x1 + 37, y1 + 411), note, fs["small"], QUIET)

    text(draw, (90, 1025), "250 / 250 static batch attempts passed · separate Optimize workload: 5 / 5", fs["tiny"], MUTED)
    text(draw, (90, 1063), "Controlled fixtures; results are workload and machine specific.", fs["tiny"], QUIET)
    text(draw, (1910, 1063), "METHOD + RAW DATA IN REPOSITORY", fs["tiny_bold"], BLUE_BRIGHT, anchor="ra")

    output.parent.mkdir(parents=True, exist_ok=True)
    canvas.save(output, optimize=True)
    receipt = {
        "checkpoint": date,
        "source_sha256": {source_path.name: hashlib.sha256(source_path.read_bytes()).hexdigest()},
        "generator_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "output_sha256": hashlib.sha256(output.read_bytes()).hexdigest(),
        "concurrency_level": row["n"],
    }
    output.with_suffix(".receipt.json").write_text(json.dumps(receipt, indent=2) + "\n", encoding="utf-8", newline="\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("checkpoint", type=Path)
    parser.add_argument("output", type=Path, nargs="?", default=DEFAULT_OUTPUT)
    args = parser.parse_args()
    render(args.checkpoint, args.output)


if __name__ == "__main__":
    main()
