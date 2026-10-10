import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { mkdtempSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { test } from "node:test";

const script = fileURLToPath(new URL("release-notes.mjs", import.meta.url));

test("release notes select the exact authored version and exclude other releases", () => {
  const dir = mkdtempSync(join(tmpdir(), "specgate-release-notes-"));
  try {
    writeFileSync(join(dir, "CHANGELOG.md"), "# Changelog\n\n## Unreleased\n\n- Not shipped.\n\n## [0.2.6] - 2026-10-10\n\n### Added\n\n- Observed checks.\n\n### Upgrade\n\n- Back up first.\n\n## [0.2.5] - 2026-09-11\n\n- Old release.\n\n[0.2.6]: https://example.test/new\n");
    const run = (version) => spawnSync(process.execPath, [script, version], { cwd: dir, encoding: "utf8" });
    const selected = run("v0.2.6");
    assert.equal(selected.status, 0, selected.stderr);
    assert.equal(selected.stdout, "### Added\n\n- Observed checks.\n\n### Upgrade\n\n- Back up first.\n");
    assert.notEqual(run("v0.2.60").status, 0);
    assert.notEqual(run("Unreleased").status, 0);
    writeFileSync(join(dir, "CHANGELOG.md"), "## [0.2.6]\n\n## [0.2.5]\n- Old\n");
    assert.notEqual(run("v0.2.6").status, 0);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
