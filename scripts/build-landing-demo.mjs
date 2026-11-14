import { cp, mkdir, readFile, writeFile } from "node:fs/promises";

const assets = new URL("../app/cli/internal/command/local_ui_assets/", import.meta.url);
const target = new URL("../app/landing/demo/shared/", import.meta.url);

await mkdir(target, { recursive: true });
await Promise.all([
  ...["core.js", "locale.js", "presentation.js", "source-view.js", "markdown.js", "assets"].map((path) =>
    cp(new URL(path, assets), new URL(path, target), { recursive: true }),
  ),
  readFile(new URL("style.css", assets), "utf8").then((css) =>
    writeFile(new URL("style.css", target), css.replaceAll('url("/assets/', 'url("./assets/')),
  ),
]);
