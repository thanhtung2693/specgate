import assert from 'node:assert/strict';
import test from 'node:test';
import { normalizeName } from './normalize.mjs';

test('rejects empty names after trimming', () => {
  assert.throws(() => normalizeName(''), /Name must not be empty/);
  assert.throws(() => normalizeName('   '), /Name must not be empty/);
});
