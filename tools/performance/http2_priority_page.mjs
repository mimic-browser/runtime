import { execFileSync } from 'node:child_process';
import { resolve } from 'node:path';
import { createHash } from 'node:crypto';
import { mkdir, readFile, writeFile } from 'node:fs/promises';
const clientDirectory = resolve(process.env.MIMIC_PRIORITY_CLIENT_DIR || process.cwd());
// Resolve using the client's ESM import condition, as the unchanged SDK workload does.
const sdkURL = execFileSync(
  process.execPath,
  [
    '--input-type=module',
    '-e',
    "process.stdout.write(import.meta.resolve('mimic-browser/playwright'))",
  ],
  { cwd: clientDirectory, encoding: 'utf8', windowsHide: true },
);
const { launch } = await import(sdkURL);
const directory = process.env.MIMIC_PRIORITY_OUTPUT;
const binaries = {
  control: process.env.MIMIC_PRIORITY_CONTROL,
  candidate: process.env.MIMIC_PRIORITY_CANDIDATE,
};
if (!directory || !binaries.control || !binaries.candidate)
  throw new Error('Set MIMIC_PRIORITY_OUTPUT, MIMIC_PRIORITY_CONTROL and MIMIC_PRIORITY_CANDIDATE');
await mkdir(directory, { recursive: true });
const rounds = Number(process.env.MIMIC_PRIORITY_ROUNDS || 5);
if (!Number.isSafeInteger(rounds) || rounds < 1)
  throw new Error('MIMIC_PRIORITY_ROUNDS must be a positive integer');
const binaryHashes = Object.fromEntries(
  await Promise.all(
    Object.entries(binaries).map(async ([label, file]) => [
      label,
      createHash('sha256')
        .update(await readFile(file))
        .digest('hex'),
    ]),
  ),
);
const rows = [];
for (let round = 0; round < rounds; round++) {
  const labels = round % 2 ? ['candidate', 'control'] : ['control', 'candidate'];
  for (const label of labels) {
    const browser = await launch({ executablePath: binaries[label] });
    try {
      const context = await browser.newContext({ ignoreHTTPSErrors: true });
      const page = await context.newPage();
      const session = await context.newCDPSession(page);
      await session.send('Security.setIgnoreCertificateErrors', { ignore: true });
      const errors = [];
      page.on('pageerror', (error) => errors.push(String(error)));
      let requested = 0;
      let completed = 0;
      page.on('request', () => requested++);
      page.on('requestfinished', () => completed++);
      const begin = performance.now();
      await page.goto(process.argv[2], { waitUntil: 'domcontentloaded', timeout: 30000 });
      const navigationMs = performance.now() - begin;
      const result = await page.evaluate(() => ({
        answer: window.answer,
        margin: getComputedStyle(document.getElementById('probe')).marginLeft,
      }));
      await page.waitForFunction(() => window.asyncDone === 16, null, { timeout: 30000 });
      await page.waitForLoadState('load');
      const totalMs = performance.now() - begin;
      const row = { round, label, navigationMs, totalMs, result, requested, completed, errors };
      rows.push(row);
      console.log(JSON.stringify(row));
      await writeFile(
        `${directory}/http2-priority-page.json`,
        JSON.stringify(
          {
            classification:
              'network-modified local HTTP/2 scheduling diagnostic: self-signed certificate accepted through CDP; no resource blocking',
            binaryHashes,
            rows,
          },
          null,
          2,
        ),
      );
      if (
        result.answer !== 'ready' ||
        result.margin !== '7px' ||
        errors.length ||
        requested !== 19 ||
        completed !== 19
      )
        throw new Error('Incorrect complete page result');
      await context.close();
    } finally {
      await browser.close();
    }
  }
}
