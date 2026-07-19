# SpecGate Landing Page

Standalone static landing page for introducing SpecGate, CLI-first IDE handoff,
artifact governance, and evidence-backed delivery review.

Design language: "the governed pipeline." The page keeps a dark control-room
canvas, indigo brand accent, semantic verdict colors, and one governance rail in
the How it works section. Reticle corners are reserved for the delivery-review
console and CLI terminal. Other sections use whitespace and sparse dividers so
the pipeline metaphor stays recognizable without repeating on every block.
Dark and light themes share the token set in `styles.css`; `landing.test.mjs`
is the executable gate for copy, accessibility, and structure.

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
- `script.js` - theme toggle, delivery review console, terminal carousel, and scroll interactions
- `logo.svg` - the branded header mark used in the top-left nav
- `fonts/` - self-hosted Host Grotesk and Commit Mono assets plus their OFL licenses

The terminal carousel demonstrates four current CLI value paths: publish an
artifact after an explicit preview, load an approved Context Pack, submit
delivery evidence with locally re-run checks and independent peer review before
the human delivery decision, and inspect observed governance signals. Keep
command shapes aligned with `specgate --help` and the user documentation. The
initial Publish frame is present in HTML for no-JavaScript use; `script.js`
enhances it with accessible tabs and plain-text playback.

The page keeps the public story compact: hero, compatibility, governed loop,
CLI demo, where-it-fits comparison, five-question FAQ, and CTA. The governed
loop should describe the real product boundary: OpenSpec, Spec Kit, Superpowers,
Markdown, and custom documents remain in their authoring systems; users map
their roles explicitly; SpecGate records approval for one immutable version and
hands it off as a pinned Context Pack. Avoid copy that implies SpecGate authors
the source spec, detects its framework, or replaces the IDE, tracker, or
repository.

Keep Lighthouse fixes grounded in the public page instead of browser extension
noise. Accessibility-critical controls should keep visible labels aligned with
accessible names, carousel tabs should preserve valid tablist roles, and muted
microcopy colors should maintain at least 4.5:1 contrast in both themes. The
Host Grotesk carries display and body text. Commit Mono is reserved for the CLI,
evidence, and compact metadata. Both are self-hosted and preloaded from `fonts/`.

## Checks

From the repository root:

```bash
node --test docs/release-readiness.test.mjs
node --test app/landing/landing.test.mjs
node -c app/landing/script.js
```

The page is intentionally dependency-free so it can be hosted as a static microsite or copied into the Vite app later.
