const AUDITS = {
  lcpMs: 'largest-contentful-paint',
  tbtMs: 'total-blocking-time',
  cls: 'cumulative-layout-shift',
};

// Returns { lcpMs, tbtMs, cls } from a Lighthouse result; throws if any is missing or invalid.
export function extract(lhr) {
  const out = {};
  for (const [key, id] of Object.entries(AUDITS)) {
    const v = lhr?.audits?.[id]?.numericValue;
    if (typeof v !== 'number' || !Number.isFinite(v) || v < 0) {
      throw new Error(`audit ${id}: missing or invalid numericValue (${JSON.stringify(v)})`);
    }
    out[key] = v;
  }
  return out;
}
