const shapes = [
  [
    [1, 1],
    [31, 1],
    [31, 23],
    [1, 23],
  ],
  [
    [1.25, 1.125],
    [31.75, 1.5],
    [29.25, 23.75],
    [2.5, 22.25],
  ],
  [
    [1, 1],
    [31, 1],
    [16, 12],
    [31, 23],
    [1, 23],
  ],
];
for (let i = 0; i < 12; i++) {
  shapes.push(
    Array.from({ length: 16 }, (_, j) => [
      16 + (12 + i / 17) * Math.cos((j * Math.PI) / 8 + i / 19),
      12 + (8 + i / 23) * Math.sin((j * Math.PI) / 8 + i / 19),
    ]),
  );
}
for (const points of shapes)
  for (const paint of [
    [17, 113, 231, 255],
    [12.5, 128.5, 300, 255],
    [17, 113, 231, 128],
    [12.5, 128.5, 300, 0],
  ]) {
    for (const kind of ['opaque', 'alpha', 'clip', 'stroke', 'float']) {
      const path = [
        { points, closed: true },
        { points: [points[0]], closed: false },
      ];
      const op = {
        path,
        rule: 'nonzero',
        paint,
        alpha: kind === 'alpha' ? 0.5 : 1,
        clips:
          kind === 'clip'
            ? [
                {
                  path: [
                    {
                      points: [
                        [5, 3],
                        [26, 3],
                        [26, 20],
                        [5, 20],
                      ],
                      closed: true,
                    },
                  ],
                  rule: 'nonzero',
                },
              ]
            : [],
        stroke: kind === 'stroke',
        width: 2.5,
        clear: false,
        shadow: null,
        composite: 'source-over',
      };
      const pixels =
        kind === 'float' ? new Float32Array(32 * 24 * 4) : new Uint8ClampedArray(32 * 24 * 4);
      for (let i = 0; i < pixels.length; i++) pixels[i] = i % 251;
      const reference = pixels.slice();
      materializePath({ width: 32, height: 24, pixels, opaque: false }, op);
      for (let y = 0; y < 24; y++)
        for (let x = 0; x < 32; x++) {
          const coverage = pathPixelCoverage(op, x, y);
          if (coverage)
            compositePixel(
              reference,
              (y * 32 + x) * 4,
              paint,
              op.alpha,
              'source-over',
              false,
              coverage,
            );
        }
      for (let i = 0; i < pixels.length; i++)
        if (pixels[i] !== reference[i])
          return JSON.stringify({ kind, i, actual: pixels[i], expected: reference[i] });
    }
  }
return 'ok';
