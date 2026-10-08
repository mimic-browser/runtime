import { spawn } from 'node:child_process';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { once } from 'node:events';
import { startFixture } from './fixture.mjs';

const binary = process.argv[2];
if (!binary) throw new Error('Usage: npm run verify -- /absolute/path/to/mimic');
const here = dirname(fileURLToPath(import.meta.url));
const cache = await mkdtemp(resolve(tmpdir(), 'mimic-examples-'));
const fixture = await startFixture(0);
let runtime;
let exited;
let log = '';
try {
  runtime = spawn(resolve(binary), ['--browser-mode', 'headless', '--listen', '127.0.0.1:0'], {
    cwd: cache,
    env: {
      ...process.env,
      XDG_CACHE_HOME: cache,
      LOCALAPPDATA: cache,
      GOV8_SHIM_LIBRARY: '',
      GOV8_SHIM_DLL: '',
    },
    windowsHide: true,
    stdio: ['ignore', 'pipe', 'pipe'],
  });
  exited = once(runtime, 'exit');
  const endpoint = await new Promise((resolveReady, reject) => {
    const timer = setTimeout(() => reject(new Error(`Startup timeout: ${log}`)), 30000);
    runtime.once('error', (error) => {
      clearTimeout(timer);
      reject(error);
    });
    runtime.once('exit', (code) => {
      clearTimeout(timer);
      reject(new Error(`Runtime exited ${code}: ${log}`));
    });
    runtime.stderr.on('data', (chunk) => {
      log += chunk;
    });
    runtime.stdout.on('data', (chunk) => {
      log += chunk;
      const match = log.match(/Mimic listening on (http:\/\/127\.0\.0\.1:\d+)/);
      if (match) {
        clearTimeout(timer);
        resolveReady(match[1]);
      }
    });
  });
  for (const example of [
    'puppeteer.mjs',
    'playwright.mjs',
    'concurrency.mjs',
    'profile-contexts.mjs',
  ]) {
    const child = spawn(process.execPath, [resolve(here, example)], {
      env: { ...process.env, MIMIC_URL: endpoint, TARGET_URL: fixture.url },
      windowsHide: true,
      stdio: 'inherit',
    });
    const timer = setTimeout(() => child.kill(), 60000);
    try {
      const [code] = await once(child, 'exit');
      if (code !== 0) throw new Error(`${example} failed with exit ${code}`);
    } finally {
      clearTimeout(timer);
    }
  }
  console.log(`PASS ${process.platform}/${process.arch}: all public examples, fresh native cache`);
} finally {
  if (runtime && runtime.exitCode === null && runtime.signalCode === null) {
    runtime.kill('SIGTERM');
    await exited;
  }
  await fixture.close();
  await rm(cache, { recursive: true, force: true });
}
