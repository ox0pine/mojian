# Colors and themes

**Colors.** One `appearance` preference (System/Light/Dark) picks the light or dark world; one `theme` preference names the ten color themes, Petal by default. `internal/theme/themes.go` builds both palettes of every theme once at start: `Window` is the window's `Colors`, `Terminal` the program palette, `UI` the MyGo widget theme, `EditorTheme` the code colors, and `PaintPreview` the Settings thumbnail. Petal keeps the palettes Mojian has always drawn; every other theme derives its surfaces and program colors from one tone.

## Source of truth

- [`internal/theme/theme.go`](../../internal/theme/theme.go) — `Colors`, `Radius`, the light and dark base palettes, `PaintBackground`, `RegisterFonts`.
- [`internal/theme/themes.go`](../../internal/theme/themes.go) — the ten themes: `DefaultID`, `Choices`, `Resolve`, `Dark`, `Window`, `Terminal`, `UI`, `EditorTheme`, `PaintPreview`.
- [`internal/theme/tone.go`](../../internal/theme/tone.go) — the OKLCH `tone` and `toneTheme` that derive a theme's surfaces and program colors from one hue.

## Verification

```sh
go test ./internal/theme
```
