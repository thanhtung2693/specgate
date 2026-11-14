# Marked browser runtime

- Version: 16.4.2, MIT license; full license is in `LICENSE.md`.
- Copied unmodified from the installed, locked UI dependency:
  `app/ui/node_modules/marked/lib/marked.umd.js`.
- Lock record: [pre-retirement UI lockfile](https://github.com/thanhtung2693/specgate/blob/2aa5c463ef61c58edb3b741e5ea8a1b2aa53c102/app/ui/package-lock.json), `node_modules/marked`.
- Package: https://registry.npmjs.org/marked/-/marked-16.4.2.tgz
- Package integrity: `sha512-TI3V8YYWvkVf3KJe1dRkpnjs68JUPyEa5vjKrp1XEEJUAOaQc+Qj+L1qWbPd0SJuAdQkFU0h73sXXqwDYxsiDA==`
- Browser file SHA-256: `2e20e86f0a13107ae9bf00675894bbb573716a8e5b4fde612eacaff1e404c128`.
- Upstream: https://github.com/markedjs/marked

This file is the upstream UMD browser build, without changes or a source map.
The Local UI renderer in `markdown.js` escapes source HTML, allows only
HTTP(S) link destinations, and renders image alt text without fetching images.
A future dependency refresh must update the runtime, license and this record
from the same locked package and rerun the actual renderer tests.
