import { useState, type CSSProperties } from "react"
import type { Dict } from "../i18n"

export type PalettePreset = {
  id: string
  name: string
  swatch: string
  lightBg: string
  lightBg2: string
  lightAccent: string
  darkBg: string
  darkBg2: string
  darkAccent: string
}

export const MOJIAN_PALETTES: PalettePreset[] = [
  {
    id: "paper",
    name: "Paper",
    swatch: "#ded8ca",
    lightBg: "#f5f2ea",
    lightBg2: "#eae5d9",
    lightAccent: "#3b5249",
    darkBg: "#171614",
    darkBg2: "#1f1d1a",
    darkAccent: "#b8c7b8",
  },
  {
    id: "petal",
    name: "Petal",
    swatch: "#e7b7bd",
    lightBg: "#f9f1f2",
    lightBg2: "#f1e3e6",
    lightAccent: "#9c4755",
    darkBg: "#191315",
    darkBg2: "#22191c",
    darkAccent: "#e4a6b0",
  },
  {
    id: "mist",
    name: "Mist",
    swatch: "#aabcc2",
    lightBg: "#eff4f6",
    lightBg2: "#e1eaee",
    lightAccent: "#385f6d",
    darkBg: "#12171a",
    darkBg2: "#192126",
    darkAccent: "#9bc1ce",
  },
  {
    id: "meadow",
    name: "Meadow",
    swatch: "#a8b89c",
    lightBg: "#f2f5ef",
    lightBg2: "#e5ece0",
    lightAccent: "#44623a",
    darkBg: "#131712",
    darkBg2: "#1b2119",
    darkAccent: "#a4c295",
  },
  {
    id: "dusk",
    name: "Dusk",
    swatch: "#8d879e",
    lightBg: "#f3f2f7",
    lightBg2: "#e6e4ee",
    lightAccent: "#564c78",
    darkBg: "#15141b",
    darkBg2: "#1e1c26",
    darkAccent: "#b5acd4",
  },
  {
    id: "tide",
    name: "Tide",
    swatch: "#7ea5aa",
    lightBg: "#eef5f5",
    lightBg2: "#dfecec",
    lightAccent: "#2c646b",
    darkBg: "#111819",
    darkBg2: "#182224",
    darkAccent: "#8bc4ca",
  },
  {
    id: "apricot",
    name: "Apricot",
    swatch: "#dfa47f",
    lightBg: "#faf3ed",
    lightBg2: "#f1e4d9",
    lightAccent: "#9a562d",
    darkBg: "#1a1512",
    darkBg2: "#241d18",
    darkAccent: "#e5ae87",
  },
  {
    id: "aurora",
    name: "Aurora",
    swatch: "#8fa3c4",
    lightBg: "#f0f3f8",
    lightBg2: "#e2e8f2",
    lightAccent: "#3b5685",
    darkBg: "#12151c",
    darkBg2: "#191e29",
    darkAccent: "#9bb5e0",
  },
  {
    id: "dune",
    name: "Dune",
    swatch: "#c6ae86",
    lightBg: "#f7f3eb",
    lightBg2: "#ece4d6",
    lightAccent: "#7a5e32",
    darkBg: "#191612",
    darkBg2: "#231f19",
    darkAccent: "#d4bc94",
  },
  {
    id: "plain",
    name: "Plain",
    swatch: "#a5a5a0",
    lightBg: "#f4f4f2",
    lightBg2: "#e8e8e5",
    lightAccent: "#2f2f2d",
    darkBg: "#141414",
    darkBg2: "#1d1d1d",
    darkAccent: "#c8c8c4",
  },
]

type TabMode = "code" | "git" | "ssh"

export function Workbench({
  t,
  activePalette,
  onPalette,
}: {
  t: Dict
  activePalette: PalettePreset
  onPalette: (p: PalettePreset) => void
}) {
  const [tab, setTab] = useState<TabMode>("code")

  const style = {
    "--wb-light-bg": activePalette.lightBg,
    "--wb-light-bg2": activePalette.lightBg2,
    "--wb-light-accent": activePalette.lightAccent,
    "--wb-dark-bg": activePalette.darkBg,
    "--wb-dark-bg2": activePalette.darkBg2,
    "--wb-dark-accent": activePalette.darkAccent,
  } as CSSProperties

  return (
    <div className="wb-stage" style={style}>
      <div className="wb-controls">
        <div className="wb-tabs" role="tablist" aria-label={t.wbAria}>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "code"}
            className={tab === "code" ? "active" : ""}
            onClick={() => setTab("code")}
          >
            {t.wbTabCode}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "git"}
            className={tab === "git" ? "active" : ""}
            onClick={() => setTab("git")}
          >
            {t.wbTabGit}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={tab === "ssh"}
            className={tab === "ssh" ? "active" : ""}
            onClick={() => setTab("ssh")}
          >
            {t.wbTabSsh}
          </button>
        </div>

        <div className="wb-palettes" aria-label={t.wbThemeAria}>
          {MOJIAN_PALETTES.map((p) => (
            <button
              key={p.id}
              type="button"
              className={`wb-dot ${activePalette.id === p.id ? "active" : ""}`}
              title={p.name}
              aria-label={p.name}
              aria-pressed={activePalette.id === p.id}
              onClick={() => onPalette(p)}
            >
              <span style={{ backgroundColor: p.swatch }} />
            </button>
          ))}
        </div>
      </div>

      <div className="wb-window">
        <div className="wb-titlebar">
          <div className="wb-traffic" aria-hidden="true">
            <i />
            <i />
            <i />
          </div>
          <div className="wb-chips">
            <span className="wb-chip">
              <span className="wb-chip-dot" />
              {tab === "ssh" ? "prod-box" : "Local"}
            </span>
            <span className="wb-chip">{tab === "ssh" ? "/srv/api" : "mojian"}</span>
            <span className="wb-chip wb-chip-branch">main</span>
          </div>
          <div className="wb-tabstrip">
            <span className={tab === "code" ? "active" : ""}>lsp.go · Terminal</span>
            <span className={tab === "git" ? "active" : ""}>Git Diff</span>
            <span className={tab === "ssh" ? "active" : ""}>
              SSH · prod-box
              <em className="wb-attn" aria-hidden="true" />
            </span>
          </div>
          <div className="wb-bar-right">
            <kbd>⌘K</kbd>
          </div>
        </div>

        {tab === "code" && (
          <div className="wb-body wb-body-code">
            <aside className="wb-tree" aria-label="Explorer">
              <div className="wb-pane-head">
                <span>EXPLORER</span>
                <span className="wb-muted">⌘B</span>
              </div>
              <ul className="wb-tree-list">
                <li className="dir">internal/</li>
                <li className="dir indent">app/</li>
                <li className="file indent2 active">lsp.go</li>
                <li className="file indent2">term_link.go</li>
                <li className="file indent2">nav.go</li>
                <li className="dir indent">git/</li>
                <li className="file indent2">hunk.go</li>
                <li className="dir indent">lsp/</li>
                <li className="file indent2">client.go</li>
                <li className="file">README.md</li>
              </ul>
            </aside>

            <div className="wb-card wb-editor">
              <div className="wb-pane-head">
                <span className="wb-pane-title">
                  lsp.go <em className="wb-dirty" />
                </span>
                <span className="wb-muted">internal/app/lsp.go</span>
              </div>
              <div className="wb-code">
                <div className="wb-line">
                  <span className="ln">138</span>
                  <code>
                    <span className="kw">func</span> (a *app){" "}
                    <span className="fn">openLink</span>(raw <span className="ty">string</span>){" "}
                    <span className="ty">bool</span> {"{"}
                  </code>
                </div>
                <div className="wb-line">
                  <span className="ln">139</span>
                  <code>
                    {"  "}p, line, col, ok := <span className="fn">parseLink</span>(raw)
                  </code>
                </div>
                <div className="wb-line added">
                  <span className="ln">140</span>
                  <code>
                    {"  "}rel, found := a.<span className="fn">resolvePath</span>(p)
                  </code>
                </div>
                <div className="wb-line active">
                  <span className="ln">141</span>
                  <code>
                    {"  "}a.<span className="fn">recordNav</span>(){" "}
                    <span className="wb-blame">You, 2m ago · term links</span>
                  </code>
                </div>
                <div className="wb-line">
                  <span className="ln">142</span>
                  <code>
                    {"  "}a.<span className="fn wb-hover-tok">openNoteAt</span>(rel, line, col, col)
                  </code>
                  <div className="wb-hover-card" aria-hidden="true">
                    <span className="wb-hover-sig">func (a *app) openNoteAt(rel string, line, col, end int)</span>
                    <span className="wb-hover-doc">Opens a file and jumps directly to line &amp; column.</span>
                  </div>
                </div>
                <div className="wb-line">
                  <span className="ln">143</span>
                  <code>
                    {"  "}
                    <span className="kw">return</span> <span className="lit">true</span>
                  </code>
                </div>
                <div className="wb-line">
                  <span className="ln">144</span>
                  <code>{"}"}</code>
                </div>
              </div>
            </div>

            <div className="wb-card wb-term">
              <div className="wb-pane-head">
                <span className="wb-pane-title">
                  <span className="wb-attn-inline" /> zsh · go test
                </span>
                <span className="wb-muted">⌘F</span>
              </div>
              <div className="wb-term-body">
                <p>
                  <span className="prompt">$</span> go test ./internal/app
                </p>
                <p className="wb-term-dim">--- FAIL: TestLinkJump (0.00s)</p>
                <p>
                  <span className="wb-link" title="⌘+Click to open in editor">
                    internal/app/lsp.go:142:5
                  </span>
                  <span className="wb-term-tag">⌘+Click</span>
                </p>
                <p className="wb-term-ok">PASS ok mojian/internal/lsp 0.4s</p>
                <p className="wb-term-ok">PASS ok mojian/internal/app 1.1s</p>
                <p className="wb-term-send">
                  <span className="prompt">$</span> claude{" "}
                  <span className="wb-ref-pill">@app/lsp.go:138-144</span>
                  <span className="wb-cursor" />
                </p>
              </div>
            </div>
          </div>
        )}

        {tab === "git" && (
          <div className="wb-body wb-body-git">
            <aside className="wb-tree" aria-label="Changes">
              <div className="wb-pane-head">
                <span>WORKING TREE</span>
                <span className="wb-muted">3</span>
              </div>
              <ul className="wb-tree-list">
                <li className="file active">
                  <span>lsp_actions.go</span>
                  <span className="wb-git-m">M</span>
                </li>
                <li className="file">
                  <span>term_link.go</span>
                  <span className="wb-git-a">A</span>
                </li>
                <li className="file">
                  <span>hunk.go</span>
                  <span className="wb-git-m">M</span>
                </li>
              </ul>
              <div className="wb-git-commit">
                <div className="wb-commit-box">feat: add hunk staging &amp; LSP rename</div>
                <div className="wb-commit-actions">
                  <span className="wb-pill-btn primary">Commit</span>
                  <span className="wb-pill-btn">Stash</span>
                  <span className="wb-pill-btn">Push ↑1</span>
                </div>
              </div>
            </aside>

            <div className="wb-card wb-diff">
              <div className="wb-pane-head">
                <span className="wb-pane-title">internal/app/lsp_actions.go</span>
                <div className="wb-diff-tools">
                  <span className="wb-pill-btn active">Split</span>
                  <span className="wb-pill-btn">Stage Hunk</span>
                  <span className="wb-pill-btn">Discard</span>
                </div>
              </div>
              <div className="wb-diff-grid">
                <div className="wb-diff-col">
                  <div className="wb-diff-hunk">@@ -88,6 +88,8 @@ func (a *app) openReferences()</div>
                  <div className="wb-dline"><span>88</span><code>locs, err := cl.References(ctx, uri, pos)</code></div>
                  <div className="wb-dline del"><span>89</span><code>- a.openNoteAt(rel, line, 0, 0)</code></div>
                  <div className="wb-dline"><span>90</span><code>a.refsItems = items</code></div>
                  <div className="wb-dline"><span>91</span><code>a.invalidate()</code></div>
                </div>
                <div className="wb-diff-col">
                  <div className="wb-diff-hunk">@@ -88,6 +88,8 @@ Hunk 1 of 2 · Staged</div>
                  <div className="wb-dline"><span>88</span><code>locs, err := cl.References(ctx, uri, pos)</code></div>
                  <div className="wb-dline add"><span>89</span><code>+ a.recordNav()</code></div>
                  <div className="wb-dline add"><span>90</span><code>+ a.openNoteAt(rel, line, col, col)</code></div>
                  <div className="wb-dline"><span>91</span><code>a.refsItems = items</code></div>
                </div>
              </div>
            </div>
          </div>
        )}

        {tab === "ssh" && (
          <div className="wb-body wb-body-ssh">
            <div className="wb-card wb-ssh-info">
              <div className="wb-pane-head">
                <span className="wb-pane-title">SSH · prod-box</span>
                <span className="wb-status-ok">Connected · mojian-server</span>
              </div>
              <div className="wb-ssh-rows">
                <div>
                  <span className="wb-ssh-k">Transport</span>
                  <span className="wb-ssh-v">Plain ssh stdio · zero open ports</span>
                </div>
                <div>
                  <span className="wb-ssh-k">Sessions</span>
                  <span className="wb-ssh-v">2 detached PTYs survive sleep &amp; Wi-Fi drops</span>
                </div>
                <div>
                  <span className="wb-ssh-k">Remote LSP</span>
                  <span className="wb-ssh-v">gopls &amp; rust-analyzer multiplexed on server</span>
                </div>
                <div>
                  <span className="wb-ssh-k">File Watch</span>
                  <span className="wb-ssh-v">Live remote sync · clean buffers reload automatically</span>
                </div>
              </div>
            </div>
            <div className="wb-card wb-term">
              <div className="wb-pane-head">
                <span className="wb-pane-title">SSH · prod-box — build</span>
                <span className="wb-muted">detached · 2h 14m</span>
              </div>
              <div className="wb-term-body">
                <p className="wb-term-dim">[Reconnected automatically after network change]</p>
                <p>
                  <span className="prompt">prod-box$</span> cargo test --release
                </p>
                <p className="wb-term-ok">test result: ok. 142 passed; 0 failed; finished in 3.8s</p>
                <p className="wb-term-dim">OSC 9 notification → "Build green on prod-box"</p>
                <p>
                  <span className="prompt">prod-box$</span> <span className="wb-cursor" />
                </p>
              </div>
            </div>
          </div>
        )}

        <div className="wb-statusbar">
          <div className="wb-sb-left">
            <span className="wb-vim-pill">NORMAL</span>
            <span>gopls</span>
            <span className="wb-sb-ok">0 problems</span>
          </div>
          <div className="wb-sb-right">
            <span>Ln 142, Col 18</span>
            <span>Tabs: 4</span>
            <span>LF</span>
            <span>Go</span>
          </div>
        </div>
      </div>
    </div>
  )
}
