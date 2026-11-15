import assert from "node:assert/strict";
import { execFileSync, spawnSync } from "node:child_process";
import { createHash } from "node:crypto";
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { delimiter, join } from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

const installer = fileURLToPath(new URL("./install-cli.sh", import.meta.url));
const archive = "specgate_9.9.9_linux_arm64.tar.gz";
const checksums = "specgate_9.9.9_checksums.txt";
const replacement = "#!/bin/sh\necho replacement\n";

function installerFixture(t, { corruptChecksum = false } = {}) {
  const root = mkdtempSync(join(tmpdir(), "specgate-installer-test-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  const installDir = join(root, "install with 'quotes'");
  const home = join(root, "home");
  const tools = join(root, "tools");
  const release = join(root, "release");
  for (const dir of [installDir, home, tools, release]) mkdirSync(dir);
  writeFileSync(join(release, "specgate"), replacement, { mode: 0o755 });
  execFileSync("tar", ["-czf", archive, "specgate"], { cwd: release });
  const digest = createHash("sha256").update(readFileSync(join(release, archive))).digest("hex");
  writeFileSync(join(release, checksums), `${corruptChecksum ? "0".repeat(64) : digest}  ${archive}\n`);
  writeFileSync(join(tools, "uname"), '#!/bin/sh\ncase "$1" in -s) echo Linux;; -m) echo aarch64;; *) exit 1;; esac\n', { mode: 0o755 });
  writeFileSync(join(tools, "curl"), `#!/bin/sh
set -eu
test "$1" = -fsSL && test "$3" = -o
case "$2" in
  "https://github.com/thanhtung2693/specgate/releases/download/v9.9.9/${checksums}"|"https://github.com/thanhtung2693/specgate/releases/download/v9.9.9/${archive}")
    cp "$RELEASE_FIXTURE/$(basename "$2")" "$4"
    ;;
  *) exit 1 ;;
esac
`, { mode: 0o755 });
  const run = ({ onPath = false } = {}) => spawnSync("sh", [installer, "--version", "v9.9.9", "--install-dir", installDir], {
    cwd: root,
    encoding: "utf8",
    env: { ...process.env, HOME: home, PATH: [tools, ...(onPath ? [installDir] : []), "/usr/bin", "/bin"].join(delimiter), RELEASE_FIXTURE: release, TMPDIR: root },
  });
  return { root, installDir, dest: join(installDir, "specgate"), run };
}

test("the installer rejects a directory or a symlink to a directory at the binary path", async (t) => {
  for (const symlink of [false, true]) {
    await t.test(symlink ? "symlink to directory" : "directory", (t) => {
      const { root, installDir, dest, run } = installerFixture(t);
      const target = symlink ? join(root, "other installation") : dest;
      mkdirSync(target);
      if (symlink) symlinkSync(target, dest);
      const unrelated = join(target, "keep");
      writeFileSync(unrelated, "user data");
      const result = run();
      assert.equal(result.error, undefined);
      assert.equal(result.status, 1, result.stdout + result.stderr);
      assert.match(result.stderr, /directory/);
      assert.ok(result.stderr.includes(dest));
      assert.doesNotMatch(result.stdout, /installed successfully/);
      assert.deepEqual(readdirSync(installDir), ["specgate"]);
      assert.deepEqual(readdirSync(target), ["keep"]);
      assert.equal(readFileSync(unrelated, "utf8"), "user data");
    });
  }
});

test("the installer replaces an existing binary only after verifying the download", async (t) => {
  for (const corruptChecksum of [false, true]) {
    await t.test(corruptChecksum ? "incorrect checksum" : "matching checksum", (t) => {
      const { installDir, dest, run } = installerFixture(t, { corruptChecksum });
      const original = "#!/bin/sh\necho original\n";
      const unrelated = join(installDir, "keep");
      writeFileSync(dest, original, { mode: 0o755 });
      writeFileSync(unrelated, "user data");
      const result = run();
      assert.equal(result.error, undefined);
      assert.equal(result.status, corruptChecksum ? 1 : 0, result.stdout + result.stderr);
      if (corruptChecksum) {
        assert.match(result.stderr, /Checksum mismatch/);
      } else {
        assert.match(result.stdout, /Checksum verified/);
        assert.equal(execFileSync(dest, { encoding: "utf8" }), "replacement\n");
      }
      assert.deepEqual(readdirSync(installDir), ["keep", "specgate"]);
      assert.equal(readFileSync(dest, "utf8"), corruptChecksum ? original : replacement);
      assert.equal(readFileSync(unrelated, "utf8"), "user data");
    });
  }
});

test("the installer prints a copyable PATH command only when the binary is missing from PATH", (t) => {
  const { root, run } = installerFixture(t);
  const missing = run();
  assert.equal(missing.status, 0, missing.stdout + missing.stderr);
  const exportLine = missing.stdout.split("\n").find((line) => line.trimStart().startsWith("export PATH="));
  assert.ok(exportLine, missing.stdout);
  const invoked = spawnSync("sh", ["-c", exportLine + "\nspecgate"], {
    cwd: root, encoding: "utf8", env: { ...process.env, PATH: "/usr/bin:/bin" },
  });
  assert.equal(invoked.status, 0, invoked.stderr);
  assert.equal(invoked.stdout, "replacement\n");
  const ready = run({ onPath: true });
  assert.equal(ready.status, 0, ready.stdout + ready.stderr);
  assert.doesNotMatch(ready.stdout, /export PATH=/);
});
