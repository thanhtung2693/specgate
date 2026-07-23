# SpecGate promotional video design

## Design read

A silent 15-second README teaser for developers evaluating an AI delivery
guardrail. The visual language is an editorial developer tool with one
continuous evidence trace, not a dashboard tour or presentation.

## Style

- Near-black canvas, warm charcoal product surface, precise hairlines.
- SpecGate indigo indicates claims and human control.
- Amber identifies one unresolved evidence gap.
- Green is reserved for reproduced evidence and accepted delivery.
- One persistent product surface and one focal concept per beat.
- Asymmetric, oversized narrative typography above a compact operational view.
- Square evidence callouts and decisions inside one gently rounded product
  frame. No grid of floating cards.

## Typography

- Narrative and product copy: Host Grotesk, 400–800.
- Commands, criteria identifiers, and evidence values: Commit Mono, 400–700.

## Story and timing

1. `0.0–1.5s`: the coding agent reports both acceptance criteria complete.
2. `1.5–4.4s`: SpecGate checks delivery evidence. AC-01 reproduces, while AC-02
   stops the trace at `expected 503 · received 500`.
3. `4.4–9.1s`: an explicitly requested peer review identifies
   `return 500 → return 503`; the human requests focused changes.
4. `9.1–12.1s`: the revision repairs AC-02 and returns the work for human
   acceptance.
5. `12.1–15.4s`: the human accepts, the receipt becomes `Delivered`, and the
   video ends after a short resolved hold.

## Motion

- State changes happen on one persistent evidence lane.
- A runner moves from claim through each criterion to human acceptance.
- The line stops at the failing criterion and continues only after the
  revision.
- Peer review attaches to the failed criterion instead of replacing the whole
  layout.
- Human actions remain explicit terminal commands.
- The final outcome holds for less than one second. There is no separate outro
  or fade tail.

## Product truth

- SpecGate detects the evidence gap; it does not claim to find the code defect.
- A requested peer review identifies the implementation gap.
- The human requests changes and later accepts the revised delivery.
- SpecGate retains the delivery evidence and human decision.
- No model judgment or capability is implied beyond the current product.

## What not to do

- No decorative dashboard, card grid, or duplicated status table.
- No artifact IDs, JSON output, file paths, Git receipts, or repository URL.
- No generic neon treatment or excessive brand-purple decoration.
- No approval language before the human decision.
- No long static ending.
