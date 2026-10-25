import assert from 'node:assert/strict';
import { appendFileSync, copyFileSync, constants, lstatSync, mkdtempSync, readFileSync, readdirSync, realpathSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';

const criteria = [
  'Trim surrounding whitespace from names @check:trim',
  'Lowercase names @check:lowercase',
  'Reject empty names after trimming @check:reject-empty',
];
const checks = ['trim', 'lowercase', 'reject-empty'];
const fixture = fileURLToPath(new URL('./fixtures/delivery-proof/', import.meta.url));
const [stage, target, acknowledgment] = process.argv.slice(2);

try {
  assert.ok((stage === 'start' || stage === 'repair') && target,
    'Usage: node local-delivery-proof.mjs start /absolute/specgate --ack-contract | repair /demo/directory');
  if (stage === 'start') {
    console.log('Review this demo contract before setup:\n' + criteria.join('\n'));
    console.log('Checks (cwd: fresh demo directory):\n' + checks.map(name => `node --test ${name}.test.mjs`).join('\n'));
    console.log('Runs Node tests through sh, creates an isolated retained Local store, and stops before human acceptance.');
    assert.equal(acknowledgment, '--ack-contract', 'Review the contract, then rerun with --ack-contract.');
    assert.ok(path.isAbsolute(target), 'Pass an absolute CLI binary path.');
  }
  const root = realpathSync(stage === 'start' ? mkdtempSync(path.join(tmpdir(), 'specgate-delivery-proof-')) : target);
  console.log(`Demo directory: ${root}`);
  const statePath = path.join(root, 'demo-state.json');
  let state = stage === 'repair' ? readJSON(statePath) : { binary: target };
  assert.ok(stage === 'start' || state.stage === 'gap', 'Demo already repaired or not at the repair stage; start a fresh run.');
  const env = Object.fromEntries(Object.entries(process.env).filter(([key]) => !key.startsWith('SPECGATE_')));
  Object.assign(env, { CI: '1', SPECGATE_CONFIG_PATH: path.join(root, 'config.json'), SPECGATE_LOCAL_DIR: path.join(root, 'governance') });
  const transcript = path.join(root, 'transcript.txt');
  function run(binary, args, expected = 0) {
    const result = spawnSync(binary, args, { cwd: root, env, encoding: 'utf8', timeout: 120_000, maxBuffer: 4 * 1024 * 1024 });
    appendFileSync(transcript, `\n$ ${[binary, ...args].join(' ')}\nexit=${result.status}\n${result.stdout ?? ''}${result.stderr ?? ''}`);
    assert.ifError(result.error);
    assert.equal(result.status, expected, `expected exit ${expected}, got ${result.status}: ${result.stdout}${result.stderr}`);
    return result.stdout;
  }
  function cli(args, expected = 0) {
    const selection = state.workspace ? ['--workspace', state.workspace] : [];
    const envelope = JSON.parse(run(state.binary, ['--json', ...selection, ...args], expected));
    assert.equal(envelope.ok, true, `CLI did not return a success envelope: ${JSON.stringify(envelope)}`);
    assert.ok(envelope.data, 'CLI envelope has no data');
    return envelope.data;
  }
  function copy(source, destination, replace = false) {
    const destinationPath = path.join(root, destination);
    if (replace) assert.ok(lstatSync(destinationPath).isFile(), `Refuse to replace a non-file: ${destination}`);
    copyFileSync(path.join(fixture, source), destinationPath, replace ? 0 : constants.COPYFILE_EXCL);
  }
  if (stage === 'start') {
    const version = run(state.binary, ['--version']).trim();
    console.log(`CLI: ${version}\nNode: ${process.version}`);
    const setup = cli(['init', '--mode', 'local', '--workspace-name', 'Delivery proof', '--display-name', 'Demo reviewer', '--username', 'demo-reviewer']);
    assert.equal(setup.mode, 'local');
    assert.equal(setup.state_dir, env.SPECGATE_LOCAL_DIR);
    state.workspace = setup.workspace.id;
    state.workspace_slug = setup.workspace.slug;
    state.cli_version = version;
    state.node_version = process.version;
    copy('initial/normalize.mjs', 'normalize.mjs');
    copy('trim.test.mjs', 'trim.test.mjs');
    copy('lowercase.test.mjs', 'lowercase.test.mjs');
  }
  const doctor = cli(['doctor']);
  assert.equal(doctor.mode, 'local');
  assert.equal(doctor.store.path, env.SPECGATE_LOCAL_DIR);
  assert.equal(doctor.identity.username, 'demo-reviewer');
  assert.equal(doctor.identity.status, 'ok');
  assert.equal(doctor.workspace.slug, state.workspace_slug);
  assert.equal(doctor.workspace.status, 'ok');
  if (stage === 'start') {
    const work = cli(['work', 'create-quick', 'Normalize names', '--description', 'Demonstrate a declared delivery evidence gap, then repair it without accepting automatically.', ...criteria.flatMap(ac => ['--ac', ac])]);
    state.work_ref = work.key;
    state.context_digest = work.context_digest;
    const context = cli(['work', 'context', work.key]);
    assert.equal(context.context_digest, state.context_digest);
    state.completion_path = cli(['delivery', 'report', work.key, '--init']).path;
  } else {
    const current = cli(['change', 'status', state.work_ref]);
    assert.equal(current.next_actor, 'implementing_agent');
    assert.equal(current.completion_id, state.completion_id);
    assert.equal(current.context_digest, state.context_digest);
    assert.ok(lstatSync(path.join(root, 'normalize.mjs')).isFile(), 'normalize.mjs must remain a regular demo file.');
    assert.equal(readFileSync(path.join(root, 'normalize.mjs'), 'utf8'), readFileSync(path.join(fixture, 'initial/normalize.mjs'), 'utf8'), 'normalize.mjs was modified; preserve this run and start a fresh demo.');
    assert.ok(!readdirSync(root).includes('reject-empty.test.mjs'), 'reject-empty.test.mjs already exists; preserve this run and start a fresh demo.');
  }
  const completionPath = path.resolve(root, state.completion_path);
  assert.ok(completionPath.startsWith(root + path.sep), 'Completion scaffold is outside the demo directory.');
  const report = readJSON(completionPath);
  assert.equal(report.context_digest, state.context_digest);
  assert.deepEqual(report.criteria.map(row => row.verification_binding).sort(), [...checks].sort(), 'Completion must retain each approved check binding exactly once.');
  report.agent = { name: 'demo-coding-agent' };
  report.summary = stage === 'start' ? 'Trim and lowercase implemented; empty-name rejection and its test are absent.' : 'Added empty-name rejection and its test; rerun all three checks.';
  report.affected_files = ['normalize.mjs', ...checks.slice(0, stage === 'start' ? 2 : 3).map(name => `${name}.test.mjs`)];
  report.checks = checks.map(name => ({ name, command: `node --test ${name}.test.mjs`, status: stage === 'start' && name === 'reject-empty' ? 'skipped' : 'pending', detail: stage === 'start' && name === 'reject-empty' ? 'Empty-name rejection is not implemented and reject-empty.test.mjs does not exist yet.' : 'Execute the named Node test.' }));
  report.criteria = report.criteria.map(row => {
    const name = row.verification_binding;
    assert.ok(checks.includes(name), 'Unexpected criterion binding in the CLI scaffold.');
    const missing = stage === 'start' && name === 'reject-empty';
    return { ...row, claim: missing ? 'not_done' : 'satisfied', evidence: { kind: missing ? 'code' : 'test', path: missing ? 'normalize.mjs' : `${name}.test.mjs`, line: missing ? 1 : 5 } };
  });
  const thirdID = report.criteria.find(row => row.verification_binding === 'reject-empty').criterion_id;
  const completionBody = JSON.stringify(report, null, 2) + '\n';
  if (stage === 'repair') {
    copy('repair/normalize.mjs', 'normalize.mjs', true);
    copy('reject-empty.test.mjs', 'reject-empty.test.mjs');
  }
  writeFileSync(completionPath, completionBody);
  console.log(run(process.execPath, ['--test', ...checks.slice(0, stage === 'start' ? 2 : 3).map(name => `${name}.test.mjs`)]).trim());
  const status = cli(['change', 'submit', state.work_ref, '--file', state.completion_path, '--run-checks', '--yes'], stage === 'start' ? 1 : 0);
  assert.equal(status.context_digest, state.context_digest);
  assert.equal(status.criteria.length, 3);
  assert.ok(status.completion_id && status.review_id);
  if (stage === 'start') {
    assert.equal(status.next_actor, 'implementing_agent');
    assert.equal(status.criteria.find(row => row.criterion_id === thirdID).verdict, 'unclear');
    assert.ok(status.criterion_evidence.find(row => row.id === thirdID).gaps.includes('bound check reject-empty: skipped'));
  } else {
    assert.equal(status.state, 'awaiting_acceptance');
    assert.equal(status.next_actor, 'human_reviewer');
    assert.ok(status.criteria.every(row => row.verdict === 'met'));
    assert.notEqual(status.completion_id, state.completion_id);
    assert.notEqual(status.review_id, state.review_id);
  }
  assert.ok(status.criterion_evidence.slice(0, stage === 'start' ? 2 : 3).every(row => row.check_status === 'pass' && row.check_provenance === 'command_only'));
  assert.notEqual(status.decision, 'Accepted by human');
  state = { ...state, stage: stage === 'start' ? 'gap' : 'repaired', completion_id: status.completion_id, review_id: status.review_id };
  writeFileSync(statePath, JSON.stringify(state, null, 2) + '\n', { flag: stage === 'start' ? 'wx' : 'w' });
  console.log(run(state.binary, ['--plain', '--workspace', state.workspace, 'change', 'status', state.work_ref]).trim());
  console.log(`Stage: ${state.stage}\nTranscript: ${transcript}`);
  if (stage === 'start') console.log(`Repair: node ${quote(fileURLToPath(import.meta.url))} repair ${quote(root)}`);
  console.log(`Isolated follow-up (set these in a new shell first):\ncd ${quote(root)}\nexport SPECGATE_CONFIG_PATH=${quote(env.SPECGATE_CONFIG_PATH)}\nexport SPECGATE_LOCAL_DIR=${quote(env.SPECGATE_LOCAL_DIR)}\nexport SPECGATE_WORKSPACE=${quote(state.workspace)}\nunset SPECGATE_SERVER\nexport PATH=${quote(path.dirname(state.binary))}:"$PATH"`);
  console.log(`Next (${status.next_actor}): ${status.next_command}`);
} catch (error) {
  console.error(error.message);
  process.exitCode = 1;
}

function readJSON(file) {
  assert.ok(lstatSync(file).isFile(), `Expected a regular file: ${file}`);
  return JSON.parse(readFileSync(file, 'utf8'));
}

function quote(value) {
  return "'" + value.replaceAll("'", "'\\''") + "'";
}
