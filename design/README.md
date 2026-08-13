# Tray icon exploration

These boards explore replacements for the current colored circle:

- `tray-icon-concept-comparison.png` compares calendar, bell, and M/clock
  silhouettes.
- `tray-icon-calendar-states.png` develops the recommended calendar family in
  light and dark system-tray contexts.

## Recommendation

Use a small calendar-page silhouette with a state-specific interior glyph.
It says "meeting" more directly than the current circle and is less generic
than a notification bell.

| State | Shape | Color | Motion |
| --- | --- | --- | --- |
| Healthy | Calendar + check | Monochrome, derived from the host theme | None |
| Authentication attention | Calendar + keyhole/badge | Amber | None |
| Active meeting alert A | Solid calendar + exclamation | Red | Alternate every 500 ms |
| Active meeting alert B | White calendar on red disc | Red/white inverse | Alternate every 500 ms |

Shape and color both change, so the three states do not depend on color vision
alone. Keeping the healthy state monochrome also reduces ambient visual noise.

## Production notes

- Draw production icons deterministically rather than shipping the generated
  mock pixels.
- Target a 22 x 22 source to match the current implementation, but verify at
  16, 20, 22, 24, and 32 px and at common display scale factors.
- Keep a 2 px safe area and align major edges to whole pixels.
- Prefer a filled silhouette and knockout glyphs at the smallest sizes; thin
  outlines disappear on some tray backgrounds.
- Supply light and dark healthy variants if the host cannot template/tint the
  icon automatically.
- Keep amber and red as secondary cues. The check, keyhole/badge, exclamation,
  and inverted alert frame carry the semantic distinction.
- Preserve the existing 500 ms alert cadence, but honor reduced-motion where
  the platform exposes it; a steady red alert icon is the fallback.

The image-generation prompts asked for flat, pixel-aware, vector-style system
tray concepts; clear 16-22 px silhouettes; light and dark menu-bar previews;
calendar, bell, and M/clock comparisons; and then a refined calendar state set
with healthy, auth, and two alternating alert frames. Gradients, 3D effects,
decorative detail, and color-only state communication were explicitly avoided.
