import { readFileSync } from "node:fs";

const [tag, ...extra] = process.argv.slice(2);
if (!tag || extra.length || !/^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/.test(tag)) {
  throw new Error("Usage: node .github/scripts/release-notes.mjs vX.Y.Z");
}
const lines = readFileSync("CHANGELOG.md", "utf8").split(/\r?\n/);
const heading = `## [${tag.slice(1)}]`;
const start = lines.findIndex((line) => line === heading || line.startsWith(`${heading} - `));
if (start < 0) throw new Error(`CHANGELOG.md has no authored section for ${tag}`);
let end = start + 1;
while (end < lines.length && !/^## |^\[[^\]]+\]:/.test(lines[end])) end += 1;
const notes = lines.slice(start + 1, end).join("\n").trim();
if (!notes) throw new Error(`CHANGELOG.md section for ${tag} is empty`);
process.stdout.write(`${notes}\n`);
