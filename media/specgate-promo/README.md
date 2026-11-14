# SpecGate promotional video

This HyperFrames composition produces the short product tour linked from the
root README and embedded on the landing page. The 15.4-second tour is silent.
It shows the missing selected unit test, the agent’s rerun, and the developer’s
decision on the exact review.

See [DESIGN.md](DESIGN.md) for visual language, timing, and product-truth
constraints.

Scripts pin HyperFrames 0.8.137. Use Node.js 22 or newer, Chrome, and FFmpeg
(including FFprobe) for rendering. Run `npx --yes hyperframes@0.8.137 doctor`
to check the environment. Lint/runtime/layout checks alone do not prove that
video encoding works. Publishing uploads the project; validation does not need
an account or publication.

## Validate

```bash
npm test
npm run check
npx --yes hyperframes@0.8.137 inspect --samples 26 --strict
```

## Render

```bash
npm run render -- \
  --output ../../app/landing/media/specgate-promo.mp4 \
  --quality high \
  --fps 30 \
  --strict-all
```

After rendering, refresh the poster frame:

```bash
ffmpeg -y \
  -ss 14.8 \
  -i ../../app/landing/media/specgate-promo.mp4 \
  -frames:v 1 \
  -update 1 \
  -q:v 2 \
  ../../app/landing/media/specgate-promo-poster.jpg
```
