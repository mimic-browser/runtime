"""Render the README benchmark image from a completed benchmark checkpoint.

Only checkpoint JSON supplies measurements. The visual follows the
Mimic site's navy, periwinkle and restrained comparison-bar design.
"""

import argparse
import hashlib
import json
from pathlib import Path
from statistics import median

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


def validate_checkpoint(directory):
    raw_path = directory / "raw.json"
    summary_path = directory / "summary.json"
    manifest_path = directory / "manifest.json"
    raw, summary, manifest = map(load_json, (raw_path, summary_path, manifest_path))
    if not raw.get("finished"):
        raise ValueError("benchmark checkpoint is incomplete")
    if raw["metadata"]["arguments"].get("smoke"):
        raise ValueError("smoke runs cannot produce the README benchmark image")
    for path in (raw_path, summary_path):
        expected = manifest["sha256"].get(path.name)
        actual = hashlib.sha256(path.read_bytes()).hexdigest()
        if expected != actual:
            raise ValueError(f"benchmark artifact hash mismatch: {path.name}")
    calibration = raw.get("startup_calibration", {})
    if not calibration.get("complete"):
        raise ValueError("startup calibration is incomplete")
    return raw, summary


def startup(raw, system, field):
    rows = [r for r in raw["startup_calibration"]["rows"] if r["system"] == system and not r["excluded"]]
    if len(rows) != 10:
        raise ValueError(f"expected 10 startup samples for {system}")
    if field == "rss_mib":
        return median(r["ready_memory"]["rss"] / 2**20 for r in rows)
    return median(r[field] for r in rows)


def concurrency(summary, system, workload="static"):
    rows = [r for r in summary["concurrency"] if r["system"] == system and r["workload"] == workload]
    valid = [r for r in rows if not r["stop"] and r["success_rate"] == 1 and r.get("waves", 0) > 0]
    return {r["n"]: r for r in valid}


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


def comparison_bar(draw, x, y, width, mimic_value, chrome_value, font_set, unit):
    maximum = max(mimic_value, chrome_value)
    for offset, label, value, color in ((0, "Chrome", chrome_value, CHROME), (47, "Mimic", mimic_value, BLUE)):
        text(draw, (x, y + offset), label, font_set["tiny"], MUTED)
        bar_x = x + 88
        bar_width = max(8, int(width * value / maximum))
        draw.rounded_rectangle((bar_x, y + offset + 7, bar_x + width, y + offset + 17), 5, fill="#25334b")
        draw.rounded_rectangle((bar_x, y + offset + 7, bar_x + bar_width, y + offset + 17), 5, fill=color)
        text(draw, (bar_x + width + 18, y + offset - 1), f"{value:.1f} {unit}", font_set["tiny_bold"])


def render(checkpoint, output):
    memory_path = checkpoint / "public-results.json"
    memory = load_json(memory_path) if memory_path.exists() else None
    if memory is None:
        raw, summary = validate_checkpoint(checkpoint)
    fs = fonts()
    canvas = Image.new("RGB", (2000, 1125), BG)
    glow = Image.new("RGBA", canvas.size, (0, 0, 0, 0))
    gd = ImageDraw.Draw(glow)
    gd.ellipse((1180, -340, 2300, 700), fill=(67, 91, 220, 54))
    gd.ellipse((-570, 90, 720, 1020), fill=(41, 89, 175, 37))
    canvas = Image.alpha_composite(canvas.convert("RGBA"), glow.filter(ImageFilter.GaussianBlur(145))).convert("RGB")
    draw = ImageDraw.Draw(canvas)

    if memory is not None:
        date = memory["methodology"]["date"]
        mimic_rss = memory["ready"]["final_mimic"]
        chrome_rss = memory["ready"]["september_chrome"]
        level = 50
        row = next(
            r for r in memory["density"]
            if r["workload"] == "static" and r["n"] == level
            and not r["failure"] and r["valid"] == r["attempts"]
        )
        mrow = {"rss_mib": row["active_rss_mib"]}
        crow = {"rss_mib": row["september_chrome_rss_mib"]}
    else:
        date = raw["metadata"]["date"][:10]
        mimic_ready = startup(raw, "mimic", "cdp_ready_ms")
        chrome_ready = startup(raw, "chrome", "cdp_ready_ms")
        mimic_rss = startup(raw, "mimic", "rss_mib")
        chrome_rss = startup(raw, "chrome", "rss_mib")
        mc, cc = concurrency(summary, "mimic"), concurrency(summary, "chrome")
        levels = sorted(set(mc) & set(cc))
        if not levels:
            raise ValueError("no common successful static concurrency level")
        level = max(n for n in levels if n <= 50) if any(n <= 50 for n in levels) else max(levels)
        mrow, crow = mc[level], cc[level]

    draw.rounded_rectangle((88, 57, 302, 101), radius=22, fill="#273765", outline="#596fb5", width=2)
    text(draw, (195, 79), "BENCHMARK", fs["eyebrow"], BLUE_BRIGHT, anchor="mm")
    text(draw, (1910, 72), f"CHROME 152  /  {date}", fs["eyebrow"], QUIET, anchor="ra")
    text(draw, (90, 150), "Same web.", fs["title"])
    text(draw, (90, 240), "Less weight.", fs["title"], BLUE)
    text(draw, (94, 356), "Measured on identical local fixtures with the same correctness gates.", fs["subtitle"], MUTED)
    draw.line((90, 459, 1910, 459), fill=LINE, width=2)

    cards = [
        (
            90, 510, 660, 967, "01 / START LIGHT",
            f"{chrome_rss / mimic_rss:.2f}×", "less ready RSS",
            mimic_rss, chrome_rss, "MiB",
            "Ready process-tree memory" if memory else f"CDP ready: {mimic_ready:.0f} vs {chrome_ready:.0f} ms",
        ),
        (
            715, 510, 1285, 967, f"02 / {level} STATIC PAGES",
            "" if memory else f"{mrow['throughput'] / crow['throughput']:.1f}×", "more throughput",
            0 if memory else mrow["throughput"], 0 if memory else crow["throughput"], "pages/s",
            "Completed static concurrency series",
        ),
        (
            1340, 510, 1910, 967, "03 / KEEP IT LIGHT",
            f"{crow['rss_mib'] / mrow['rss_mib']:.1f}×", "less active RSS",
            mrow["rss_mib"] / 1024, crow["rss_mib"] / 1024, "GiB",
            f"Measured with {level} live static Pages",
        ),
    ]
    if memory is not None:
        cards[1] = (
            715, 510, 1285, 967, "02 / 50 STATIC PAGES",
            f"{crow['rss_mib'] / mrow['rss_mib']:.2f}×", "less active RSS",
            mrow["rss_mib"], crow["rss_mib"], "MiB",
            f"{row['valid']} / {row['attempts']} measured attempts passed",
        )
        cards[2] = (
            1340, 510, 1910, 967, "03 / READY FOOTPRINT",
            f"{(1 - mimic_rss / chrome_rss) * 100:.0f}%", "less ready memory",
            mimic_rss, chrome_rss, "MiB",
            "Memory-only checkpoint",
        )
    for x1, y1, x2, y2, heading, metric, label, mimic, chrome, unit, note in cards:
        draw.rounded_rectangle((x1, y1, x2, y2), radius=23, fill=SURFACE, outline=LINE, width=2)
        text(draw, (x1 + 34, y1 + 38), heading, fs["eyebrow"], BLUE_BRIGHT)
        text(draw, (x1 + 34, y1 + 100), metric, fs["metric"], BLUE)
        text(draw, (x1 + 37, y1 + 213), label, fs["label"])
        comparison_bar(draw, x1 + 37, y1 + 292, 235, mimic, chrome, fs, unit)
        text(draw, (x1 + 37, y1 + 411), note, fs["small"], QUIET)

    text(draw, (90, 1025), f"{memory['methodology']['single_valid']} / {memory['methodology']['single_attempts']} single-page attempts passed · React-100 excluded" if memory else "10 fresh starts · 20 warm samples/workload · process-tree RSS", fs["tiny"], MUTED)
    text(draw, (90, 1063), "Controlled fixtures; results are workload and machine specific.", fs["tiny"], QUIET)
    text(draw, (1910, 1063), "METHOD + RAW DATA IN REPOSITORY", fs["tiny_bold"], BLUE_BRIGHT, anchor="ra")

    output.parent.mkdir(parents=True, exist_ok=True)
    canvas.save(output, optimize=True)
    receipt = {
        "checkpoint": date,
        "source_sha256": {
            path.name: hashlib.sha256(path.read_bytes()).hexdigest()
            for path in ([memory_path] if memory else [checkpoint / "raw.json", checkpoint / "summary.json"])
        },
        "generator_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
        "output_sha256": hashlib.sha256(output.read_bytes()).hexdigest(),
        "concurrency_level": level,
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
