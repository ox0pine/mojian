# Mojian website

The marketing site for [Mojian](https://github.com/ox0pine/mojian). This is the
only Node/Bun build in the repository; the desktop app remains Go and MyGo, and
nothing under `internal/` depends on it.

## Stack

- React 19 + TypeScript 7
- Vite 8
- Base UI (`@base-ui/react`) for the theme and mobile menus
- Hand-written CSS with the same tokens and layout as the official MyGo site:
  the `frame` rails, the `label` utility, a 56px sticky header, hairline grids
  and Geist / Geist Mono

Tailwind and TanStack are deliberately not used. The official MyGo site uses
them, but this page is a single static landing page; the CSS already carries the
design system and the live release lookup is a small effect.

## Commands

```sh
bun install
bun run dev       # http://127.0.0.1:5173
bun run build     # typecheck + dist/
bun run preview   # serve dist/
bun run typecheck
```

`bun.lock` is committed.

## Layout

| Path | Purpose |
|---|---|
| `index.html` | document shell, metadata, pre-paint theme script, `#root` |
| `src/App.tsx` | header, hero, stats, screenshot, features, files, downloads, footer |
| `src/components/Menus.tsx` | Base UI theme and mobile menus |
| `src/components/Icons.tsx` | inline SVG icons |
| `src/i18n-data.ts` | the four locale dictionaries |
| `src/i18n.ts` | language detection, title splitting |
| `src/theme.ts` | Light / Dark / System and persistence |
| `src/release.ts` | latest GitHub release lookup, static v0.4.3 fallback |
| `src/styles.css` | tokens, layout, responsive rules, reduced motion |
| `public/` | fonts, logo, screenshots, icons, social card; copied as-is |

Language is chosen from `?lang=`, then `localStorage`, then the browser. Theme is
chosen before first paint from `localStorage` or `prefers-color-scheme`; the
System option removes the stored choice.

## Deploy

```sh
bun run build
# deploy site/dist
```

`vite.config.ts` uses `base: "/"`. If the site is hosted under a subpath, set
`base` to that path and update the root-absolute public asset URLs in
`index.html` and `src/styles.css` (`/fonts/...`, `/logo.svg`, `/home-*.webp`) or
move those files into `src/assets/` so Vite hashes and rebases them.

The social card is `public/home.jpg`, 1200x630, referenced by `og:image` and the
JSON-LD `screenshot`.
