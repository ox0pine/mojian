import { type ReactNode, useEffect, useState } from "react"

import { asset, LATEST, RELEASES, REPO, useRelease, type AssetKey, type Release } from "./release"
import { detectLang, T, tagOf, titleParts, type Dict, type Lang } from "./i18n"
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
  ["f1Title", "f1Body", "projects-and-files"],
  ["f2Title", "f2Body", "tabs-panes-and-terminals"],
  ["f3Title", "f3Body", "remote-projects"],
  ["f4Title", "f4Body", "writing-and-previewing"],
  ["f5Title", "f5Body", "git"],
  ["f6Title", "f6Body", "settings"],
] as const

const PLATFORM_LINKS = [
  ["dlIntel", "mac-amd64"],
  ["dlArm", "linux-arm64"],
  ["dlRpm", "linux-amd64-rpm"],
  ["dlWinX64", "windows-amd64"],
  ["dlWinArm", "windows-arm64"],
] as const

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
    <header className="frame site-header">
      <div className="bar">
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
    <section className="hero">
      <div className="hero-copy">
        <a className="version" href={LATEST}>
          <span className="dot" aria-hidden="true" />
          <b>v{release.version}</b>
          <span>{t.badge}</span>
        </a>
        <h1>
          {first}
          <span>{second}</span>
        </h1>
        <p className="lede">{t.lede}</p>
        <div className="actions">
          <a className="btn btn-primary" href="#download">
            <DownloadIcon />
            <span>{t.ctaDownload}</span>
          </a>
          <a className="btn btn-secondary" href={REPO}>
            <GitHubIcon />
            <span>{t.ctaGithub}</span>
          </a>
        </div>
        <p className="platforms">
          {PLATFORM_LINKS.map(([label, key]) => (
            <a key={key} href={asset(release, key)} data-download={key}>
              {t[label]}
            </a>
          ))}
          <a href={RELEASES}>{t.dlAll}</a>
        </p>
      </div>
      <div className="hero-art" aria-hidden="true">
        <div className="mini">
          <div className="mini-bar">
            <span className="mini-dots">
              <i />
              <i />
              <i />
            </span>
            <span className="mini-title">Explorer</span>
            <span style={{ marginLeft: "auto" }}>Reading notes.md</span>
          </div>
          <div className="mini-body">
            <div className="mini-side">
              <div className="mini-row on"><i /> Reading notes.md</div>
              <div className="mini-row"><i /> Ideas</div>
              <div className="mini-row"><i /> scripts</div>
              <div className="mini-row"><i /> Welcome.md</div>
              <div className="mini-row"><i /> README.md</div>
            </div>
            <div className="mini-editor">
              <div><b>#</b> Reading notes</div>
              <div>A quiet place to keep</div>
              <div>the sentences that matter.</div>
              <div>&nbsp;</div>
              <div><span className="c">##</span> Morning</div>
              <div>The cursor stays<span className="mini-caret" /></div>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}

function Stats({ t }: { t: Dict }) {
  return (
    <dl className="stats">
      {STATS.map(([label, value]) => (
        <div className="stat" key={label}>
          <dt className="label">{t[label]}</dt>
          <dd>{value}</dd>
        </div>
      ))}
    </dl>
  )
}

function Proof({ t }: { t: Dict }) {
  return (
    <section className="proof">
      <figure className="shot">
        <img
          id="shot"
          src="/home-1000.webp"
          srcSet="/home-1000.webp 1000w, /home-1466.webp 1466w"
          sizes="(max-width: 1160px) calc(100vw - 32px), 1040px"
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

function Features({ t }: { t: Dict }) {
  return (
    <section className="section" id="features">
      <p className="eyebrow">{t.featuresEyebrow}</p>
      <h2>{t.featuresTitle}</h2>
      <p className="section-lede">{t.featuresLede}</p>
      <div className="feature-grid">
        {FEATURES.map(([title, body, anchor]) => (
          <article className="feature" key={title}>
            <a className="feature-link" href={`${REPO}#${anchor}`}>
              <h3>
                <span>{t[title]}</span>
                <ArrowRightIcon />
              </h3>
              <p>{t[body]}</p>
            </a>
          </article>
        ))}
      </div>
    </section>
  )
}

function Files({ t }: { t: Dict }) {
  return (
    <section className="section" id="files">
      <div className="files-grid">
        <div>
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
        <div>
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
    <section className="section" id="download">
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
      <div className="download-grid">
        {PLATFORMS.map((platform) => (
          <article className="platform-card" key={platform.name}>
            <div className="platform-head">
              {platform.icon}
              <h3>{platform.name}</h3>
              <span className="tag">{platform.tag}</span>
            </div>
            <p>{t[platform.req]}</p>
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
          </article>
        ))}
      </div>
      <p className="download-note">{t.downloadNote}</p>
    </section>
  )
}

function Footer({ t, release }: { t: Dict; release: Release }) {
  return (
    <footer className="frame site-footer">
      <div className="footer-row label">
        <span>
          Mojian <b>v{release.version}</b> ·{" "}
          <a href={`${REPO}/blob/main/LICENSE`}>{t.footerLicense}</a>
        </span>
        <nav className="footer-links" aria-label="Footer">
          <a href={REPO}>GitHub</a>
          <a href={RELEASES}>{t.navDownload}</a>
          <a href="https://mygo.run">{t.footerBuilt}</a>
        </nav>
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
      <main className="frame" id="top">
        <Hero t={t} release={release} />
        <Stats t={t} />
        <Proof t={t} />
        <Features t={t} />
        <Files t={t} />
        <Downloads t={t} release={release} />
      </main>
      <Footer t={t} release={release} />
    </>
  )
}
