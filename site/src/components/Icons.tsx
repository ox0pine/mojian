import { APPLE_PATH, GITHUB_PATH, LINUX_PATH, WINDOWS_PATH } from "../platform-paths"

type Props = { className?: string }

const stroke = {
  fill: "none",
  stroke: "currentColor",
  strokeWidth: 1.8,
  strokeLinecap: "round" as const,
  strokeLinejoin: "round" as const,
}

export function SunIcon({ className }: Props) {
  return (
    <svg className={className} viewBox="0 0 24 24" aria-hidden="true" {...stroke}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41" />
    </svg>
  )
}

export function MoonIcon({ className }: Props) {
  return (
    <svg className={className} viewBox="0 0 24 24" aria-hidden="true" {...stroke}>
      <path d="M21 12.79A9 9 0 1 1 11.21 3 7 7 0 0 0 21 12.79z" />
    </svg>
  )
}

export function MonitorIcon({ className }: Props) {
  return (
    <svg className={className} viewBox="0 0 24 24" aria-hidden="true" {...stroke}>
      <rect x="3" y="4" width="18" height="13" rx="2" />
      <path d="M8 21h8M12 17v4" />
    </svg>
  )
}

export function MenuIcon({ className }: Props) {
  return (
    <svg className={className} viewBox="0 0 24 24" aria-hidden="true" {...stroke}>
      <path d="M4 6h16M4 12h16M4 18h16" />
    </svg>
  )
}

export function CheckIcon({ className }: Props) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth={2}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="m5 12 5 5L20 7" />
    </svg>
  )
}

export function ArrowRightIcon({ className }: Props) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.9}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M5 12h14" />
      <path d="m12 5 7 7-7 7" />
    </svg>
  )
}

export function DownloadIcon({ className }: Props) {
  return (
    <svg
      className={className}
      viewBox="0 0 24 24"
      aria-hidden="true"
      fill="none"
      stroke="currentColor"
      strokeWidth={1.9}
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <path d="M12 3v12" />
      <path d="m7 11 5 5 5-5" />
      <path d="M5 21h14" />
    </svg>
  )
}

export function GitHubIcon({ className }: Props) {
  return (
    <svg className={className} viewBox="0 0 16 16" fill="currentColor" aria-hidden="true">
      <path d={GITHUB_PATH} />
    </svg>
  )
}

export function AppleIcon({ className }: Props) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d={APPLE_PATH} />
    </svg>
  )
}

export function LinuxIcon({ className }: Props) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d={LINUX_PATH} />
    </svg>
  )
}

export function WindowsIcon({ className }: Props) {
  return (
    <svg className={className} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
      <path d={WINDOWS_PATH} />
    </svg>
  )
}
