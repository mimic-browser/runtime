#!/usr/bin/env bash
# Copyright (c) the go-webengine/engine authors.
# SPDX-License-Identifier: BSD-3-Clause
#
# Ratchet coverage gate for the pure-logic packages.
#
# Only css/layout/paint/dom/paginate are gated: they are font-free /
# deterministic pure logic (cascade + inheritance, the line-breaker, box
# metrics, the DOM tree, the page cutter) and are fully unit-testable offline. The root `engine` package and `cmd/render`
# are deliberately excluded because they perform live-network I/O (HTTP fetch of
# pages and image sub-resources) whose coverage is not deterministic in CI.
#
# The floors below are the Phase-0 *measured* per-package coverage. They are a
# ratchet: CI fails if a package drops below its floor, and the floor should be
# raised (never lowered) as coverage improves toward 100%. They are not a hard
# 100% because Phase-0 is not yet fully covered; raise each toward 100% as the
# engine matures.
set -euo pipefail

# package -> minimum acceptable statement coverage (percent)
PKGS=(css layout paint dom paginate)
declare -A FLOOR=(
  [css]=99.5
  [layout]=100.0
  [paint]=100.0
  [dom]=97.4
  [paginate]=100.0
)

fail=0
for pkg in "${PKGS[@]}"; do
  floor="${FLOOR[$pkg]}"
  out=$(CGO_ENABLED=0 go test "./${pkg}/..." -cover 2>&1)
  echo "$out"
  cov=$(printf '%s\n' "$out" | grep -oE 'coverage: [0-9.]+% of statements' | grep -oE '[0-9.]+' | head -1)
  if [ -z "${cov:-}" ]; then
    echo "::error::${pkg}: could not parse coverage"
    fail=1
    continue
  fi
  # Float comparison via awk: cov must be >= floor.
  if awk -v c="$cov" -v f="$floor" 'BEGIN{exit !(c+0 >= f-0)}'; then
    echo "OK  ${pkg}: ${cov}% >= floor ${floor}%"
  else
    echo "::error::${pkg}: coverage ${cov}% is below floor ${floor}%"
    fail=1
  fi
done

if [ "$fail" -ne 0 ]; then
  echo "coverage gate FAILED"
  exit 1
fi
echo "coverage gate PASSED"
