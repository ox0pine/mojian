import { useEffect, useState } from "react"

export type Theme = "light" | "dark" | "system"

const KEY = "mojian:theme"
const QUERY = "(prefers-color-scheme: dark)"

export function readTheme(): Theme {
  try {
    const v = localStorage.getItem(KEY)
    return v === "light" || v === "dark" ? v : "system"
  } catch {
    return "system"
  }
}

export function applyTheme(theme: Theme): void {
  const dark = theme === "dark" || (theme === "system" && matchMedia(QUERY).matches)
  document.documentElement.dataset.theme = dark ? "dark" : "light"
  const meta = document.querySelector('meta[name="theme-color"]')
  if (meta) meta.setAttribute("content", dark ? "#0e0d0b" : "#fcfbf9")
}

function storeTheme(theme: Theme): void {
  try {
    if (theme === "system") localStorage.removeItem(KEY)
    else localStorage.setItem(KEY, theme)
  } catch {
    // storage can be blocked
  }
}

export function useTheme(): readonly [Theme, (theme: Theme) => void] {
  const [theme, setTheme] = useState<Theme>(readTheme)

  useEffect(() => {
    applyTheme(theme)
    if (theme !== "system") return
    const query = matchMedia(QUERY)
    const onChange = () => applyTheme("system")
    query.addEventListener("change", onChange)
    return () => query.removeEventListener("change", onChange)
  }, [theme])

  const choose = (next: Theme) => {
    storeTheme(next)
    setTheme(next)
  }

  return [theme, choose] as const
}
