import assert from 'node:assert/strict';
import test from 'node:test';
import { normalizeName } from './normalize.mjs';

test('lowercases names', () => {
  assert.equal(normalizeName('ALIce'), 'alice');
});
