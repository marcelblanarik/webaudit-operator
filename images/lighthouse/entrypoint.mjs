import { spawnSync } from 'node:child_process';
import { readFileSync, writeFileSync, existsSync } from 'node:fs';
import { extract } from './extract.mjs';

const TERMINATION_LOG = '/dev/termination-log';
const OUT = '/tmp/lhr.json';

function fail(msg) {
  console.error(`audit failed: ${msg}`);
  process.exit(1);
}

const url = process.env.AUDIT_URL ?? '';
const formFactor = process.env.AUDIT_FORM_FACTOR ?? '';
if (!/^https?:\/\//.test(url)) fail('AUDIT_URL must start with http:// or https://');
if (formFactor !== 'mobile' && formFactor !== 'desktop') fail('AUDIT_FORM_FACTOR must be mobile or desktop');

const args = [
  url,
  '--output=json',
  `--output-path=${OUT}`,
  '--quiet',
  '--chrome-flags=--headless --no-sandbox --disable-dev-shm-usage',
];
if (formFactor === 'desktop') args.push('--preset=desktop');

// args array, no shell: the URL is never interpreted by a shell.
const res = spawnSync('lighthouse', args, { stdio: ['ignore', 'ignore', 'inherit'] });
if (res.error || res.status !== 0) fail(`lighthouse failed: ${res.error?.message ?? `exit status ${res.status}`}`);

let summary;
try {
  summary = extract(JSON.parse(readFileSync(OUT, 'utf8')));
} catch (e) {
  fail(e.message);
}

const json = JSON.stringify(summary);
console.log(json);
if (existsSync(TERMINATION_LOG)) {
  try {
    writeFileSync(TERMINATION_LOG, json);
  } catch (e) {
    fail(`cannot write ${TERMINATION_LOG}: ${e.message}`);
  }
}
