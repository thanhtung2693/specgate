import assert from 'node:assert/strict';
import test from 'node:test';
import { normalizeName } from './normalize.mjs';

test('trims surrounding whitespace', () => {
  assert.equal(normalizeName('  alice  '), 'alice');
});
