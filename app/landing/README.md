# SpecGate Landing Page

Standalone static landing page for introducing SpecGate, CLI-first IDE handoff,
artifact governance, and post-build verification.

## Preview

From the repository root:

```bash
python3 -m http.server 4177 --directory app/landing
```

Open `http://127.0.0.1:4177`.

## GitHub Pages

The repository publishes this static landing page from `app/landing` with the
`pages` GitHub Actions workflow. The workflow runs on every `main` push and can
also be started manually. It intentionally avoids path filters so release squash
or force-push workflows cannot leave the public site stale. Each run validates
the dependency-free page, uploads `app/landing` as the Pages artifact, and
deploys it to the `github-pages` environment.

## Files

- `index.html` - page structure and product copy
- `styles.css` - design-system tokens and responsive layout based on `ui/DESIGN.md`
- `script.js` - theme toggle, verification console, terminal carousel, and scroll interactions
- `logo.svg` - the branded header mark used in the top-left nav

The terminal carousel uses the same CLI-first handoff sequence documented for
coding agents: list ready work, fetch the Context Pack, run gates, report
completion evidence, review delivery, and inspect delivery status. Output lines
are abbreviated for the landing page, but command shapes should stay aligned
with the real CLI. Carousel line strings are typed with `textContent`, so keep
them as plain text and use the line kind for visual emphasis instead of inline
HTML. The carousel segment controls are generated from the demo data in
`script.js`; do not hand-maintain a separate set of progress buttons in the
HTML.

The page keeps the public story compact: hero, governed loop, CLI demo, where-it-fits,
FAQ, and CTA. The governed loop should describe the real product boundary:
flexible artifact documents are mapped to declared roles, versioned, gated by a
profile, and handed off as a pinned Context Pack. Avoid copy that implies
SpecGate magically understands every document or replaces the IDE, tracker, or
repository.

## Checks

From the repository root:

```bash
node --test docs/release-readiness.test.mjs
node --test app/landing/landing.test.mjs
node -c app/landing/script.js
```

The page is intentionally dependency-free so it can be hosted as a static microsite or copied into the Vite app later.
