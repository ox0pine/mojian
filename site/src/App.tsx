import { type ReactNode, useEffect, useState } from "react"

import { asset, LATEST, RELEASES, REPO, useRelease, type AssetKey, type Release } from "./release"
import { T, tagOf, titleParts, type Dict, type Lang } from "./i18n"
import { useTheme, type Theme } from "./theme"
import { MobileMenu, ThemeMenu } from "./components/Menus"
import {
  AppleIcon,
  ArrowRightIcon,
  DownloadIcon,
  GitHubIcon,
  LinuxIcon,
  WindowsIcon,
} from "./components/Icons"

const NAV = [
  ["navFeatures", "#features"],
  ["navFiles", "#files"],
  ["navDownload", "#download"],
] as const

const STATS = [
  ["statWebview", "0"],
  ["statCgo", "0"],
  ["statBinary", "1"],
  ["statPlatform", "3"],
] as const

const FEATURES = [
  ["f3Title", "f3Body", "remote-projects", "terminal"],
  ["f4Title", "f4Body", "writing-and-previewing", "preview"],
  ["f5Title", "f5Body", "git", "git"],
  ["f6Title", "f6Body", "settings", "themes"],
] as const

type FeatureKind = (typeof FEATURES)[number][3]

type Platform = {
  name: string
  tag: string
  req: keyof Dict
  icon: ReactNode
  primary: AssetKey
  primaryLabel: keyof Dict
  sub: { key: AssetKey; label: keyof Dict }[]
}

const PLATFORMS: Platform[] = [
  {
    name: "macOS",
    tag: "arm64",
    req: "macReq",
    icon: <AppleIcon />,
    primary: "mac-arm64",
    primaryLabel: "dlMac",
    sub: [{ key: "mac-amd64", label: "dlIntel" }],
  },
  {
    name: "Linux",
    tag: "amd64",
    req: "linuxReq",
    icon: <LinuxIcon />,
    primary: "linux-amd64",
    primaryLabel: "dlLinux",
    sub: [
      { key: "linux-arm64", label: "dlArm" },
      { key: "linux-amd64-rpm", label: "dlRpm" },
    ],
  },
  {
    name: "Windows",
    tag: "x64",
    req: "winReq",
    icon: <WindowsIcon />,
    primary: "windows-amd64",
    primaryLabel: "dlWin",
    sub: [{ key: "windows-arm64", label: "dlWinArm" }],
  },
]

const POINTS = ["point1", "point2", "point3"] as const

const THEME_COLORS = [
  ["Petal", "#e7b7bd"],
  ["Mist", "#aabcc2"],
  ["Meadow", "#a8b89c"],
  ["Dusk", "#8d879e"],
  ["Tide", "#7ea5aa"],
  ["Apricot", "#dfa47f"],
  ["Aurora", "#8fa3c4"],
  ["Dune", "#c6ae86"],
  ["Paper", "#ded8ca"],
  ["Plain", "#a5a5a0"],
] as const

function Header({
  t,
  lang,
  onLang,
  theme,
  onTheme,
}: {
  t: Dict
  lang: Lang
  onLang: (lang: Lang) => void
  theme: Theme
  onTheme: (theme: Theme) => void
}) {
  return (
    <header className="site-header">
      <div className="bar shell">
        <a className="brand" href="#top" aria-label="Mojian">
          <span className="brand-mark" aria-hidden="true" />
        </a>
        <nav className="main-nav" aria-label="Primary">
          {NAV.map(([key, href]) => (
            <a key={key} href={href}>
              {t[key]}
            </a>
          ))}
        </nav>
        <div className="nav-end">
          <select
            className="lang-select"
            id="lang"
            aria-label={t.langLabel}
            value={lang}
            onChange={(e) => onLang(e.target.value as Lang)}
          >
            <option value="zh-Hans">简体中文</option>
            <option value="zh-Hant">繁體中文</option>
            <option value="en">English</option>
            <option value="ja">日本語</option>
          </select>
          <ThemeMenu t={t} theme={theme} onTheme={onTheme} />
          <a className="icon-btn github-link" href={REPO} aria-label="GitHub">
            <GitHubIcon />
          </a>
          <MobileMenu t={t} />
        </div>
      </div>
    </header>
  )
}

function Hero({ t, release }: { t: Dict; release: Release }) {
  const [first, second] = titleParts(t.title)
  return (
    <section className="hero shell">
      <div className="hero-copy">
        <a className="version hero-enter" href={LATEST}>
          <span className="dot" aria-hidden="true" />
          <b>v{release.version}</b>
          <span>{t.badge}</span>
        </a>
        <h1 className="hero-enter">
          {first}
          <span>{second}</span>
        </h1>
        <p className="lede hero-enter">{t.lede}</p>
        <div className="actions hero-enter">
          <a className="btn btn-primary" href="#download">
            <DownloadIcon />
            <span>{t.ctaDownload}</span>
          </a>
          <a className="btn btn-secondary" href={REPO}>
            <GitHubIcon />
            <span>{t.ctaGithub}</span>
          </a>
        </div>
      </div>
    </section>
  )
}

function Proof({ t }: { t: Dict }) {
  return (
    <section className="proof shell" aria-label={t.proofCaption}>
      <figure className="shot">
        <img
          id="shot"
          src="/home-1000.webp"
          srcSet="/home-1000.webp 1000w, /home-1466.webp 1466w"
          sizes="(max-width: 1280px) calc(100vw - 80px), 1120px"
          width={1466}
          height={919}
          fetchPriority="high"
          decoding="async"
          alt={t.alt}
        />
      </figure>
      <p className="caption">{t.proofCaption}</p>
    </section>
  )
}

function Stats({ t }: { t: Dict }) {
  return (
    <dl className="stats shell">
      {STATS.map(([label, value]) => (
        <div className="stat" key={label}>
          <dt>{t[label]}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  )
}

function WorkspaceVisual() {
  return (
    <div className="workspace-demo" aria-hidden="true">
      <div className="workspace-tabs">
        <span className="active">notes.md</span>
        <span>Terminal</span>
        <span className="tab-plus">+</span>
      </div>
      <div className="workspace-body">
        <div className="workspace-files">
          <span className="demo-label">EXPLORER</span>
          <span>Ideas/</span>
          <span className="selected">notes.md</span>
          <span>research.pdf</span>
          <span>assets/</span>
          <span>README.md</span>
        </div>
        <div className="workspace-editor">
          <span className="editor-kicker"># Field notes</span>
          <span className="editor-title">A quieter place<br />for words and work.</span>
          <span className="editor-line" />
          <span className="editor-line short" />
          <span className="editor-line" />
          <span className="editor-caret" />
        </div>
        <div className="workspace-terminal">
          <span className="demo-label">TERMINAL</span>
          <span><i>$</i> git status</span>
          <span className="terminal-success">On branch main</span>
          <span>nothing to commit</span>
          <span><i>$</i> <b className="terminal-caret" /></span>
        </div>
      </div>
    </div>
  )
}

function TerminalVisual() {
  return (
    <div className="terminal-demo" aria-hidden="true">
      <div className="terminal-head">
        <span><i /> SSH · studio</span>
        <span>zsh</span>
      </div>
      <div className="terminal-copy">
        <span><i>$</i> mojian ~/notes</span>
        <span className="terminal-muted">Connected. Session restored.</span>
        <span><i>$</i> git switch writing</span>
        <span className="terminal-success">Switched to branch 'writing'</span>
        <span><i>$</i> <b className="terminal-caret" /></span>
      </div>
    </div>
  )
}

function PreviewVisual() {
  return (
    <div className="preview-demo" aria-hidden="true">
      <div className="image-page">
        <svg viewBox="0 0 200 130">
          <rect width="200" height="130" rx="5" />
          <circle cx="151" cy="32" r="14" />
          <path d="M0 112 58 55l39 35 29-25 74 65H0Z" />
        </svg>
        <span>landscape.png</span>
      </div>
      <div className="pdf-page">
        <div className="pdf-sheet">
          <b>PDF</b>
          <i />
          <i />
          <i />
          <i />
        </div>
        <span>reference.pdf</span>
      </div>
    </div>
  )
}

function GitVisual() {
  return (
    <div className="diff-demo" aria-hidden="true">
      <div className="diff-head"><span>notes.md</span><span>+4 −2</span></div>
      <div className="diff-row muted"><span>12</span><span>## Morning pages</span></div>
      <div className="diff-row removed"><span>13</span><span>- Make room for more tools.</span></div>
      <div className="diff-row added"><span>13</span><span>+ Make room for more words.</span></div>
      <div className="diff-row muted"><span>14</span><span>&nbsp;</span></div>
      <div className="diff-row added"><span>15</span><span>+ Keep the work in reach.</span></div>
    </div>
  )
}

function ThemesVisual() {
  return (
    <div className="themes-demo" aria-hidden="true">
      {THEME_COLORS.map(([name, color]) => (
        <div className="theme-swatch" key={name}>
          <span style={{ backgroundColor: color }} />
          <small>{name}</small>
        </div>
      ))}
    </div>
  )
}

function FeatureVisual({ kind }: { kind: FeatureKind }) {
  if (kind === "terminal") return <TerminalVisual />
  if (kind === "preview") return <PreviewVisual />
  if (kind === "git") return <GitVisual />
  return <ThemesVisual />
}

function Features({ t }: { t: Dict }) {
  return (
    <section className="section shell" id="features">
      <div className="section-head feature-section-head">
        <div>
          <p className="eyebrow">{t.featuresEyebrow}</p>
          <h2>{t.featuresTitle}</h2>
        </div>
        <p className="section-lede">{t.featuresLede}</p>
      </div>

      <a className="workspace-feature" href={`${REPO}#tabs-panes-and-terminals`}>
        <div className="workspace-copy">
          <span className="feature-index">01 / 05</span>
          <h3>{t.f2Title}</h3>
          <p>{t.f2Body}</p>
          <span className="feature-arrow"><ArrowRightIcon /></span>
        </div>
        <div className="workspace-visual"><WorkspaceVisual /></div>
      </a>

      <div className="feature-grid">
        {FEATURES.map(([title, body, anchor, kind], index) => (
          <article className={`feature feature-${kind}`} key={title}>
            <a className="feature-link" href={`${REPO}#${anchor}`}>
              <div className="feature-visual"><FeatureVisual kind={kind} /></div>
              <div className="feature-content">
                <span className="feature-index">0{index + 2}</span>
                <h3>
                  <span>{t[title]}</span>
                  <ArrowRightIcon />
                </h3>
                <p>{t[body]}</p>
              </div>
            </a>
          </article>
        ))}
      </div>
    </section>
  )
}

function Files({ t }: { t: Dict }) {
  return (
    <section className="files-wrap" id="files">
      <div className="files-panel shell">
        <div className="files-copy">
          <p className="eyebrow">{t.filesEyebrow}</p>
          <h2>{t.filesTitle}</h2>
          <p className="section-lede">{t.filesBody}</p>
          <ul className="points">
            {POINTS.map((key) => (
              <li key={key}>
                <Check />
                <span>{t[key]}</span>
              </li>
            ))}
          </ul>
        </div>
        <div className="files-art">
          <div className="tree-card">
            <div className="tree-head">
              <span>{t.treeLabel}</span>
              <span className="tag">notes/</span>
            </div>
            <pre className="tree" aria-label={t.treeLabel}>
              <span className="dir">notes/</span>{"\n"}
              {"├── "}<span className="dir">Ideas/</span>{"\n"}
              {"│   └── "}<span className="file">Quiet mornings.md</span>{"\n"}
              {"├── "}<span className="dir">scripts/</span>{"\n"}
              {"│   ├── "}<span className="file">app.js</span>{"\n"}
              {"│   └── "}<span className="file">tasks.py</span>{"\n"}
              {"├── "}<span className="file">Reading notes.md</span>{"\n"}
              {"└── "}<span className="file">Welcome.md</span>
            </pre>
          </div>
          <p className="tree-caption">{t.treeCaption}</p>
        </div>
      </div>
    </section>
  )
}

function Downloads({ t, release }: { t: Dict; release: Release }) {
  return (
    <section className="section downloads shell" id="download">
      <div className="section-head">
        <div>
          <p className="eyebrow">{t.downloadsEyebrow}</p>
          <h2>{t.downloadsTitle}</h2>
          <p className="section-lede">{t.downloadsLede}</p>
        </div>
        <a className="head-link" href={RELEASES}>
          <span>{t.dlAll}</span>
          <ArrowRightIcon />
        </a>
      </div>
      <div className="download-list">
        {PLATFORMS.map((platform) => (
          <article className="platform-card" key={platform.name}>
            <div className="platform-head">
              {platform.icon}
              <h3>{platform.name}</h3>
              <span className="tag">{platform.tag}</span>
            </div>
            <p>{t[platform.req]}</p>
            <div className="platform-actions">
              <a
                className="btn btn-secondary"
                href={asset(release, platform.primary)}
                data-download={platform.primary}
              >
                <span>{t[platform.primaryLabel]}</span>
                <DownloadIcon />
              </a>
              <div className="sub-links">
                {platform.sub.map((sub) => (
                  <a key={sub.key} href={asset(release, sub.key)} data-download={sub.key}>
                    {t[sub.label]}
                  </a>
                ))}
              </div>
            </div>
          </article>
        ))}
      </div>
      <p className="download-note">{t.downloadNote}</p>
    </section>
  )
}

function Footer({ t, release }: { t: Dict; release: Release }) {
  return (
    <footer className="site-footer">
      <div className="footer-row shell">
        <a className="brand footer-brand" href="#top" aria-label="Mojian">
          <span className="brand-mark" aria-hidden="true" />
        </a>
        <div className="footer-meta">
          <span>Mojian <b>v{release.version}</b> · <a href={`${REPO}/blob/main/LICENSE`}>{t.footerLicense}</a></span>
          <nav className="footer-links" aria-label="Footer">
            <a href={REPO}>GitHub</a>
            <a href={RELEASES}>{t.navDownload}</a>
            <a href="https://mygo.run">{t.footerBuilt}</a>
          </nav>
        </div>
      </div>
    </footer>
  )
}

function Check() {
  return (
    <svg viewBox="0 0 24 24" aria-hidden="true">
      <path d="m5 12 5 5L20 7" />
    </svg>
  )
}

export default function App({ initialLang }: { initialLang: Lang }) {
  const [lang, setLang] = useState<Lang>(initialLang)
  const [theme, setTheme] = useTheme()
  const release = useRelease()
  const t = T[lang] as Dict

  useEffect(() => {
    document.documentElement.lang = tagOf(lang)
    document.title = t.doc
    const meta = document.querySelector('meta[name="description"]')
    if (meta) meta.setAttribute("content", t.desc)
  }, [lang, t])

  const chooseLang = (next: Lang) => {
    setLang(next)
    try {
      localStorage.setItem("mojian:lang", next)
    } catch {
      // storage can be blocked
    }
    const url = new URL(location.href)
    if (next === "zh-Hans") url.searchParams.delete("lang")
    else url.searchParams.set("lang", next)
    history.replaceState(null, "", url)
  }

  return (
    <>
      <Header t={t} lang={lang} onLang={chooseLang} theme={theme} onTheme={setTheme} />
      <main id="top">
        <Hero t={t} release={release} />
        <Proof t={t} />
        <Stats t={t} />
        <Features t={t} />
        <Files t={t} />
        <Downloads t={t} release={release} />
      </main>
      <Footer t={t} release={release} />
    </>
  )
}
