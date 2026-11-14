import { spawn } from "node:child_process";
import { realpath } from "node:fs/promises";
import { basename, isAbsolute } from "node:path";
import { setTimeout as delay } from "node:timers/promises";
import { parseArgs } from "node:util";

const POLL_MS = 30_000;
const childOptions = { timeout: 10_000, maxBuffer: 4 * 1024 * 1024 };
const humanActors = new Set(["human_reviewer", "maintainer"]);
const actorsByState = {
  implementation: "implementing_agent",
  review_pending: "implementing_agent",
  rework_requested: "implementing_agent",
  awaiting_review: "human_reviewer",
  awaiting_acceptance: "human_reviewer",
  accepted: "none",
  blocked: "maintainer",
};
const usage = "Usage: node scripts/local-review-notifications.mjs --repo <bound Git root> --workspace <slug or ID> [--specgate <absolute CLI path>] [--macos-notifications]";
const appleScript = [
  "-e", "on run argv",
  "-e", 'display notification (item 1 of argv) with title "SpecGate Local"',
  "-e", "end run", "--",
];

export function readCommand(file, args, { signal, timeout, maxBuffer, ...options }) {
  return new Promise((resolve, reject) => {
    signal.throwIfAborted();
    const processGroup = process.platform !== "win32";
    const child = spawn(file, args, { ...options, detached: processGroup, stdio: ["ignore", "pipe", "pipe"] });
    const chunks = [];
    let failure;
    function stop(error) {
      if (failure) return;
      failure = error;
      if (child.pid !== undefined) {
        try {
          // Children inheriting stdout can delay close after their parent exits.
          process.kill(processGroup ? -child.pid : child.pid, "SIGKILL");
        } catch (error) {
          if (error.code !== "ESRCH") failure = error;
        }
      }
      child.stdout.destroy();
      child.stderr.destroy();
    }
    const abort = () => stop(signal.reason);
    const deadline = setTimeout(() => stop(new Error("CLI subprocess timed out.")), timeout);
    signal.addEventListener("abort", abort, { once: true });
    for (const stream of [child.stdout, child.stderr]) {
      let size = 0;
      stream.on("data", (chunk) => {
        size += chunk.length;
        if (size > maxBuffer) stop(new Error("CLI subprocess exceeded the output limit."));
        else if (stream === child.stdout) chunks.push(chunk);
      });
      stream.on("error", stop);
    }
    child.on("error", stop);
    child.on("close", (code) => {
      clearTimeout(deadline);
      signal.removeEventListener("abort", abort);
      if (failure || code !== 0) reject(failure ?? new Error("CLI subprocess failed."));
      else resolve({ stdout: Buffer.concat(chunks).toString("utf8") });
    });
  });
}
const identifier = (value) => typeof value === "string" && /^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$/.test(value);
// Match the Unicode letters, decimal digits and hyphens used by the Local CLI.
const workspaceSlug = (value) => typeof value === "string" && /^[\p{L}\p{Nd}]+(?:-[\p{L}\p{Nd}]+)*$/u.test(value);
const safePath = (value) => typeof value === "string" && value.length > 0 && !/[\p{Cc}\u202a-\u202e\u2066-\u2069]/u.test(value);
const quote = (value) => `'${value.replaceAll("'", `'"'"'`)}'`;
const validActor = (state, actor) => Object.hasOwn(actorsByState, state) && actorsByState[state] === actor;

export async function runPilot(target, {
  signal,
  run = readCommand,
  wait = () => delay(POLL_MS, undefined, { signal }),
  write = console.log,
  warn = console.error,
  platform = process.platform,
} = {}) {
  const { repo, workspace, binary, macos } = target;
  if (macos && platform !== "darwin") throw new Error("--macos-notifications requires macOS.");

  const env = { ...process.env, SPECGATE_NO_UPDATE_CHECK: "1" };
  delete env.SPECGATE_WORKSPACE;
  const options = { ...childOptions, cwd: repo, env, signal };
  async function read(command, args) {
    try {
      const { stdout } = await run(binary, [...args, "--json"], options);
      const envelope = JSON.parse(stdout);
      if (envelope?.schema_version === "specgate.cli/v1" && envelope.command === command &&
          envelope.ok === true && envelope.data && typeof envelope.data === "object" && !Array.isArray(envelope.data)) {
        return envelope.data;
      }
    } catch {
      signal.throwIfAborted();
    }
    throw new Error(`Could not read ${command} through the Local CLI.`);
  }

  async function readIdentity() {
    const doctor = await read("doctor", ["doctor"]);
    if (doctor.mode !== "local" || doctor.repository?.message !== repo ||
        !["repository", "workspace", "identity", "store"].every((key) => doctor[key]?.status === "ok") ||
        !identifier(doctor.store.id)) {
      throw new Error("Choose a healthy Local CLI and its bound repository root.");
    }
    const current = await read("workspace.current", ["workspace", "current"]);
    if (current.scope !== "project" || current.project_root !== repo || !identifier(current.workspace?.id) ||
        !workspaceSlug(current.workspace.slug) || current.workspace.slug !== doctor.workspace.slug ||
        ![current.workspace.id, current.workspace.slug].includes(workspace)) {
      throw new Error("The selected Local workspace must match the repository binding.");
    }
    return { store: doctor.store.id, workspace: current.workspace.id };
  }

  async function readPending(workspaceId) {
    const selection = ["--workspace", workspaceId];
    const { items } = await read("work.list", [...selection, "work", "list"]);
    if (!Array.isArray(items)) throw new Error("Invalid Local work list.");
    const pending = [];
    const ids = new Set();
    const refs = new Set();
    for (const item of items) {
      if (!identifier(item?.id) || !identifier(item.key) || item.workspace_id !== workspaceId ||
          !validActor(item.change_state, item.next_actor) || ids.has(item.id) || refs.has(item.key)) {
        throw new Error("Invalid or out-of-scope Local work list.");
      }
      ids.add(item.id);
      refs.add(item.key);
      if (!humanActors.has(item.next_actor)) continue;

      const detail = await read("change.status", [...selection, "change", "status", item.key]);
      if (detail.mode !== "local" || detail.ref !== item.key || !validActor(detail.state, detail.next_actor) ||
          ((detail.review_id !== undefined || detail.next_actor === "human_reviewer") && !identifier(detail.review_id))) {
        throw new Error("Invalid or out-of-scope Local change status.");
      }
      if (!humanActors.has(detail.next_actor)) continue;
      const identity = JSON.stringify([repo, workspaceId, item.id, detail.next_actor, detail.state, detail.review_id]);
      pending.push({ identity, ref: item.key, actor: detail.next_actor, state: detail.state });
    }
    return pending;
  }

  try {
    const seen = new Set();
    let baseline = true;
    let identity;
    const verifyIdentity = async () => {
      const current = await readIdentity();
      identity ??= current;
      if (current.store !== identity.store || current.workspace !== identity.workspace) {
        throw new Error("The Local store or workspace changed. Restart the pilot to select it.");
      }
    };
    while (!signal.aborted) {
      try {
        await verifyIdentity();
        const pending = await readPending(identity.workspace);
        await verifyIdentity();
        signal.throwIfAborted();
        for (const { identity: key, ref, actor, state } of pending) {
          signal.throwIfAborted();
          if (seen.has(key)) continue;
          seen.add(key);
          if (baseline) continue;
          const action = `(cd ${quote(repo)} && SPECGATE_NO_UPDATE_CHECK=1 ${quote(binary)} --workspace ${quote(identity.workspace)} change status ${quote(ref)} --detail)`;
          write(`${ref} needs ${actor} (${state}). Read again before deciding: ${action}`);
          if (macos) {
            const body = `${basename(repo).slice(0, 70)}: ${ref} needs ${actor}. Read the current status in the pilot terminal.`;
            try {
              await run("/usr/bin/osascript", [...appleScript, body], options);
            } catch {
              if (!signal.aborted) warn("macOS notice could not be sent. The terminal notice is available.");
            }
          }
        }
        if (baseline) {
          write(`Baseline recorded for ${repo} (${identity.workspace}). Polling every 30 seconds after each read. Ctrl-C stops the pilot.`);
          baseline = false;
        }
      } catch (error) {
        if (!identity) throw error;
        if (!signal.aborted) warn(`${error.message} Observations preserved; retrying in 30 seconds.`);
      }
      if (!signal.aborted) await wait();
    }
  } catch (error) {
    if (!signal.aborted) throw error;
  }
}

async function main() {
  const { values } = parseArgs({ options: {
    repo: { type: "string" },
    workspace: { type: "string" },
    specgate: { type: "string" },
    "macos-notifications": { type: "boolean" },
    help: { type: "boolean", short: "h" },
  } });
  if (values.help) return console.log(usage);
  if (!safePath(values.repo) || (!identifier(values.workspace) && !workspaceSlug(values.workspace)) ||
      (values.specgate !== undefined && (!safePath(values.specgate) || !isAbsolute(values.specgate)))) {
    throw new Error(usage);
  }
  const repo = await realpath(values.repo).catch(() => { throw new Error("The supplied repository path is unavailable."); });
  if (!safePath(repo)) throw new Error("The resolved repository path contains unsupported control characters.");
  const controller = new AbortController();
  const stop = () => controller.abort();
  process.on("SIGINT", stop);
  process.on("SIGTERM", stop);
  try {
    await runPilot({ repo, workspace: values.workspace, binary: values.specgate ?? "specgate", macos: values["macos-notifications"] }, { signal: controller.signal });
  } finally {
    process.off("SIGINT", stop);
    process.off("SIGTERM", stop);
  }
}

if (import.meta.main) {
  main().catch((error) => {
    console.error(error.message);
    process.exitCode = 1;
  });
}
