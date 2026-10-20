import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { extract } from './extract.mjs';

const fixture = () => JSON.parse(readFileSync(new URL('./testdata/lhr.json', import.meta.url), 'utf8'));

test('returns the three metrics from the fixture', () => {
  const lhr = fixture();
  const a = lhr.audits;
  assert.deepEqual(extract(lhr), {
    lcpMs: a['largest-contentful-paint'].numericValue,
    tbtMs: a['total-blocking-time'].numericValue,
    cls: a['cumulative-layout-shift'].numericValue,
  });
});

test('throws when an audit is missing', () => {
  for (const id of ['largest-contentful-paint', 'total-blocking-time', 'cumulative-layout-shift']) {
    const lhr = fixture();
    delete lhr.audits[id];
    assert.throws(() => extract(lhr), new RegExp(id));
  }
  assert.throws(() => extract({}), /audit/);
});

test('throws on negative, non-finite or non-numeric values', () => {
  for (const bad of [-1, NaN, Infinity, '100', null, undefined]) {
    const lhr = fixture();
    lhr.audits['total-blocking-time'].numericValue = bad;
    assert.throws(() => extract(lhr), /total-blocking-time/, String(bad));
  }
});
