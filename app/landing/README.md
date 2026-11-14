# SpecGate Landing Page

Standalone static landing page for SpecGate Local. It introduces approved
agent handoff, per-criterion delivery evidence, and developer acceptance for
solo developers and small teams.

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
`pages` GitHub Actions workflow. Pull requests that change the landing page,
promo sources, or workflow validate the dependency-free page. Matching `main`
pushes upload `app/landing` and deploy it to the `github-pages` environment.
Use the manual workflow trigger to redeploy unchanged landing-page files.

## Files

- `index.html` - page structure and product copy
- `styles.css` - stylesheet entry point for the focused files under `styles/`
- `script.js` - theme toggle, delivery review console, terminal carousel, and scroll interactions
- `logo.svg` - the branded header mark used in the top-left nav
- `fonts/` - self-hosted Host Grotesk and Commit Mono assets plus their OFL licenses

The terminal carousel shows local artifact publication after preview, resuming
with approved Context Pack documents, observing a missing selected-test
result, and reading an eligible bounded Git handoff. Keep commands aligned
with `specgate --help` and the user documentation. Use the returned scaffold
path and the exact review ID for Local acceptance. The examples are abbreviated
illustrations. The initial Publish frame is available without JavaScript.

The hero and tour illustrate a unit-test suite that exits zero while omitting
the agreed 503 case. The [selected-test walkthrough](../../docs/using-specgate/guides/observe-selected-tests.md)
uses the same health-status function and retains watched-input drift after the
agent adds the case. The
[delivery proof](../../docs/using-specgate/guides/try-delivery-proof.md) uses a
small name-normalization example and observes command results. Both leave the
acceptance decision with the developer. Refresh the landing MP4 and poster by
following the [HyperFrames render guide](../../media/specgate-promo/README.md).

The page covers the entry path, tool compatibility, Local workflow, product
tour, CLI examples, and FAQ. Preserve the authoring and execution boundaries.
In the versioned artifact route, OpenSpec, Spec Kit, Superpowers,
Markdown, and custom documents remain in their authoring systems; users map
their roles explicitly; SpecGate records approval for one immutable version and
hands it off as a pinned Context Pack. Avoid copy that implies SpecGate authors
the source spec, detects its framework, or replaces the IDE, tracker, or
repository.

The compatibility section explains IDE handoff and optional Local browser
review. A teammate can use a bounded Git handoff to read the request beside the
diff. The originating Local store records the human decision.

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

The page is intentionally dependency-free so GitHub Pages can host it directly.
