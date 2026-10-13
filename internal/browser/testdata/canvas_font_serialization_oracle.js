(() => {
  const ctx = document.createElement('canvas').getContext('2d');
  const rows = [
    ['16px Arial', '16px Arial'],
    ["16px 'Segoe UI Emoji'", '16px "Segoe UI Emoji"'],
    ["16px 'Segoe UI Emoji','Arial',sans-serif", '16px "Segoe UI Emoji", Arial, sans-serif'],
    [
      "16px 'Segoe Fluent Icons','Ink Free','Bahnschrift','Segoe MDL2 Assets','HoloLens MDL2 Assets','Leelawadee UI','Javanese Text','Segoe UI Emoji',Aldhabi,Gadugi",
      '16px "Segoe Fluent Icons", "Ink Free", Bahnschrift, "Segoe MDL2 Assets", "HoloLens MDL2 Assets", "Leelawadee UI", "Javanese Text", "Segoe UI Emoji", Aldhabi, Gadugi',
    ],
    ['bold 15px "Times New Roman", serif', 'bold 15px "Times New Roman", serif'],
    ['italic 12.5px Arial,sans-serif', 'italic 12.5px Arial, sans-serif'],
    ['16px "a,b",serif', '16px "a,b", serif'],
    ['16px Times New Roman', '16px "Times New Roman"'],
    ['16px   Arial  ,  serif', '16px Arial, serif'],
    ['16px "Arial"', '16px Arial'],
    ['16px Arial Unicode MS,monospace', '16px "Arial Unicode MS", monospace'],
    ['bold italic 13px "A B", serif', 'italic bold 13px "A B", serif'],
    ['16px "serif"', '16px "serif"'],
    ['16px "A\\26 B",serif', '16px "A&B", serif'],
  ];
  const mismatches = [];
  for (const [input, expected] of rows) {
    ctx.font = input;
    if (ctx.font !== expected) mismatches.push({ input, expected, actual: ctx.font });
  }
  const before = ctx.font;
  ctx.font = 'invalid';
  if (ctx.font !== before) mismatches.push({ input: 'invalid', expected: before, actual: ctx.font });
  return mismatches;
})()
