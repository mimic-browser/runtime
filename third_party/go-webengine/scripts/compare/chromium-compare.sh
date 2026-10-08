#!/usr/bin/env bash
# Visual-fidelity protocol: render each page in scripts/compare/pages/ (plus any
# passed as args) with go-webengine AND with headless Chromium at the SAME
# viewport width + device scale 1, then emit a labelled side-by-side and a
# coarse pixel-difference score per page.
#
# This is a LOCAL developer tool — it requires a Chrome/Chromium binary and is
# not part of CI (CI has no browser). It is how the renderer's typography and
# layout are checked against a real browser (see FIDELITY.md).
#
# Usage:
#   scripts/compare/chromium-compare.sh [-w WIDTH] [-o OUTDIR] [page.html ...]
set -euo pipefail

WIDTH=640
OUTDIR="${TMPDIR:-/tmp}/webengine-compare"
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"

while getopts "w:o:" opt; do
  case "$opt" in
    w) WIDTH="$OPTARG" ;;
    o) OUTDIR="$OPTARG" ;;
    *) echo "usage: $0 [-w WIDTH] [-o OUTDIR] [page.html ...]" >&2; exit 2 ;;
  esac
done
shift $((OPTIND - 1))

# Locate a Chrome/Chromium binary.
CHROME=""
for c in \
  "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome" \
  "$(command -v google-chrome || true)" \
  "$(command -v chromium || true)" \
  "$(command -v chromium-browser || true)"; do
  [ -n "$c" ] && [ -x "$c" ] && { CHROME="$c"; break; }
done
if [ -z "$CHROME" ]; then
  echo "error: no Chrome/Chromium found (this tool needs one)" >&2
  exit 1
fi

PAGES=("$@")
if [ ${#PAGES[@]} -eq 0 ]; then
  for f in "$ROOT"/scripts/compare/pages/*.html; do PAGES+=("$f"); done
fi

mkdir -p "$OUTDIR"
RENDER="$OUTDIR/render"
( cd "$ROOT" && CGO_ENABLED=0 GOWORK=off go build -o "$RENDER" ./cmd/render )

for page in "${PAGES[@]}"; do
  name="$(basename "${page%.html}")"
  # Our renderer (height is auto-grown to content; 2000 is just a viewport min).
  "$RENDER" -file "$page" -w "$WIDTH" -h 2000 -out "$OUTDIR/$name.ours.png" >/dev/null
  # Chromium reference at the same width, device scale 1, white background.
  "$CHROME" --headless=new --disable-gpu --force-device-scale-factor=1 \
    --hide-scrollbars --window-size="$WIDTH,2000" --default-background-color=FFFFFFFF \
    --screenshot="$OUTDIR/$name.chrome.png" "file://$page" 2>/dev/null
  python3 "$ROOT/scripts/compare/compare.py" \
    "$OUTDIR/$name.ours.png" "$OUTDIR/$name.chrome.png" "$OUTDIR" "$name"
done

echo "side-by-side images in $OUTDIR"
