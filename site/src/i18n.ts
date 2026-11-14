import { T } from "./i18n-data"

export { T }

export type Lang = keyof typeof T

/** Every locale has the same keys; values widen to string. */
export type Dict = { [K in keyof (typeof T)["en"]]: string }

export function tagOf(lang: Lang): string {
  return lang === "zh-Hans" ? "zh-CN" : lang
}

export function detectLang(): Lang {
  const q = new URLSearchParams(location.search).get("lang")
  if (q && q in T) return q as Lang
  try {
    const saved = localStorage.getItem("mojian:lang")
    if (saved && saved in T) return saved as Lang
  } catch {
    // storage can be blocked
  }
  for (const l of navigator.languages || [navigator.language]) {
    const v = (l || "").toLowerCase()
    if (/^zh-(tw|hk|mo|hant)/.test(v)) return "zh-Hant"
    if (v.startsWith("zh")) return "zh-Hans"
    if (v.startsWith("ja")) return "ja"
    if (v.startsWith("en")) return "en"
  }
  return "en"
}

/** The title in the dictionary is `line one<br><span>line two</span>`. */
export function titleParts(title: string): [string, string] {
  const [a, b = ""] = title.split("<br>")
  return [a, b.replace(/^<span>/, "").replace(/<\/span>$/, "")]
}
