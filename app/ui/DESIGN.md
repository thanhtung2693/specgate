# SpecGate UI Design

## Design Read

SpecGate is a developer-ops governance product for technical builders and review operators. The UI should feel like a calm control room: dense enough for repeated work, quiet enough for judgment, and explicit about governed next steps.

## Visual Direction

The app uses premium utilitarian minimalism: warm monochrome canvas, flat bordered surfaces, compact type, and restrained status color. It should feel more like an operational document than a marketing dashboard.

Use a density level of 7 for work surfaces: enough information to compare items quickly, but with clear row rhythm and no decorative weight.

## Palette

Use the landing-page palette as semantic app tokens, calibrated for a warmer product shell.

| Role | Dark | Light |
| --- | --- | --- |
| Background | `#010102` | `#fbfbfa` |
| Surface 1 | `#0f1011` | `#ffffff` |
| Surface 2 | `#141516` | `#f4f3ef` |
| Hairline | `#23252a` | `#e6e2da` |
| Primary | `#828fff` | `#5e6ad2` |
| Success | `#27a644` | `#1f9d3f` |
| Warning | `#e2a748` | `#c98a1e` |
| Failure | `#db5c57` | `#d24233` |

Status accents use washed-out fills only:

| State | Light fill | Light text |
| --- | --- | --- |
| Ready | `#edf3ec` | `#346538` |
| Needs attention | `#fbf3db` | `#956400` |
| Failed | `#fdebec` | `#9f2f2d` |
| Focus | `#eef0ff` | `#5e6ad2` |

## UX Principles

- Start on work, not marketing. The first screen is a usable Work queue.
- Keep platform governance separate from agent habits. The shell has primary surfaces for Work, Reviews, Artifacts, and Settings.
- Treat the work item as the shared center of gravity. Operators scan many work items; developers deep-link into one work item before returning to the CLI.
- Use small composable surfaces. Each route should become a vertical slice with its own data adapter and tests.
- Treat workspace and user as first-class context. The shell always shows the current workspace and local user.
- Keep the assistant present but not dominant. It opens as a compact modal from the shell header and can become a side panel on review/detail surfaces.
- Use composer context triggers for governed objects. Work items, artifacts, and skills should be inserted through structured trigger popovers rather than free-form copy.
- Keep Context Packs, gates, delivery, and skills discoverable in context instead of promoting each backend object to primary navigation.

## Component Rules

- shadcn-generated components stay in `src/components/ui/`.
- Product composition lives outside generated files.
- Use semantic tokens for color.
- Use `gap-*`, not `space-*`.
- Use icons in icon buttons and compact controls.
- Keep cards at 8px radius and avoid nested cards unless a repeated item needs framing.
- Use the landing-page logo for the product mark.
- Prefer borders, whitespace, and table rhythm over shadows.
- Keep primary actions charcoal-on-light or light-on-dark; avoid large colored blocks.
- Use mono type for IDs, commands, counts, and generated URIs only.
