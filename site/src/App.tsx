import { type ReactNode, useEffect, useMemo, useState } from "react"

import { asset, LATEST, RELEASES, REPO, useRelease, type AssetKey, type Release } from "./release"
import { T, tagOf, titleParts, type Dict, type Lang } from "./i18n"
import { useTheme, type Theme } from "./theme"
import { MobileMenu, ThemeMenu } from "./components/Menus"
import { MOJIAN_PALETTES, Workbench, type PalettePreset } from "./components/Workbench"
import {
  AppleIcon,
  ArrowRightIcon,
  CheckIcon,
  CodeIcon,
  CommandIcon,
  DownloadIcon,
  GitBranchIcon,
  GitHubIcon,
  LinuxIcon,
  PaletteIcon,
  ServerIcon,
  TerminalIcon,
  WindowsIcon,
} from "./components/Icons"

type OSKind = "mac" | "linux" | "win"

function detectOS(): OSKind {
  if (typeof navigator === "undefined") return "mac"
  const ua = (navigator.userAgent || "").toLowerCase()
  if (ua.includes("win")) return "win"
  if (ua.includes("linux") && !ua.includes("android")) return "linux"
  return "mac"
}

const NAV = [
  ["navFeatures", "#features"],
  ["navWorkflow", "#shortcuts"],
  ["navFiles", "#files"],
  ["navDownload", "#download"],
] as const

const STATS = [
  ["statWebview", "0"],
  ["statCgo", "0"],
  ["statBinary", "1"],
  ["statPlatform", "3"],
] as const

type Platform = {
  id: OSKind
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
    id: "mac",
    name: "macOS",
    tag: "arm64 · x64",
    req: "macReq",
    icon: <AppleIcon />,
    primary: "mac-arm64",
    primaryLabel: "dlMac",
    sub: [{ key: "mac-amd64", label: "dlIntel" }],
  },
  {
    id: "linux",
    name: "Linux",
    tag: "deb · rpm",
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
    id: "win",
    name: "Windows",
    tag: "x64 · arm64",
    req: "winReq",
    icon: <WindowsIcon />,
    primary: "windows-amd64",
    primaryLabel: "dlWin",
    sub: [{ key: "windows-arm64", label: "dlWinArm" }],
  },
]

const SHORTCUTS = [
  ["⌘P", "k1"],
  ["⇧⌘F", "k2"],
  ["⇧⌘O", "k3"],
  ["⌥⌘L", "k4"],
  ["⌘J", "k5"],
  ["⌃⇧G", "k6"],
  ["⌘S", "k7"],
] as const

const POINTS = ["point1", "point2", "point3"] as const

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
          <span className="brand-word">Mojian</span>
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

function Hero({
  t,
  release,
  os,
  palette,
  onPalette,
}: {
  t: Dict
  release: Release
  os: OSKind
  palette: PalettePreset
  onPalette: (p: PalettePreset) => void
}) {
  const [first, second] = titleParts(t.title)
  const primaryAsset: AssetKey = os === "win" ? "windows-amd64" : os === "linux" ? "linux-amd64" : "mac-arm64"
  const primaryLabel = os === "win" ? t.ctaForWin : os === "linux" ? t.ctaForLinux : t.ctaForMac

  return (
    <section className="hero shell">
      <div className="hero-copy">
        <a className="version-pill" href={LATEST}>
          <span className="version-dot" aria-hidden="true" />
          <strong>v{release.version}</strong>
          <span className="version-sep" aria-hidden="true">·</span>
          <span>{t.badge}</span>
          <ArrowRightIcon className="version-arrow" />
        </a>

        <h1 className="hero-title">
          {first}
          <span>{second}</span>
        </h1>

        <p className="hero-lede">{t.lede}</p>

        <div className="hero-actions">
          <a className="btn btn-primary" href={asset(release, primaryAsset)}>
            <DownloadIcon />
            <span>{primaryLabel}</span>
          </a>
          <a className="btn btn-secondary" href={REPO}>
            <GitHubIcon />
            <span>{t.ctaGithub}</span>
          </a>
        </div>

        <dl className="hero-metrics">
          {STATS.map(([label, value]) => (
            <div className="metric" key={label}>
              <dd>{value}</dd>
              <dt>{t[label]}</dt>
            </div>
          ))}
        </dl>
      </div>

      <Workbench t={t} activePalette={palette} onPalette={onPalette} />
    </section>
  )
}

function Proof({ t }: { t: Dict }) {
  return (
    <section className="proof-section shell" aria-label={t.proofCaption}>
      <div className="proof-head">
        <p className="eyebrow">{t.proofEyebrow}</p>
        <p className="proof-caption">{t.proofCaption}</p>
      </div>
      <figure className="shot-frame">
        <img
          id="shot"
          src="/home-1000.webp"
          srcSet="/home-1000.webp 1000w, /home-1466.webp 1466w"
          sizes="(max-width: 1280px) calc(100vw - 48px), 1120px"
          width={1466}
          height={919}
          fetchPriority="high"
          decoding="async"
          alt={t.alt}
        />
      </figure>
    </section>
  )
}

function Features({
  t,
  palette,
  onPalette,
}: {
  t: Dict
  palette: PalettePreset
  onPalette: (p: PalettePreset) => void
}) {
  return (
    <section className="section shell" id="features">
      <div className="section-head">
        <div>
          <p className="eyebrow">{t.featuresEyebrow}</p>
          <h2>{t.featuresTitle}</h2>
        </div>
        <p className="section-lede">{t.featuresLede}</p>
      </div>

      <div className="bento">
        <article className="bento-card span-7">
          <div className="bento-meta">
            <span className="bento-icon"><TerminalIcon /></span>
            <span className="bento-tag">{t.b1Tag}</span>
          </div>
          <h3>{t.b1Title}</h3>
          <p>{t.b1Body}</p>
          <div className="micro-panel micro-synergy" aria-hidden="true">
            <div className="micro-row">
              <span className="micro-badge">⌘+Click</span>
              <code>internal/app/lsp.go:142:5</code>
              <span className="micro-arrow">→</span>
              <span className="micro-dim">split &amp; jump</span>
            </div>
            <div className="micro-row">
              <span className="micro-badge accent">⌥⌘L</span>
              <code>@internal/app/lsp.go:138-144</code>
              <span className="micro-arrow">→</span>
              <span className="micro-dim">paste to terminal</span>
            </div>
            <div className="micro-row">
              <span className="micro-badge">OSC 9</span>
              <code>Tests green (142 passed)</code>
              <span className="micro-arrow">→</span>
              <span className="micro-dot" />
            </div>
          </div>
        </article>

        <article className="bento-card span-5">
          <div className="bento-meta">
            <span className="bento-icon"><CodeIcon /></span>
            <span className="bento-tag">{t.b2Tag}</span>
          </div>
          <h3>{t.b2Title}</h3>
          <p>{t.b2Body}</p>
          <div className="micro-chips" aria-hidden="true">
            <span>gopls</span>
            <span>tsserver</span>
            <span>pyright</span>
            <span>rust-analyzer</span>
            <span>clangd</span>
            <span>zls</span>
            <span>lua-ls</span>
            <span>yaml-ls</span>
            <span>sourcekit</span>
          </div>
        </article>

        <article className="bento-card span-5">
          <div className="bento-meta">
            <span className="bento-icon"><ServerIcon /></span>
            <span className="bento-tag">{t.b3Tag}</span>
          </div>
          <h3>{t.b3Title}</h3>
          <p>{t.b3Body}</p>
          <div className="micro-panel micro-ssh" aria-hidden="true">
            <div className="ssh-line">
              <i className="ssh-led" />
              <strong>prod-box</strong>
              <span>mojian-server · stdio</span>
            </div>
            <div className="ssh-line dim">
              <span>2 PTYs attached · remote watch · auto-reconnect</span>
            </div>
          </div>
        </article>

        <article className="bento-card span-7">
          <div className="bento-meta">
            <span className="bento-icon"><GitBranchIcon /></span>
            <span className="bento-tag">{t.b4Tag}</span>
          </div>
          <h3>{t.b4Title}</h3>
          <p>{t.b4Body}</p>
          <div className="micro-panel micro-diff" aria-hidden="true">
            <div className="mdiff-head">
              <span>@@ -42,4 +42,5 @@ hunk.go</span>
              <div className="mdiff-acts">
                <span>Stage Hunk</span>
                <span>Discard</span>
              </div>
            </div>
            <div className="mdiff-line del">- return r.Run(dir, &quot;apply&quot;)</div>
            <div className="mdiff-line add">+ return sr.RunStdin(dir, patch, &quot;apply&quot;, &quot;--cached&quot;)</div>
          </div>
        </article>

        <article className="bento-card span-6">
          <div className="bento-meta">
            <span className="bento-icon"><CommandIcon /></span>
            <span className="bento-tag">{t.b5Tag}</span>
          </div>
          <h3>{t.b5Title}</h3>
          <p>{t.b5Body}</p>
          <div className="micro-toggles" aria-hidden="true">
            <div><span>Vim Mode</span><b>ON</b></div>
            <div><span>Status Bar</span><b>ON</b></div>
            <div><span>Git Blame</span><b>ON</b></div>
          </div>
        </article>

        <article className="bento-card span-6">
          <div className="bento-meta">
            <span className="bento-icon"><PaletteIcon /></span>
            <span className="bento-tag">{t.b6Tag}</span>
          </div>
          <h3>{t.b6Title}</h3>
          <p>{t.b6Body}</p>
          <div className="palette-strip">
            {MOJIAN_PALETTES.map((p) => (
              <button
                key={p.id}
                type="button"
                className={`palette-chip ${palette.id === p.id ? "active" : ""}`}
                onClick={() => onPalette(p)}
              >
                <span style={{ backgroundColor: p.swatch }} />
                <small>{p.name}</small>
              </button>
            ))}
          </div>
        </article>
      </div>
    </section>
  )
}

function ShortcutsBand({ t }: { t: Dict }) {
  return (
    <section className="shortcuts-section shell" id="shortcuts">
      <div className="shortcuts-head">
        <p className="eyebrow">{t.keysEyebrow}</p>
        <h2>{t.keysTitle}</h2>
      </div>
      <div className="shortcuts-grid">
        {SHORTCUTS.map(([keys, label]) => (
          <div className="shortcut-item" key={label}>
            <kbd>{keys}</kbd>
            <span>{t[label]}</span>
          </div>
        ))}
      </div>
    </section>
  )
}

function Files({ t }: { t: Dict }) {
  return (
    <section className="section shell" id="files">
      <div className="files-panel">
        <div className="files-copy">
          <p className="eyebrow">{t.filesEyebrow}</p>
          <h2>{t.filesTitle}</h2>
          <p className="section-lede">{t.filesBody}</p>
          <ul className="points">
            {POINTS.map((key) => (
              <li key={key}>
                <span className="point-check" aria-hidden="true">
                  <CheckIcon />
                </span>
                <span>{t[key]}</span>
              </li>
            ))}
          </ul>
        </div>
        <div className="files-specimen">
          <div className="tree-card">
            <div className="tree-head">
              <span>{t.treeLabel}</span>
              <span className="tree-pill">~/code/project</span>
            </div>
            <pre className="tree" aria-label={t.treeLabel}>
              <span className="dir">project/</span>{"\n"}
              {"├── "}<span className="dir">cmd/</span>{"\n"}
              {"│   └── "}<span className="file">main.go</span>{"        "}<span className="meta">2.4 KB · clean</span>{"\n"}
              {"├── "}<span className="dir">docs/</span>{"\n"}
              {"│   ├── "}<span className="file">spec.md</span>{"        "}<span className="meta">6.1 KB · preview</span>{"\n"}
              {"│   └── "}<span className="file">arch.pdf</span>{"       "}<span className="meta">420 KB · native</span>{"\n"}
              {"├── "}<span className="dir">assets/</span>{"\n"}
              {"│   └── "}<span className="file">hero.png</span>{"       "}<span className="meta">118 KB · image</span>{"\n"}
              {"└── "}<span className="file">README.md</span>{"          "}<span className="meta">1.8 KB · ⌘S</span>
            </pre>
          </div>
          <p className="tree-caption">{t.treeCaption}</p>
        </div>
      </div>
    </section>
  )
}

function Downloads({ t, release, os }: { t: Dict; release: Release; os: OSKind }) {
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
        {PLATFORMS.map((platform) => {
          const isCurrent = platform.id === os
          return (
            <article className={`platform-card ${isCurrent ? "recommended" : ""}`} key={platform.name}>
              <div className="platform-top">
                <div className="platform-head">
                  <span className="platform-icon">{platform.icon}</span>
                  <div>
                    <h3>{platform.name}</h3>
                    <span className="platform-tag">{platform.tag}</span>
                  </div>
                </div>
                {isCurrent && <span className="rec-badge">{t.recommended}</span>}
              </div>
              <p className="platform-req">{t[platform.req]}</p>
              <div className="platform-actions">
                <a
                  className={`btn ${isCurrent ? "btn-primary" : "btn-secondary"}`}
                  href={asset(release, platform.primary)}
                  data-download={platform.primary}
                >
                  <DownloadIcon />
                  <span>{t[platform.primaryLabel]}</span>
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
          )
        })}
      </div>
      <p className="download-note">{t.downloadNote}</p>
    </section>
  )
}

function Footer({ t, release }: { t: Dict; release: Release }) {
  return (
    <footer className="site-footer">
      <div className="foot shell">
        <div className="foot-brand">
          <span className="brand-mark small" aria-hidden="true" />
          <strong>Mojian</strong>
          <span className="foot-ver">v{release.version}</span>
          <span className="foot-sep" aria-hidden="true">·</span>
          <span>{t.footerBuilt}</span>
        </div>
        <div className="foot-links">
          <a href={REPO}>GitHub</a>
          <a href={RELEASES}>Releases</a>
          <a href={`${REPO}/blob/main/CHANGELOG.md`}>Changelog</a>
          <a href={`${REPO}/blob/main/LICENSE`}>{t.footerLicense}</a>
        </div>
      </div>
    </footer>
  )
}

export default function App({ initialLang }: { initialLang: Lang }) {
  const [lang, setLang] = useState<Lang>(initialLang)
  const [theme, setTheme] = useTheme()
  const [palette, setPalette] = useState<PalettePreset>(MOJIAN_PALETTES[0])
  const release = useRelease()
  const os = useMemo(() => detectOS(), [])
  const t = T[lang]

  useEffect(() => {
    document.documentElement.lang = tagOf(lang)
    document.title = t.doc
    const desc = document.querySelector('meta[name="description"]')
    if (desc) desc.setAttribute("content", t.desc)
    try {
      localStorage.setItem("mojian:lang", lang)
    } catch {
      // storage can be blocked
    }
  }, [lang, t])

  return (
    <div className="page-root" id="top">
      <Header t={t} lang={lang} onLang={setLang} theme={theme} onTheme={setTheme} />
      <main>
        <Hero t={t} release={release} os={os} palette={palette} onPalette={setPalette} />
        <Proof t={t} />
        <Features t={t} palette={palette} onPalette={setPalette} />
        <ShortcutsBand t={t} />
        <Files t={t} />
        <Downloads t={t} release={release} os={os} />
      </main>
      <Footer t={t} release={release} />
    </div>
  )
}
