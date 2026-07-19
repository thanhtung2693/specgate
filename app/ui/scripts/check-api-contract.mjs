import { spawnSync } from "node:child_process"
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs"
import { tmpdir } from "node:os"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const uiDir = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const registryDir = resolve(uiDir, "../doc-registry")
const tempDir = mkdtempSync(resolve(tmpdir(), "specgate-openapi-"))

try {
  const openapi = spawnSync("go", ["run", "./cmd/openapi"], {
    cwd: registryDir,
    encoding: "utf8",
  })
  if (openapi.status !== 0) {
    process.stderr.write(openapi.stderr)
    process.exit(openapi.status ?? 1)
  }
  const tempOpenAPI = resolve(tempDir, "openapi.json")
  const tempSchema = resolve(tempDir, "schema.d.ts")
  writeFileSync(tempOpenAPI, openapi.stdout)
  const generated = spawnSync(
    "npx",
    ["--yes", "openapi-typescript@7.13.0", tempOpenAPI, "-o", tempSchema],
    { cwd: uiDir, encoding: "utf8", stdio: "inherit" },
  )
  if (generated.status !== 0) process.exit(generated.status ?? 1)

  const checks = [
    [resolve(uiDir, "openapi.json"), tempOpenAPI],
    [resolve(uiDir, "src/api/schema.d.ts"), tempSchema],
  ]
  const drifted = checks
    .filter(([committed, generatedPath]) => readFileSync(committed, "utf8") !== readFileSync(generatedPath, "utf8"))
    .map(([committed]) => committed)
  if (drifted.length > 0) {
    process.stderr.write(`Generated API contract drifted:\n${drifted.map((path) => `- ${path}`).join("\n")}\nRun npm run api:generate.\n`)
    process.exit(1)
  }
} finally {
  rmSync(tempDir, { recursive: true, force: true })
}
