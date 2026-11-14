import assert from "node:assert/strict";
import { execFile, spawn } from "node:child_process";
import { mkdtempSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test } from "node:test";
import { setTimeout as delay } from "node:timers/promises";
import { fileURLToPath } from "node:url";
import { promisify } from "node:util";

const script = fileURLToPath(new URL("./local-review-notifications.mjs", import.meta.url));
const repo = "/tmp/explicit repo's $(literal)";
const binary = "/tmp/explicit cli's binary";
const workspace = { id: "workspace-id", slug: "workspace-slug" };
const row = (key, next_actor, change_state) => ({
  key, id: `${key}-id`, workspace_id: workspace.id, next_actor, change_state,
  next_command: `specgate --yes change accept ${key}`,
});
const human = row("LOCAL-12345678", "human_reviewer", "awaiting_acceptance");
const maintainer = row("LOCAL-87654321", "maintainer", "blocked");
const done = { ...human, next_actor: "none", change_state: "accepted" };
const pending = { ...human, next_actor: "implementing_agent", change_state: "implementation" };
const status = (item, review_id) => ({
  mode: "local", ref: item.key, state: item.change_state, next_actor: item.next_actor, review_id,
});

function fixture(frames) {
  const controller = new AbortController();
  const lines = [];
  const warnings = [];
  const commands = [];
  let frameIndex = 0;
  let identityReads = 0;
  return {
    lines, warnings, commands,
    target: { repo, workspace: workspace.slug, binary },
    runtime: {
      signal: controller.signal,
      write: (line) => lines.push(line),
      warn: (line) => warnings.push(line),
      wait: async () => {
        frameIndex += 1;
        identityReads = 0;
        if (frameIndex === frames.length) controller.abort();
      },
      run: async (file, args, options) => {
        const frame = frames[frameIndex];
        commands.push({ file, args, options });
        if (file === "/usr/bin/osascript") return { stdout: "" };
        assert.equal(file, binary);
        assert.equal(options.cwd, repo);
        assert.equal(options.env.SPECGATE_NO_UPDATE_CHECK, "1");
        assert.equal(options.env.SPECGATE_WORKSPACE, undefined);
        assert.ok(options.timeout > 0 && options.maxBuffer > 0);
        assert.equal(options.signal, controller.signal);
        assert.equal(args.at(-1), "--json");
        let command = args[0];
        let data;
        if (command === "doctor") {
          const identity = identityReads++ === 0 ? frame : { ...frame, ...frame.postflight };
          data = {
            mode: identity.mode ?? "local",
            repository: { status: "ok", message: identity.repo ?? repo },
            store: { status: "ok", id: identity.store ?? "store-id" },
            workspace: { status: "ok", slug: identity.doctorSlug ?? workspace.slug },
            identity: { status: "ok" },
          };
        } else if (command === "workspace") {
          assert.deepEqual(args, ["workspace", "current", "--json"]);
          command = "workspace.current";
          data = { workspace: (identityReads > 1 ? frame.postflight?.workspace : undefined) ?? frame.workspace ?? workspace,
            scope: "project", project_root: repo };
        } else {
          assert.deepEqual(args.slice(0, 2), ["--workspace", workspace.id]);
          command = args.slice(2, 4).join(".");
          if (command === "work.list") {
            if (frame.raw !== undefined) return { stdout: frame.raw };
            if (frame.failure) throw new Error("controlled child failure with private output");
            data = { items: frame.items };
          } else {
            assert.equal(command, "change.status");
            data = frame.statuses[args[4]];
          }
        }
        return { stdout: JSON.stringify({ schema_version: "specgate.cli/v1", command, ok: true, data }) };
      },
    },
  };
}

test("announces only a fresh human action or replacement review, once per identity", async () => {
  const { runPilot } = await import(script);
  const initial = { items: [human], statuses: { [human.key]: status(human, "review-1") } };
  const replacement = { items: [human], statuses: { [human.key]: status(human, "review-2") } };
  const f = fixture([
    initial, initial, replacement, replacement,
    { items: [done] }, replacement,
    { items: [human, maintainer], statuses: {
      [human.key]: status(pending, "review-3"), [maintainer.key]: status(maintainer),
    } },
  ]);
  await runPilot(f.target, f.runtime);
  assert.equal(f.lines.filter((line) => line.includes("Baseline recorded")).length, 1);
  const actions = f.lines.filter((line) => line.includes("change status"));
  assert.equal(actions.length, 2);
  assert.match(actions[0], /LOCAL-12345678/);
  assert.match(actions[1], /LOCAL-87654321/);
  assert.match(actions[0], /'\/tmp\/explicit repo'"'"'s \$\(literal\)'/);
  assert.match(actions[0], /--workspace 'workspace-id' change status 'LOCAL-12345678' --detail/);
  assert.ok(f.lines.every((line) => !line.includes("change accept")));
  assert.ok(f.commands.every(({ file }) => file === binary));
  assert.deepEqual(f.warnings, []);
});

test("failed and malformed reads preserve the last complete observation", async () => {
  const { runPilot } = await import(script);
  const humanReview = { items: [human], statuses: { [human.key]: status(human, "review-1") } };
  const f = fixture([
    { items: [pending] }, humanReview, { failure: true }, { raw: "not json" },
    { raw: '{"schema_version":"wrong","command":"work.list","ok":true,"data":{"items":[]}}' },
    { items: [human, { ...done, id: "changed-id" }] },
    { items: [human], statuses: { [human.key]: status(human) } },
    { items: [human], statuses: { [human.key]: { ...status(human, "fake"), ref: maintainer.key } } },
    { items: [human], statuses: { [human.key]: { ...status(done), next_actor: "human_reviewer" } } },
    humanReview,
  ]);
  await runPilot(f.target, f.runtime);
  assert.equal(f.lines.filter((line) => line.includes("change status")).length, 1);
  assert.equal(f.warnings.length, 7);
  assert.ok(f.warnings.every((line) => !line.includes("private output")));
});

test("checks exact Local repository, store and workspace before polling", async () => {
  const { runPilot } = await import(script);
  for (const frame of [{ mode: "full" }, { repo: "/tmp/wrong" }, { workspace: { ...workspace, slug: "wrong" } }]) {
    const initial = fixture([frame]);
    await assert.rejects(runPilot(initial.target, initial.runtime), /Local|workspace/);
    assert.ok(initial.commands.every(({ args }) => !args.includes("list")));
  }
  const rebound = fixture([{ items: [pending] }, { store: "new-store" },
    { workspace: { ...workspace, id: "new-workspace" } }]);
  await runPilot(rebound.target, rebound.runtime);
  assert.equal(rebound.commands.filter(({ args }) => args.includes("list")).length, 1);
  assert.equal(rebound.warnings.length, 2);
});

test("accepts canonical Unicode Local workspace slugs and their immutable IDs", async () => {
  const { runPilot } = await import(script);
  for (const slug of ["đội-phát-triển-１２", "研究-" + "界".repeat(130)]) {
    for (const selected of [slug, workspace.id]) {
      const identity = { workspace: { ...workspace, slug }, doctorSlug: slug };
      const pilot = fixture([
        { ...identity, items: [] },
        { ...identity, items: [human], statuses: { [human.key]: status(human, "review-1") } },
      ]);
      await runPilot({ ...pilot.target, workspace: selected }, pilot.runtime);
      assert.deepEqual(pilot.warnings, []);
      assert.equal(pilot.lines.filter((line) => line.includes("change status")).length, 1);
      assert.match(pilot.lines.at(-1), /--workspace 'workspace-id'/);
    }
  }
});

test("discards the entire batch when the workspace rebinds after status was read", async () => {
  const { runPilot } = await import(script);
  const review = { items: [human], statuses: { [human.key]: status(human, "review-1") } };
  const f = fixture([{ items: [] }, { ...review, postflight: { workspace: { ...workspace, id: "new-workspace" } } }, review]);
  await runPilot(f.target, f.runtime);
  const statusReads = f.commands.filter(({ args }) => args.includes("status"));
  assert.equal(statusReads.length, 2);
  assert.equal(f.warnings.length, 1);
  assert.equal(f.lines.filter((line) => line.includes("change status")).length, 1);
});

test("the macOS sender receives inert arguments only on explicit opt-in", async () => {
  const { runPilot } = await import(script);
  const frames = [{ items: [] }, { items: [human], statuses: { [human.key]: status(human, "review-1") } }];
  const f = fixture(frames);
  await runPilot({ ...f.target, macos: true }, { ...f.runtime, platform: "darwin" });
  const sends = f.commands.filter(({ file }) => file === "/usr/bin/osascript");
  assert.equal(sends.length, 1);
  assert.equal(sends[0].args.at(-2), "--");
  assert.match(sends[0].args.at(-1), /LOCAL-12345678/);
  assert.ok(sends[0].args.slice(0, -1).every((arg) => !arg.includes("$(literal)")));
  await assert.rejects(runPilot({ ...f.target, macos: true }, { ...f.runtime, platform: "linux" }), /macOS/);
});

test("enforces a child output limit and a deadline", async () => {
  const { readCommand } = await import(script);
  const signal = new AbortController().signal;
  await assert.rejects(readCommand(process.execPath, ["-e", "process.stdout.write('x'.repeat(10000))"], {
    signal, timeout: 2000, maxBuffer: 1024,
  }), /output limit/);

  const started = Date.now();
  await assert.rejects(readCommand(process.execPath, ["-e", "setInterval(() => {}, 1000)"], {
    signal, timeout: 200, maxBuffer: 1024,
  }), /timed out/);
  assert.ok(Date.now() - started < 2500);
});

test("Ctrl-C cancels an in-flight CLI subprocess and exits cleanly", { skip: process.platform === "win32", timeout: 7000 }, async (t) => {
  const dir = mkdtempSync(join(tmpdir(), "specgate-notifications-test-"));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const fake = join(dir, "fake-cli");
  // Use a PID file for the fixture because the pilot intentionally suppresses CLI stderr.
  const pidPath = join(dir, "child-pids.json");
  writeFileSync(fake, `#!/usr/bin/env node
const child = require('node:child_process').spawn(process.execPath,
  ['-e', 'setTimeout(() => {}, 2500)'], { stdio: 'inherit' });
require('node:fs').writeFileSync(${JSON.stringify(pidPath)}, JSON.stringify([process.pid, child.pid]));
process.on('SIGTERM', () => {});
setInterval(() => {}, 1000);
`, { mode: 0o700 });
  const child = spawn(process.execPath, [script, "--repo", dir, "--workspace", "fake", "--specgate", fake]);
  t.after(() => child.kill("SIGKILL"));
  const closed = new Promise((resolve) => child.once("close", (code) => resolve(code)));
  const { readFile } = await import("node:fs/promises");
  const deadline = Date.now() + 5000;
  let pids;
  while (!pids && Date.now() < deadline) {
    try { pids = JSON.parse(await readFile(pidPath, "utf8")); } catch (error) { if (error.code !== "ENOENT") throw error; }
    if (!pids) await delay(20);
  }
  assert.ok(pids?.every(Number.isInteger), "fake CLI and its descendant were started");
  t.after(() => {
    for (const pid of pids) {
      try { process.kill(pid, "SIGKILL"); } catch {}
    }
  });
  const stopped = Date.now();
  child.kill("SIGINT");
  assert.equal(await closed, 0);
  assert.ok(Date.now() - stopped < 1500, "inherited pipes did not delay shutdown");
  for (const pid of pids) assert.throws(() => process.kill(pid, 0), { code: "ESRCH" });

  const { stdout } = await promisify(execFile)(process.execPath, [script, "--help"]);
  assert.match(stdout, /--repo.*--workspace/);
});
