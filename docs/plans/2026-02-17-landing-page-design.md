# Landing Page Redesign: "Cold Launch"

## Vibe Spec

- **Name:** Cold Launch
- **Reference:** 60s NASA mission control + glacier
- **Emotion:** Confident + Inspired
- **Collision:** Mainframe printouts + telemetry readouts
- **Anti-patterns:** No low-code, no chatbot wrapper, no startup gradient slop

### Colors
| Role | Hex | Why |
|------|-----|-----|
| Primary | `#0B1821` | Deep glacial dark (mission control screens at night) |
| Secondary | `#1B3A4B` | Midnight ice blue (deep crevasse shadow) |
| Background | `#F4F1EC` | Aged continuous-feed paper |
| Accent | `#D4651A` | Amber telemetry warning (NASA status lights) |
| Text | `#1A1A18` | Printout ink black |
| Muted | `#8B9DAF` | Glacial blue-gray fog |
| Palette name | "Cold Launch" |

### Typography
- **Display:** Space Grotesk — geometric, engineered, mid-century modern
- **Mono:** IBM Plex Mono — mainframe printout authenticity
- **Body:** DM Sans (existing) — clean utility

### Layout
- Full-width, no nav rail on landing page
- Alternating dark/paper backgrounds between sections
- Horizontal "telemetry bands" as section connectors
- Sharp corners for data elements, gentle rounds for containers

### Motion
- Subtle and precise (nothing bouncy)
- Signature: teletype character-by-character text reveal
- Scroll-triggered stagger fades
- Dashed connector lines that draw themselves

### Wildcard
- Perforated tractor-feed paper edge detail on section dividers

## Approach: Full-Page Scroll Story (Approach A)

Replace the minimal home with a full-width marketing landing page. Nav rail disappears — full-width experience. Once you click into the app, nav rail returns.

### Routing Changes
- `GET /` — New landing page (full-width, no nav rail)
- `GET /projects` — Existing home with project list (nav rail layout)
- All other routes unchanged

## Sections

### 1. Hero
- Full-bleed dark background (#0B1821)
- Eyebrow (Mono, amber): `ONE BINARY. ENTIRE FACTORY.`
- Headline (Space Grotesk, ~4rem, white): `The Software Factory That Fits in Your Pocket`
- Subheadline (DM Sans, muted): `From plain-language prompt to production artifacts. Spec it, shape it, build it — one Go binary, zero infrastructure.`
- CTAs: "Launch a Pipeline" (amber) + "View on GitHub" (outline)
- Animated telemetry strip showing pipeline phases typing out
- Subtle vertical grid lines, faint glacial-blue radial glow

### 2. Pipeline Journey
- Paper background (#F4F1EC) with tractor-feed paper strip
- Four phase cards (01 SPEC → 02 EDIT → 03 BUILD → 04 ARTIFACTS)
- Connected by dashed lines (data bus)
- Each card: number (mono/amber), phase name, description, pulsing LED dot
- Stagger fade-in on scroll, dashed lines draw left-to-right

### 3. Stack Architecture
- Dark background (#0B1821)
- Three horizontal console tiers (bottom-up pyramid):
  - Layer 01 — LLM Client (widest): Unified SDK, three providers
  - Layer 02 — Agent Loop (middle): Agentic execution, tools, subagents
  - Layer 03 — Attractor Engine (top, narrowest): DAG runner, DOT graphs
- Amber border-left status bars
- Tiers slide in bottom-to-top sequentially

### 4. How It Works
- Paper background, three numbered steps
- 01 Install (`go install ...`)
- 02 Describe (plain-language prompt)
- 03 Build (validates, executes, streams, writes artifacts)
- Clean typography, no cards, horizontal desktop / vertical mobile

### 5. Live Feel
- Dark background, simulated build output
- Auto-scrolling teletype-style log lines
- Amber text on dark, typewriter timing
- ~15 lines that loop, labeled "LIVE TELEMETRY PREVIEW"

### 6. CTA
- Dark background, centered
- "Ready to launch?" + "One binary. Your prompt. Production artifacts."
- "Start Building" button (amber) + "Read the docs" link

### 7. Footer
- Darkest shade, minimal
- mammoth wordmark (mono) | GitHub, Docs, Setup | Built by 2389 Research
- Perforated edge decoration on top border

## Technical Implementation

### Files to Create
- `web/templates/landing.html` — Full landing page template (standalone, not using layout.html)
- `web/static/css/landing.css` — Landing page styles
- `web/static/js/landing.js` — Teletype animation, scroll reveals, telemetry simulation

### Files to Modify
- `web/server.go` — Route `/` to landing page, move project list to `/projects`
- `web/templates/layout.html` — Add font imports for Space Grotesk + IBM Plex Mono
- `web/static/css/tokens.css` — Add Cold Launch color tokens (landing-specific)

### Font Loading
- Google Fonts: Space Grotesk (display), IBM Plex Mono (mono)
- Keep existing DM Sans + DM Serif Display for the app interior
