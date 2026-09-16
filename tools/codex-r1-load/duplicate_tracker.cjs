"use strict";

// Exact per-run duplicate tracker for the synthetic B8 request-id format.
// IDs are <run-prefix>-<baseline|candidate>-<worker|smoke>-<sequence>.
// A bitset preserves exact duplicate detection without retaining every full ID.
class DuplicateTracker {
  constructor(maxSequence = 5_000_000) {
    this.maxSequence = maxSequence;
    this.runPrefix = null;
    this.bits = new Map();
    this.bytes = 0;
  }

  mark(rid) {
    const m = /^(.*)-(baseline|candidate)-(smoke|\d+)-(\d+)$/.exec(String(rid));
    if (!m || !m[1]) return { valid: false, duplicate: false, reason: "bad_request_id" };
    const prefix = m[1], variant = m[2], worker = m[3], sequence = Number(m[4]);
    if (!Number.isSafeInteger(sequence) || sequence < 0 || sequence > this.maxSequence)
      return { valid: false, duplicate: false, reason: "bad_sequence" };
    if (worker !== "smoke") {
      const workerNumber = Number(worker);
      if (!Number.isSafeInteger(workerNumber) || workerNumber < 0 || workerNumber > 255)
        return { valid: false, duplicate: false, reason: "bad_worker" };
    }
    if (this.runPrefix === null) this.runPrefix = prefix;
    if (prefix !== this.runPrefix)
      return { valid: false, duplicate: false, reason: "mixed_run_prefix" };

    const key = variant + ":" + worker;
    const byteIndex = sequence >>> 3;
    let bitmap = this.bits.get(key);
    if (!bitmap || byteIndex >= bitmap.length) {
      const oldLength = bitmap ? bitmap.length : 0;
      let nextLength = Math.max(byteIndex + 1, oldLength ? oldLength * 2 : 64);
      const maxLength = (this.maxSequence >>> 3) + 1;
      if (nextLength > maxLength) nextLength = maxLength;
      const replacement = Buffer.alloc(nextLength);
      if (bitmap) bitmap.copy(replacement);
      bitmap = replacement;
      this.bits.set(key, bitmap);
      this.bytes += nextLength - oldLength;
    }
    const mask = 1 << (sequence & 7);
    const duplicate = (bitmap[byteIndex] & mask) !== 0;
    bitmap[byteIndex] |= mask;
    return { valid: true, duplicate };
  }

  snapshot() {
    return { run_prefix: this.runPrefix, keys: this.bits.size, bytes: this.bytes, max_sequence: this.maxSequence };
  }
}

module.exports = { DuplicateTracker };

if (require.main === module) {
  const t = new DuplicateTracker();
  const prefix = "sub2api-r1-b8-tracker-selftest";
  let unique = 0;
  for (const variant of ["baseline", "candidate"]) {
    for (let worker = 0; worker < 4; worker++) {
      for (let n = 0; n < 50_000; n++) {
        const r = t.mark(`${prefix}-${variant}-${worker}-${n}`);
        if (!r.valid || r.duplicate) throw new Error("unique ID rejected");
        unique++;
      }
    }
    for (let n = 0; n < 10; n++) {
      const r = t.mark(`${prefix}-${variant}-smoke-${n}`);
      if (!r.valid || r.duplicate) throw new Error("smoke ID rejected");
      unique++;
    }
  }
  const duplicate = t.mark(`${prefix}-candidate-3-4242`);
  if (!duplicate.valid || !duplicate.duplicate) throw new Error("duplicate missed");
  if (t.mark(`other-run-candidate-3-4242`).valid) throw new Error("mixed run accepted");
  const memory = process.memoryUsage();
  console.log(JSON.stringify({ passed: true, unique, tracker: t.snapshot(), heap_used: memory.heapUsed, rss: memory.rss }));
}
