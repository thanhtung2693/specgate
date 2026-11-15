# SpecGate Landing Page

This static page introduces SpecGate Local for solo developers and small
teams. The hero draws the gate from the SpecGate logo around three sheets:
approved scope, the agent's code, and results per criterion. A single agent
claim splits into those results. The evidence sheet turns toward the reader
while the scope and code remain in depth behind it. The last step reveals the
decision actions. The example shows a successful suite with the selected 503 test
result missing. The developer makes the delivery decision.

The hero plays once. Its story tabs support arrow keys, Home and End. The
play control pauses the phase timer and in-progress transitions, and replay
starts from the first phase. Reduced motion and JavaScript-disabled visits
start with the complete review. Keep the install command and documentation
links usable when scripting is unavailable.

The page uses Host Grotesk for English display text, Geist for body and
Vietnamese display text, and Commit Mono for code and evidence. The fonts,
including the Geist Vietnamese subset, and their OFL licenses are in
`fonts/`. The warm light and graphite dark themes share color tokens. The
hero and closing band use the dark palette in both themes.

## Content

Keep commands aligned with the [Quickstart](../../docs/using-specgate/quickstart.md)
and [IDE setup guide](../../docs/using-specgate/guides/install-ide-plugins.md).
The install selector shows the Local CLI, Codex and Claude Code marketplace
setup, project-local Cursor setup via the SpecGate CLI, and a copyable prompt
for the agent. The icon-only choices carry names in their accessible labels and
tooltips. The selector uses the Codex app icon bundled with OpenAI's ChatGPT
desktop app, the Claude star from the [official Claude site](https://claude.com/),
and both Cursor Cube 2D theme variants from the
[Cursor brand kit](https://cursor.com/brand).
The CLI owns Local scope, evidence, and decisions. The
[Local review guide](../../docs/using-specgate/guides/local-review.md) explains
the optional browser opened by `specgate ui`. The public hero is an
illustration. The setup link starts with the CLI and the demo link opens a
review with sample data.

The four-step workflow below the tool lane links to project rubrics, work
resume, selected-test verification, and optional whole-store Local transfer.
The links stay readable as the connectors draw in sequence when the strip
first enters view. The icons respond to hover and keyboard focus. Reduced
motion shows the full strip immediately.
Spec authors keep their tools and source documents. Coding agents implement
in the repository. SpecGate records the approved scope and evidence so the
developer can accept or request changes. Keep the selected-test example
consistent with the
[selected-test walkthrough](../../docs/using-specgate/guides/observe-selected-tests.md).

The public demo at `demo/` uses the Local review presentation with sample
data. Its guided tour opens the 503 evidence, shows the cited code diff, and
leads to the Decision tab. Skip or restart the tour at any time. Restarting the
tour keeps the current sample and any draft. **Reset demo** starts a fresh review.
It runs in the browser without using a real repository, Local store, or
account. The original `#delivery-proof` anchor opens the browser-review FAQ,
which links to both the demo and the guide for real work.

## Preview and check

Build the demo assets and serve the landing directory from the repository root:

```bash
node scripts/build-landing-demo.mjs
python3 -m http.server 4177 --directory app/landing
node --test app/landing/landing.test.mjs app/landing/demo/demo.test.mjs docs/release-readiness.test.mjs
node -c app/landing/script.js
```

Open `http://127.0.0.1:4177` and follow **Try the UI demo**. Check desktop
and narrow layouts in both languages, with light and dark themes. Test the
landing player and demo using the keyboard, then check reduced motion. With
JavaScript disabled, the landing shows the final review and CLI setup links.
In the demo, check the tour's Next, Back, Skip and Restart controls, including
after opening the revised sample. Walk through a decision separately.

## GitHub Pages

The `pages` GitHub Actions workflow validates landing changes on pull
requests. Matching `main` pushes upload `app/landing` to the
`github-pages` environment. The manual workflow trigger redeploys the
current landing files. The demo build uses Node.js and repository assets.
The published site is static.
