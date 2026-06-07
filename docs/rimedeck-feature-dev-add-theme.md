# RimeDeck Theme Design: Hoarfrost / Moonlit Verse

> **Status**: Design  
> **Date**: 2026-06-06  
> **Scope**: Web + Desktop + Mobile

---

## 1. Design Philosophy

"Rime" carries two meanings — **雾凇** (hoarfrost, rime ice) and **韵律** (rhyme, rhythm). The theme system encodes both:

| Mode | Name | Chinese | Concept |
|------|------|---------|---------|
| Light | **Hoarfrost** | 霜凝 | Early-morning frost on branches. Crystalline whites, silver-blue mist, light filtering through ice — calm, transparent, precise. |
| Dark | **Moonlit Verse** | 月韵 | Deep winter night with moonlight on frost. Indigo sky, rhythmic constellations, the cadence of silence — contemplative, layered, flowing. |

### Guiding principles

1. **Subtlety over spectacle.** The current zinc palette (h ≈ 286, chroma ≈ 0.001–0.016) is neutral gray with a faint purple tint. We shift hue toward **230–250** (blue/ice direction) and raise chroma just enough to be perceptible (0.005–0.025). The result feels intentional, not garish.
2. **Perceptual uniformity.** Web/Desktop stay in OKLch; same-lightness surfaces produce equal visual contrast regardless of hue shift.
3. **Brand continuity.** The existing brand blue (`oklch(0.55 0.16 255)`) already sits on the ice-blue axis. We keep it, strengthening the connection between brand identity and the "rime" metaphor.
4. **Functional first.** Every token change must pass WCAG AA contrast (4.5:1 normal text, 3:1 large text / UI). Aesthetics don't override readability.
5. **Mobile stays HSL.** Mobile uses Tailwind v3.4 + NativeWind — no OKLch support. We translate the design intent into HSL equivalents, accepting minor perceptual drift.

---

## 2. Color Palette

### 2.1 Light Theme — "Hoarfrost" (霜凝)

Visual metaphor: frost on glass. Background carries a barely-perceptible blue, like breath crystallizing on a winter window. Surfaces layer like ice sheets — each tier gains a slightly deeper blue. Text is blue-tinged charcoal, not pure black, evoking winter shadows on snow.

| Token | Current (zinc h≈286) | Proposed (frost h≈240) | Delta |
|-------|---------------------|----------------------|-------|
| `--background` | `oklch(1 0 0)` | `oklch(0.988 0.005 235)` | Near-white → frost-white |
| `--foreground` | `oklch(0.141 0.005 285.823)` | `oklch(0.155 0.015 245)` | Black → winter shadow |
| `--card` | `oklch(1 0 0)` | `oklch(0.993 0.003 235)` | Pure white → breath-on-ice |
| `--card-foreground` | `oklch(0.141 0.005 285.823)` | `oklch(0.155 0.015 245)` | — |
| `--popover` | `oklch(1 0 0)` | `oklch(0.993 0.003 235)` | — |
| `--popover-foreground` | `oklch(0.141 0.005 285.823)` | `oklch(0.155 0.015 245)` | — |
| `--primary` | `oklch(0.21 0.006 285.885)` | `oklch(0.22 0.02 245)` | Zinc-900 → deep frost |
| `--primary-foreground` | `oklch(0.985 0 0)` | `oklch(0.985 0.004 235)` | — |
| `--secondary` | `oklch(0.967 0.001 286.375)` | `oklch(0.958 0.008 235)` | Zinc-100 → ice layer |
| `--secondary-foreground` | `oklch(0.21 0.006 285.885)` | `oklch(0.22 0.02 245)` | — |
| `--muted` | `oklch(0.967 0.001 286.375)` | `oklch(0.958 0.008 235)` | — |
| `--muted-foreground` | `oklch(0.552 0.016 285.938)` | `oklch(0.55 0.025 240)` | Gray → blue mist |
| `--accent` | `oklch(0.967 0.001 286.375)` | `oklch(0.948 0.012 230)` | — (slightly more chromatic) |
| `--accent-foreground` | `oklch(0.21 0.006 285.885)` | `oklch(0.22 0.02 245)` | — |
| `--destructive` | `oklch(0.577 0.245 27.325)` | `oklch(0.577 0.245 27.325)` | Unchanged (red is red) |
| `--border` | `oklch(0.92 0.004 286.32)` | `oklch(0.908 0.012 235)` | Gray line → frost edge |
| `--input` | `oklch(0.92 0.004 286.32)` | `oklch(0.908 0.012 235)` | — |
| `--ring` | `oklch(0.705 0.015 286.067)` | `oklch(0.68 0.04 240)` | Focus ring → icy blue |
| `--brand` | `oklch(0.55 0.16 255)` | `oklch(0.55 0.16 255)` | Unchanged — already ice-blue |
| `--brand-foreground` | `oklch(0.985 0 0)` | `oklch(0.985 0.004 235)` | — |
| `--success` | `oklch(0.55 0.16 145)` | `oklch(0.55 0.16 152)` | Slight shift toward teal-green (frost moss) |
| `--warning` | `oklch(0.75 0.16 85)` | `oklch(0.75 0.16 85)` | Unchanged |
| `--info` | `oklch(0.55 0.18 250)` | `oklch(0.55 0.18 245)` | Slight shift toward brand axis |
| `--chart-1` | `oklch(0.55 0.16 255)` | `oklch(0.55 0.16 250)` | Brand hue → frost blue |
| `--chart-2` | `oklch(0.66 0.13 255)` | `oklch(0.66 0.12 235)` | Step lighter, slightly more ice |
| `--chart-3` | `oklch(0.76 0.10 255)` | `oklch(0.76 0.09 220)` | Wider hue spread for rhythm |
| `--chart-4` | `oklch(0.85 0.06 255)` | `oklch(0.85 0.06 205)` | — |
| `--chart-5` | `oklch(0.92 0.03 255)` | `oklch(0.92 0.04 190)` | Lightest → frost-green tint |
| `--sidebar` | `oklch(0.985 0 0)` | `oklch(0.980 0.005 235)` | — |
| `--sidebar-foreground` | `oklch(0.141 0.005 285.823)` | `oklch(0.155 0.015 245)` | — |
| `--sidebar-primary` | `oklch(0.21 0.006 285.885)` | `oklch(0.22 0.02 245)` | — |
| `--sidebar-primary-foreground` | `oklch(0.985 0 0)` | `oklch(0.985 0.004 235)` | — |
| `--sidebar-accent` | `oklch(0.95 0.002 286.375)` | `oklch(0.945 0.010 235)` | — |
| `--sidebar-accent-foreground` | `oklch(0.21 0.006 285.885)` | `oklch(0.22 0.02 245)` | — |
| `--sidebar-border` | `oklch(0.92 0.004 286.32)` | `oklch(0.908 0.012 235)` | — |
| `--sidebar-ring` | `oklch(0.705 0.015 286.067)` | `oklch(0.68 0.04 240)` | — |
| `--scrollbar-thumb` | `oklch(0 0 0 / 10%)` | `oklch(0.30 0.03 240 / 12%)` | Tinted frost |
| `--scrollbar-thumb-hover` | `oklch(0 0 0 / 18%)` | `oklch(0.30 0.03 240 / 20%)` | — |
| `--scrollbar-track` | `transparent` | `transparent` | — |

### 2.2 Dark Theme — "Moonlit Verse" (月韵)

Visual metaphor: deep winter night. Background is not pure black but deep indigo — the color of a moonlit sky. Surfaces rise in lightness like layers of frost catching moonlight. The "verse/rhythm" metaphor is expressed through the harmonic spacing of lightness tiers and the chart palette's hue rotation (like notes in a scale).

| Token | Current (zinc h≈286) | Proposed (night h≈250) | Delta |
|-------|---------------------|----------------------|-------|
| `--background` | `oklch(0.18 0.005 285.823)` | `oklch(0.175 0.015 250)` | Dark gray → deep indigo |
| `--foreground` | `oklch(0.985 0 0)` | `oklch(0.975 0.005 235)` | White → frost-white |
| `--card` | `oklch(0.21 0.006 285.885)` | `oklch(0.205 0.018 250)` | — |
| `--card-foreground` | `oklch(0.985 0 0)` | `oklch(0.975 0.005 235)` | — |
| `--popover` | `oklch(0.21 0.006 285.885)` | `oklch(0.205 0.018 250)` | — |
| `--popover-foreground` | `oklch(0.985 0 0)` | `oklch(0.975 0.005 235)` | — |
| `--primary` | `oklch(0.92 0.004 286.32)` | `oklch(0.915 0.010 240)` | — |
| `--primary-foreground` | `oklch(0.21 0.006 285.885)` | `oklch(0.205 0.018 250)` | — |
| `--secondary` | `oklch(0.274 0.006 286.033)` | `oklch(0.265 0.018 250)` | Zinc-800 → moonlit layer |
| `--secondary-foreground` | `oklch(0.985 0 0)` | `oklch(0.975 0.005 235)` | — |
| `--muted` | `oklch(0.274 0.006 286.033)` | `oklch(0.265 0.018 250)` | — |
| `--muted-foreground` | `oklch(0.705 0.015 286.067)` | `oklch(0.68 0.025 240)` | — |
| `--accent` | `oklch(0.274 0.006 286.033)` | `oklch(0.275 0.022 245)` | Slightly more chromatic |
| `--accent-foreground` | `oklch(0.985 0 0)` | `oklch(0.975 0.005 235)` | — |
| `--destructive` | `oklch(0.704 0.191 22.216)` | `oklch(0.704 0.191 22.216)` | Unchanged |
| `--border` | `oklch(1 0 0 / 10%)` | `oklch(0.65 0.04 240 / 12%)` | White ghost → blue-frost edge |
| `--input` | `oklch(1 0 0 / 15%)` | `oklch(0.65 0.04 240 / 18%)` | — |
| `--ring` | `oklch(0.552 0.016 285.938)` | `oklch(0.55 0.06 245)` | — |
| `--chart-1` | `oklch(0.72 0.16 255)` | `oklch(0.72 0.16 250)` | Bright frost blue |
| `--chart-2` | `oklch(0.62 0.13 255)` | `oklch(0.65 0.12 230)` | — |
| `--chart-3` | `oklch(0.52 0.10 255)` | `oklch(0.58 0.10 210)` | Harmonic hue spread |
| `--chart-4` | `oklch(0.42 0.06 255)` | `oklch(0.50 0.08 190)` | — |
| `--chart-5` | `oklch(0.32 0.03 255)` | `oklch(0.42 0.06 170)` | Teal-green: aurora echo |
| `--sidebar` | `oklch(0.21 0.006 285.885)` | `oklch(0.195 0.018 250)` | — |
| `--sidebar-foreground` | `oklch(0.985 0 0)` | `oklch(0.975 0.005 235)` | — |
| `--sidebar-primary` | `oklch(0.488 0.243 264.376)` | `oklch(0.55 0.20 250)` | More ice-blue, less purple |
| `--sidebar-primary-foreground` | `oklch(0.985 0 0)` | `oklch(0.975 0.005 235)` | — |
| `--sidebar-accent` | `oklch(0.274 0.006 286.033)` | `oklch(0.265 0.018 250)` | — |
| `--sidebar-accent-foreground` | `oklch(0.985 0 0)` | `oklch(0.975 0.005 235)` | — |
| `--sidebar-border` | `oklch(1 0 0 / 10%)` | `oklch(0.65 0.04 240 / 12%)` | — |
| `--sidebar-ring` | `oklch(0.552 0.016 285.938)` | `oklch(0.55 0.06 245)` | — |
| `--brand` | `oklch(0.65 0.16 255)` | `oklch(0.65 0.16 250)` | Slightly warmer blue |
| `--brand-foreground` | `oklch(0.985 0 0)` | `oklch(0.975 0.005 235)` | — |
| `--success` | `oklch(0.65 0.15 145)` | `oklch(0.65 0.15 152)` | Teal-green shift |
| `--warning` | `oklch(0.70 0.16 85)` | `oklch(0.70 0.16 85)` | Unchanged |
| `--info` | `oklch(0.65 0.18 250)` | `oklch(0.65 0.18 245)` | — |
| `--scrollbar-thumb` | `oklch(1 0 0 / 8%)` | `oklch(0.65 0.04 240 / 10%)` | Blue-frost tint |
| `--scrollbar-thumb-hover` | `oklch(1 0 0 / 18%)` | `oklch(0.65 0.04 240 / 20%)` | — |
| `--scrollbar-track` | `transparent` | `transparent` | — |

### 2.3 Chart Palette — "Rhythmic Intervals" (韵律音程)

The chart colors embody the "rhythm" meaning of rime. Instead of a monochromatic blue stack, they form a **harmonic hue rotation** — like notes on a scale, spaced at perceptually uniform intervals across the cool spectrum:

```
Light:  250 → 235 → 220 → 205 → 190   (frost blue → ice → steel → ocean → teal)
Dark:   250 → 230 → 210 → 190 → 170   (moonlit blue → dawn → water → jade → aurora)
```

This gives charts visual rhythm — a sense of ordered progression rather than arbitrary color assignment. The hue spacing (15-20 degrees per step) mirrors a musical interval pattern.

---

## 3. "Frost Accent" System — Optional Enhancement

Beyond the base light/dark swap, introduce two accent colors that subtly reinforce the rime identity across interactive elements:

| Token | Value (Light) | Value (Dark) | Use |
|-------|--------------|-------------|-----|
| `--frost` | `oklch(0.85 0.06 220)` | `oklch(0.45 0.08 225)` | Hover states, selection highlights, active tab underline |
| `--aurora` | `oklch(0.75 0.10 175)` | `oklch(0.55 0.12 170)` | Success-adjacent feedback, online indicators, positive badges |

These are supplementary — consumed explicitly by specific UI surfaces (e.g., `bg-frost/10` for hover overlays) rather than replacing core semantic tokens. They give the theme a signature beyond what light/dark alone provides.

---

## 4. Mobile Translation (HSL)

Mobile stays on HSL. Approximate translations for the frost-tinted neutrals:

### 4.1 Mobile Light (`:root`)

```css
--background: 220 25% 98%;       /* frost-white */
--foreground: 225 20% 12%;       /* winter shadow */
--card: 220 20% 99%;
--card-foreground: 225 20% 12%;
--primary: 225 25% 15%;          /* deep frost */
--primary-foreground: 220 20% 98%;
--secondary: 220 18% 94%;        /* ice layer */
--secondary-foreground: 225 25% 15%;
--muted: 220 18% 94%;
--muted-foreground: 225 12% 50%; /* blue mist */
--accent: 218 22% 93%;
--accent-foreground: 225 25% 15%;
--border: 220 15% 88%;           /* frost edge */
--input: 220 15% 88%;
--ring: 225 18% 60%;             /* icy focus */
```

### 4.2 Mobile Dark (`.dark:root`)

```css
--background: 235 20% 11%;       /* deep indigo */
--foreground: 220 15% 96%;       /* frost-white */
--card: 235 22% 13%;
--card-foreground: 220 15% 96%;
--primary: 225 15% 90%;
--primary-foreground: 235 22% 13%;
--secondary: 235 20% 18%;        /* moonlit layer */
--secondary-foreground: 220 15% 96%;
--muted: 235 20% 18%;
--muted-foreground: 225 12% 62%;
--accent: 230 22% 19%;
--accent-foreground: 220 15% 96%;
--border: 225 14% 32%;           /* blue-frost edge */
--input: 225 14% 35%;
--ring: 230 18% 48%;
```

### 4.3 `lib/theme.ts` Mirror

Every CSS variable change must be mirrored in `apps/mobile/lib/theme.ts` per existing sync rule. The `NAV_THEME` object picks up the new `THEME.light.*` / `THEME.dark.*` values automatically.

---

## 5. landing-light Override

`apps/web/app/custom.css` contains a `.landing-light` class that force-pins light-mode tokens for the landing page. This must be updated in lockstep with the new `:root` values so landing components using semantic tokens (`--background`, `--border`, etc.) stay in sync with the hoarfrost palette.

---

## 6. WindowMockup Preview Update

`packages/views/settings/components/preferences-tab.tsx` renders a `WindowMockup` with hardcoded hex colors (`LIGHT_COLORS`, `DARK_COLORS`). These should be updated to reflect the frost/indigo tints:

```ts
const LIGHT_COLORS = {
  titleBar: "#e0e5ec",   // frost-tinted gray (was #e8e8e8)
  content: "#f8f9fc",    // frost-white (was #ffffff)
  sidebar: "#eef1f6",    // ice layer (was #f4f4f5)
  bar: "#d5dbe5",        // frost edge (was #e4e4e7)
  barMuted: "#c2cada",   // mist (was #d4d4d8)
};

const DARK_COLORS = {
  titleBar: "#2a2d3a",   // moonlit surface (was #333338)
  content: "#1e2132",    // deep indigo (was #27272a)
  sidebar: "#171a28",    // night sky (was #1e1e21)
  bar: "#363a4d",        // moonlit edge (was #3f3f46)
  barMuted: "#484d64",   // starlight (was #52525b)
};
```

---

## 7. Implementation Plan

### Phase 1 — Token swap (1 PR, no structural changes)

Files to modify:
1. `packages/ui/styles/tokens.css` — `:root` and `.dark` blocks
2. `apps/web/app/custom.css` — `.landing-light` block
3. `apps/mobile/global.css` — `:root` and `.dark:root` blocks
4. `apps/mobile/lib/theme.ts` — `THEME.light` and `THEME.dark` objects
5. `packages/views/settings/components/preferences-tab.tsx` — `LIGHT_COLORS` / `DARK_COLORS`

**Not touched**: `tailwind.config.js` (mobile), `theme-provider.tsx`, `globals.css` (web/desktop) — the variable-mapping layer is unchanged, only the values change.

### Phase 2 — Frost accent tokens (optional, separate PR)

1. Add `--frost` and `--aurora` to `tokens.css` and `global.css`
2. Map to Tailwind utilities (`@theme inline` for web; `tailwind.config.js` for mobile)
3. Apply to specific surfaces: sidebar hover, active indicators, presence dots

### Phase 3 — Contrast audit & tuning

1. Run automated WCAG contrast checks against all token pairs
2. Tune specific values where contrast falls below 4.5:1
3. Test with macOS "Increase contrast" accessibility setting
4. Visual QA: sidebar, issue detail, comment bubbles, code blocks, charts

---

## 8. Contrast Budget

Key pairs to verify (foreground on background):

| Pair | Target | Notes |
|------|--------|-------|
| `--foreground` on `--background` | ≥ 7:1 (AAA) | Body text — must be flawless |
| `--muted-foreground` on `--background` | ≥ 4.5:1 (AA) | Secondary text — the weakest link |
| `--muted-foreground` on `--muted` | ≥ 4.5:1 (AA) | Text on chip/badge backgrounds |
| `--primary-foreground` on `--primary` | ≥ 4.5:1 (AA) | Button text |
| `--brand-foreground` on `--brand` | ≥ 4.5:1 (AA) | Brand buttons |
| `--card-foreground` on `--card` | ≥ 7:1 (AAA) | Card body text |
| `--border` vs `--background` | ≥ 3:1 (AA graphics) | Border visibility |

The proposed palette is designed to meet these; phase 3 is the verification pass.

---

## 9. Design Rationale — Why This Works

### The frost connection (雾凇)

Hoarfrost forms when supercooled water vapor deposits directly onto surfaces as ice crystals. The visual quality is:
- **Translucent, not opaque** — you see through frost, just with a blue-white filter. Our backgrounds are not opaque white or black; they carry a transparent blue tint.
- **Layered** — frost builds in layers on branches. Our surface elevation tiers (background → card → secondary → surface-1 → surface-2) each gain slightly more blue chroma as they rise, like accumulating ice.
- **Edge-defined** — individual ice crystals have sharp facets. Our borders use higher chroma than surfaces, creating crisp delineation.

### The rhythm connection (韵律)

Musical rhythm is about intervals — the spacing between beats creates pattern. We express this through:
- **Lightness cadence** — the elevation tiers are spaced at perceptually uniform intervals (each ~5% L apart), creating a visual rhythm in nested surfaces.
- **Harmonic hue rotation** — chart colors step through the cool spectrum at regular intervals (15-20 deg), like notes in a scale. Light mode ascends; dark mode does the same but shifted (moonlight refracts differently than daylight).
- **Consistent chroma pulse** — neutral tokens carry a faint blue heartbeat (chroma 0.005–0.025). It's below conscious notice but above "generic gray." The UI feels unified without anyone knowing why.

### Why not a dramatic departure?

A rime-themed UI could go maximalist — deep blues, visible frost textures, aurora gradients. That would be beautiful as a concept render and painful as a daily-driver productivity tool. Rimedeck is an issue tracker; users stare at it for hours. The theme must:
- Not compete with content (issue cards, comments, code blocks)
- Not fatigue the eye over long sessions
- Not break third-party content (embedded images, markdown, avatars)

Our approach is "felt, not seen" — the frost/rhythm identity is encoded in the structure of the palette, not its intensity. A user might describe the app as "clean" or "calm" without consciously noticing the blue. That's the goal.

---

## 10. Migration & Rollback

- **Zero structural changes.** Only CSS variable values change. No new providers, no new context, no new build config.
- **Instant rollback.** Revert the one token file per platform to restore the current zinc palette.
- **A/B-safe.** If we later want to offer "Zinc Classic" as a user-selectable palette alongside Hoarfrost/Moonlit Verse, the infrastructure is already there — `next-themes` supports arbitrary themes via `data-theme` attributes. But that's a future enhancement, not part of this PR.
