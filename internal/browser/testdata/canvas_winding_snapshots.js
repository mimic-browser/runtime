const paths = [
  [],
  [{ points: [], closed: false }],
  [{ points: [[0, 0]], closed: false }],
  [
    {
      points: [
        [0, 0],
        [8, 8],
        [0, 8],
        [8, 0],
      ],
      closed: true,
    },
  ],
  [
    {
      points: [
        [0, 0],
        [10, 0],
        [10, 10],
        [0, 10],
      ],
      closed: false,
    },
    {
      points: [
        [2, 2],
        [8, 2],
        [8, 8],
        [2, 8],
      ],
      closed: true,
    },
  ],
  [
    {
      points: [
        [0.125, 0.25],
        [0.125, 0.25],
        [7.875, 2.125],
        [3.5, 9.75],
        [0.125, 0.25],
      ],
      closed: true,
    },
  ],
  [
    {
      points: [
        [-1e10, 0],
        [1e10, 0],
        [0, 8],
      ],
      closed: true,
    },
  ],
  [
    {
      points: [
        [NaN, 0],
        [8, Infinity],
        [0, 8],
      ],
      closed: true,
    },
  ],
];
for (let seed = 1; seed <= 16; seed++) {
  paths.push([
    {
      closed: true,
      points: Array.from({ length: 31 }, (_, i) => [
        5 + Math.cos(i * seed) * (2 + (i % 5)),
        5 + Math.sin(i * seed) * (2 + (i % 7)),
      ]),
    },
  ]);
}
for (const path of paths) {
  for (const rule of ['nonzero', 'evenodd']) {
    const snapshot = { path: pathCopy(path), rule };
    const points = path.flatMap((sub) => sub.points);
    points.push([NaN, 1], [Infinity, 1], [1, Infinity], [1, -Infinity]);
    for (let y = -1; y <= 11; y += 0.25) for (let x = -1; x <= 11; x += 0.25) points.push([x, y]);
    for (const [x, y] of points)
      for (const dx of [0, -1e-12, 1e-12])
        for (const dy of [0, -1e-12, 1e-12]) {
          const expected = containsPath(path, x + dx, y + dy, rule);
          const actual = containsWindingSnapshot(snapshot, x + dx, y + dy);
          if (actual !== expected)
            return JSON.stringify({ path, rule, x, y, dx, dy, expected, actual });
        }
  }
}
const many = {
  rule: 'evenodd',
  path: Array.from({ length: 20 }, (_, i) => ({
    closed: true,
    points: [
      [i, 0],
      [i + 2, 0],
      [i + 2, 5000],
      [i, 5000],
    ],
  })),
};
for (let y = 0; y < 5000; y++) {
  const actual = containsWindingSnapshot(many, 0.5, y + 0.125);
  if (actual !== containsPath(many.path, 0.5, y + 0.125, many.rule)) return 'uncached row mismatch';
}
if (many.winding.rows.size > 4096 || many.winding.cachedEdges > 16384) return 'unbounded cache';
const mutable = [
  {
    points: [
      [0, 0],
      [8, 0],
      [8, 8],
      [0, 8],
    ],
    closed: true,
  },
];
const frozen = { path: pathCopy(mutable), rule: 'nonzero' };
containsWindingSnapshot(frozen, 4, 4);
mutable[0].points = [
  [0, 0],
  [1, 0],
  [1, 1],
  [0, 1],
];
if (containsPath(mutable, 4, 4, 'nonzero') || !containsWindingSnapshot(frozen, 4, 4))
  return 'snapshot alias';
return 'ok';
