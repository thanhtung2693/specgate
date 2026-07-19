import { spawnSync } from "node:child_process"
import { writeFileSync } from "node:fs"
import { dirname, resolve } from "node:path"
import { fileURLToPath } from "node:url"

const uiDir = resolve(dirname(fileURLToPath(import.meta.url)), "..")
const registryDir = resolve(uiDir, "../doc-registry")
const openapiPath = resolve(uiDir, "openapi.json")
const schemaPath = resolve(uiDir, "src/api/schema.d.ts")

const openapi = spawnSync("go", ["run", "./cmd/openapi"], {
  cwd: registryDir,
  encoding: "utf8",
})
if (openapi.status !== 0) {
  process.stderr.write(openapi.stderr)
  process.exit(openapi.status ?? 1)
}
writeFileSync(openapiPath, openapi.stdout, { mode: 0o644 })

const generated = spawnSync(
  "npx",
  ["--yes", "openapi-typescript@7.13.0", openapiPath, "-o", schemaPath],
  { cwd: uiDir, encoding: "utf8", stdio: "inherit" },
)
process.exit(generated.status ?? 1)
