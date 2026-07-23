# SpecGate promotional video

This HyperFrames composition produces the short product tour linked from the
root README. It is silent and caption-led so it works in a GitHub preview
without audio.

## Validate

```bash
npm run check
```

## Render

```bash
npm run render -- \
  --output ../../app/landing/media/specgate-promo.mp4 \
  --quality high \
  --fps 30
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
