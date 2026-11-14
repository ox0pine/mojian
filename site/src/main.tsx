import { StrictMode } from "react"
import { createRoot } from "react-dom/client"

import App from "./App"
import { detectLang, T, tagOf } from "./i18n"
import "./styles.css"

const lang = detectLang()
document.documentElement.lang = tagOf(lang)
document.title = T[lang].doc
document.querySelector('meta[name="description"]')?.setAttribute("content", T[lang].desc)

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <App initialLang={lang} />
  </StrictMode>,
)
