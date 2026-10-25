import assert from 'node:assert/strict';
import { cpSync, existsSync, mkdtempSync, readFileSync, writeFileSync, mkdirSync, readdirSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import test from 'node:test';

const script = fileURLToPath(new URL('./local-delivery-proof.mjs', import.meta.url));
const binary = fileURLToPath(new URL('../../bin/specgate', import.meta.url));
const scratch = () => mkdtempSync(path.join(tmpdir(), 'delivery-proof-test-'));
function run(args, options = {}) {
  return spawnSync(process.execPath, [script, ...args], {
    cwd: scratch(), encoding: 'utf8', timeout: 120_000, ...options,
  });
}
function directory(result) {
  const dir = result.stdout.match(/^Demo directory: (.+)$/m)?.[1];
  assert.ok(dir, result.stdout + result.stderr);
  return dir;
}
const state = dir => JSON.parse(readFileSync(path.join(dir, 'demo-state.json'), 'utf8'));

test('reviewed contract is required before any setup', () => {
  const result = run(['start', binary]);
  assert.equal(result.status, 1);
  assert.match(result.stdout, /reject empty names after trimming/i);
  assert.doesNotMatch(result.stdout, /Demo directory:/);
});

test('real CLI gap and repair preserve one contract and isolate caller data', () => {
  const caller = scratch();
  const sentinel = path.join(caller, 'config.json');
  writeFileSync(sentinel, 'unrelated config sentinel\n');
  mkdirSync(path.join(caller, '.codex'));
  writeFileSync(path.join(caller, '.codex', 'keep'), 'unrelated plugin sentinel\n');
  const before = readdirSync(caller);
  const env = { ...process.env, SPECGATE_CONFIG_PATH: sentinel,
    SPECGATE_LOCAL_DIR: path.join(caller, 'do-not-create'),
    SPECGATE_SERVER: 'http://127.0.0.1:1', SPECGATE_WORKSPACE: 'wrong-workspace' };
  const initial = run(['start', binary, '--ack-contract'], { cwd: caller, env });
  assert.equal(initial.status, 0, initial.stdout + initial.stderr);
  const dir = directory(initial);
  assert.notEqual(dir, caller);
  const first = state(dir);
  assert.equal(first.stage, 'gap');
  assert.match(initial.stdout, /\[unclear\].*reject empty/i);
  assert.match(initial.stdout, /bound check reject-empty: skipped/);
  assert.match(initial.stdout, /command_only/);
  assert.match(initial.stdout, /Next actor: implementing_agent/);
  const completion = JSON.parse(readFileSync(path.resolve(dir, first.completion_path), 'utf8'));
  assert.equal(completion.checks[2].status, 'skipped');
  assert.equal(completion.criteria[2].claim, 'not_done');
  const repaired = run(['repair', dir], { cwd: caller, env });
  assert.equal(repaired.status, 0, repaired.stdout + repaired.stderr);
  const second = state(dir);
  assert.equal(second.stage, 'repaired');
  assert.equal(second.work_ref, first.work_ref);
  assert.equal(second.context_digest, first.context_digest);
  assert.notEqual(second.completion_id, first.completion_id);
  assert.notEqual(second.review_id, first.review_id);
  assert.match(repaired.stdout, /Next actor: human_reviewer/);
  assert.match(repaired.stdout, /Awaiting human acceptance/);
  assert.match(repaired.stdout, /command_only/);
  const report = JSON.parse(readFileSync(path.resolve(dir, second.completion_path), 'utf8'));
  assert.equal(report.checks[2].status, 'pending');
  assert.equal(report.checks[2].command, 'node --test reject-empty.test.mjs');
  assert.equal(report.criteria[2].claim, 'satisfied');
  assert.notEqual(report.agent.name, 'demo-reviewer');
  assert.equal(readFileSync(sentinel, 'utf8'), 'unrelated config sentinel\n');
  assert.equal(readFileSync(path.join(caller, '.codex', 'keep'), 'utf8'), 'unrelated plugin sentinel\n');
  assert.deepEqual(readdirSync(caller), before);
  const transcript = readFileSync(path.join(dir, 'transcript.txt'), 'utf8');
  assert.match(transcript, /exit=1/);
  assert.match(transcript, /exit=0/);
  assert.match(transcript, /"check_name":"reject-empty","check_status":"pass","check_source":"specgate_cli"/);
  const repeat = run(['repair', dir], { env });
  assert.equal(repeat.status, 1);
  assert.match(repeat.stderr, /already repaired/);
  assert.deepEqual(state(dir), second);
});

test('unexpected CLI failure is never treated as the expected gap', () => {
  const dir = scratch();
  const launcher = path.join(dir, 'unavailable-cli');
  writeFileSync(launcher, `#!/bin/sh\ncase " $* " in\n*" change submit "*) printf '%s\\n' '{"ok":false,"error":{"code":"unavailable"}}'; exit 5;;\nesac\nexec '${binary.replaceAll("'", "'\\''")}' "$@"\n`, { mode: 0o700 });
  const result = run(['start', launcher, '--ack-contract']);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /expected exit 1.*got 5/);
  const retained = directory(result);
  assert.match(readFileSync(path.join(retained, 'transcript.txt'), 'utf8'), /exit=5/);
});

test('repair refuses to overwrite edits in the retained demo', () => {
  const initial = run(['start', binary, '--ack-contract']);
  assert.equal(initial.status, 0, initial.stdout + initial.stderr);
  const dir = directory(initial);
  const file = path.join(dir, 'normalize.mjs');
  writeFileSync(file, '// user edit\n');
  const result = run(['repair', dir]);
  assert.equal(result.status, 1);
  assert.match(result.stderr, /normalize.mjs was modified/);
  assert.equal(readFileSync(file, 'utf8'), '// user edit\n');
  assert.equal(state(dir).stage, 'gap');
});

test('an existing third test prevents any partial repair', () => {
  const initial = run(['start', binary, '--ack-contract']);
  assert.equal(initial.status, 0, initial.stdout + initial.stderr);
  const dir = directory(initial);
  const original = readFileSync(path.join(dir, 'normalize.mjs'), 'utf8');
  writeFileSync(path.join(dir, 'reject-empty.test.mjs'), '// user test\n');
  const result = run(['repair', dir]);
  assert.equal(result.status, 1);
  assert.equal(readFileSync(path.join(dir, 'normalize.mjs'), 'utf8'), original);
  assert.equal(readFileSync(path.join(dir, 'reject-empty.test.mjs'), 'utf8'), '// user test\n');
  assert.equal(state(dir).stage, 'gap');
});

for (const invalid of ['JSON', 'context digest', 'binding', 'duplicate binding', 'outside path']) {
  test(`invalid completion ${invalid} leaves repair files unchanged and permits a corrected retry`, () => {
    const initial = run(['start', binary, '--ack-contract']);
    assert.equal(initial.status, 0, initial.stdout + initial.stderr);
    const dir = directory(initial);
    const before = state(dir);
    const statePath = path.join(dir, 'demo-state.json');
    const completionPath = path.resolve(dir, before.completion_path);
    const originalReport = readFileSync(completionPath, 'utf8');
    const originalSource = readFileSync(path.join(dir, 'normalize.mjs'), 'utf8');
    const report = JSON.parse(originalReport);
    if (invalid === 'context digest') report.context_digest = 'wrong-context';
    if (invalid === 'binding') report.criteria[0].verification_binding = 'wrong-check';
    if (invalid === 'duplicate binding') report.criteria[0].verification_binding = report.criteria[1].verification_binding;
    if (invalid === 'outside path') writeFileSync(statePath, JSON.stringify({ ...before, completion_path: path.join(scratch(), 'outside.json') }));
    else writeFileSync(completionPath, invalid === 'JSON' ? '{ broken JSON' : JSON.stringify(report));
    const invalidState = readFileSync(statePath, 'utf8');
    const invalidReport = readFileSync(completionPath, 'utf8');
    const result = run(['repair', dir]);
    assert.equal(result.status, 1, result.stdout + result.stderr);
    assert.equal(readFileSync(path.join(dir, 'normalize.mjs'), 'utf8'), originalSource);
    assert.equal(existsSync(path.join(dir, 'reject-empty.test.mjs')), false);
    assert.equal(readFileSync(statePath, 'utf8'), invalidState);
    assert.equal(readFileSync(completionPath, 'utf8'), invalidReport);
    writeFileSync(statePath, JSON.stringify(before));
    writeFileSync(completionPath, originalReport);
    const retry = run(['repair', dir]);
    assert.equal(retry.status, 0, retry.stdout + retry.stderr);
    assert.equal(state(dir).stage, 'repaired');
  });
}

test('printed repair command runs through sh with spaces and apostrophes in both paths', () => {
  const caller = scratch();
  const source = path.join(caller, "source's copy");
  cpSync(path.dirname(script), source, { recursive: true });
  const temporary = path.join(caller, "demo's temporary directory");
  mkdirSync(temporary);
  const env = { ...process.env, TMPDIR: temporary };
  const initial = spawnSync(process.execPath, [path.join(source, path.basename(script)), 'start', binary, '--ack-contract'], {
    cwd: caller, env, encoding: 'utf8', timeout: 120_000,
  });
  assert.equal(initial.status, 0, initial.stdout + initial.stderr);
  const command = initial.stdout.match(/^Repair: (.+)$/m)?.[1];
  assert.ok(command, initial.stdout);
  const repair = spawnSync('sh', ['-c', command], { cwd: caller, env, encoding: 'utf8', timeout: 120_000 });
  assert.equal(repair.status, 0, repair.stdout + repair.stderr);
  assert.equal(state(directory(initial)).stage, 'repaired');
  assert.match(repair.stdout, /Next actor: human_reviewer/);
});
