const sub = (points, closed = true) => ({ points, closed });
const rect = [
  [1, 1],
  [13, 1],
  [13, 13],
  [1, 13],
];
const circle = Array.from({ length: 129 }, (_, i) => [
  7 + 5.25 * Math.cos((i * Math.PI) / 64),
  7 + 5.25 * Math.sin((i * Math.PI) / 64),
]);
const star = Array.from({ length: 5 }, (_, i) => [
  7 + 6 * Math.cos((i * 4 * Math.PI) / 5),
  7 + 6 * Math.sin((i * 4 * Math.PI) / 5),
]);
const cases = [
  { path: [sub(rect)], area: true },
  {
    path: [
      sub([
        [1, 1],
        [1, 1],
        [13, 1],
        [13, 13],
        [1, 13],
        [1, 1],
      ]),
      sub([[1, 1]]),
    ],
    area: true,
  },
  { path: [sub(circle), sub([circle[0]])], area: true },
  { path: [sub(star)], area: true },
  { path: [sub(rect)], clips: [{ path: [sub(circle), sub([circle[0]])], rule: 'nonzero' }] },
  {
    path: [
      sub(rect),
      sub([
        [4, 4],
        [10, 4],
        [10, 10],
        [4, 10],
      ]),
    ],
    rule: 'evenodd',
  },
  {
    path: [
      sub([
        [1, 1],
        [13, 1],
        [7, 7],
        [13, 13],
        [1, 13],
      ]),
    ],
  },
  { path: [sub(circle)], stroke: true, width: 1.5 },
  {
    path: [sub([[2, 12], [7, 2], [12, 12]], false)],
    stroke: true,
    width: 4,
    lineJoin: 'miter',
    miterLimit: 10,
  },
  {
    path: [sub([[2, 12], [7, 2], [12, 12]], false)],
    stroke: true,
    width: 4,
    lineJoin: 'round',
    miterLimit: 10,
  },
  {
    path: [
      sub(
        [
          [1, 1],
          [1, 1],
          [13, 13],
        ],
        false,
      ),
    ],
    stroke: true,
    width: 3,
  },
  {
    path: [
      sub(
        [
          [-1e10, 7],
          [1e10, 7],
        ],
        false,
      ),
    ],
    stroke: true,
    width: 1,
  },
];
for (let index = 0; index < cases.length; index++) {
  const item = cases[index];
  const op = { clips: [], rule: 'nonzero', stroke: false, width: 1, ...item };
  for (let y = 0; y < 15; y++)
    for (let x = 0; x < 15; x++) {
      let expected;
      if (item.area) expected = Math.min(1, Math.abs(polygonPixelArea(item.path[0].points, x, y)));
      else {
        let count = 0;
        for (let yy = 0; yy < 8; yy++)
          for (let xx = 0; xx < 8; xx++) {
            const px = x + (xx + 0.5) / 8,
              py = y + (yy + 0.5) / 8;
            if (
              op.clips.every((c) => containsPath(c.path, px, py, c.rule)) &&
              (op.stroke
                ? strokeContains(
                    op.path,
                    px,
                    py,
                    op.width,
                    op.lineCap,
                    op.lineJoin,
                    op.miterLimit,
                  )
                : containsPath(op.path, px, py, op.rule))
            )
              count++;
          }
        expected = count / 64;
      }
      const actual = pathPixelCoverage(op, x, y);
      if (actual !== expected) return JSON.stringify({ index, x, y, expected, actual });
    }
}
// Mutable hit testing must observe appended points, independently of snapshot caches.
const mutable = [sub(rect.map((p) => p.slice()))];
if (!containsPath(mutable, 7, 7, 'nonzero')) return 'mutable initial';
mutable[0].points = [
  [0, 0],
  [1, 0],
  [1, 1],
  [0, 1],
];
if (containsPath(mutable, 7, 7, 'nonzero')) return 'mutable update';
return 'ok';
