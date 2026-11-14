import { useEffect, useState } from "react"

export const REPO = "https://github.com/ox0pine/mojian"
export const LATEST = `${REPO}/releases/latest`
export const RELEASES = `${REPO}/releases`

export type AssetKey =
  | "mac-arm64"
  | "mac-amd64"
  | "linux-amd64"
  | "linux-arm64"
  | "linux-amd64-rpm"
  | "windows-amd64"
  | "windows-arm64"

export type Release = {
  version: string
  assets: Partial<Record<AssetKey, string>>
}

const VERSION = "0.4.3"
const ASSET = (name: string) => `${REPO}/releases/download/v${VERSION}/${name}`

const FALLBACK: Release = {
  version: VERSION,
  assets: {
    "mac-arm64": ASSET(`mojian-${VERSION}-arm64.dmg`),
    "mac-amd64": ASSET(`mojian-${VERSION}-amd64.dmg`),
    "linux-amd64": ASSET(`mojian_${VERSION}_amd64.deb`),
    "linux-arm64": ASSET(`mojian_${VERSION}_arm64.deb`),
    "linux-amd64-rpm": ASSET(`mojian-${VERSION}-x86_64.rpm`),
    "windows-amd64": ASSET(`mojian-setup-${VERSION}-amd64.exe`),
    "windows-arm64": ASSET(`mojian-setup-${VERSION}-arm64.exe`),
  },
}

const CACHE_KEY = "mojian:release"
const CACHE_MS = 10 * 60 * 1000

const pickers: Record<AssetKey, RegExp> = {
  "mac-arm64": /-arm64\.dmg$/,
  "mac-amd64": /-amd64\.dmg$/,
  "linux-amd64": /_amd64\.deb$/,
  "linux-arm64": /_arm64\.deb$/,
  "linux-amd64-rpm": /x86_64\.rpm$/,
  "windows-amd64": /-setup-.*-amd64\.exe$/,
  "windows-arm64": /-setup-.*-arm64\.exe$/,
}

type GitHubAsset = { name: string; browser_download_url: string }

let pending: Promise<Release | null> | null = null

async function loadRelease(): Promise<Release | null> {
  try {
    const cached = JSON.parse(sessionStorage.getItem(CACHE_KEY) || "null") as
      | { at: number; release: Release }
      | null
    if (cached && Date.now() - cached.at < CACHE_MS) return cached.release
  } catch {
    // cache is optional
  }

  try {
    const res = await fetch(`https://api.github.com/repos/ox0pine/mojian/releases/latest`, {
      headers: { Accept: "application/vnd.github+json" },
    })
    if (!res.ok) return null
    const data = (await res.json()) as { tag_name?: string; assets?: GitHubAsset[] }
    const assets: Partial<Record<AssetKey, string>> = {}
    for (const [key, re] of Object.entries(pickers) as [AssetKey, RegExp][]) {
      const hit = data.assets?.find((a) => re.test(a.name))
      if (hit) assets[key] = hit.browser_download_url
    }
    const release: Release = {
      version: String(data.tag_name || VERSION).replace(/^v/, ""),
      assets,
    }
    try {
      sessionStorage.setItem(CACHE_KEY, JSON.stringify({ at: Date.now(), release }))
    } catch {
      // cache is optional
    }
    return release
  } catch {
    return null
  }
}

export function asset(release: Release, key: AssetKey): string {
  return release.assets[key] ?? FALLBACK.assets[key]!
}

export function useRelease(): Release {
  const [release, setRelease] = useState<Release>(FALLBACK)

  useEffect(() => {
    let alive = true
    pending ??= loadRelease()
    void pending.then((next) => {
      if (alive && next) setRelease(next)
    })
    return () => {
      alive = false
    }
  }, [])

  return release
}
