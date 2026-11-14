import { Menu } from "@base-ui/react/menu"
import { useState } from "react"

import type { Dict } from "../i18n"
import type { Theme } from "../theme"
import { CheckIcon, MenuIcon, MonitorIcon, MoonIcon, SunIcon } from "./Icons"

type ThemeProps = {
  t: Dict
  theme: Theme
  onTheme: (theme: Theme) => void
}

export function ThemeMenu({ t, theme, onTheme }: ThemeProps) {
  const [open, setOpen] = useState(false)
  return (
    <Menu.Root open={open} onOpenChange={setOpen}>
      <Menu.Trigger className="icon-btn theme-toggle" aria-label={t.themeLabel}>
        <SunIcon className="sun" />
        <MoonIcon className="moon" />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner className="menu-positioner" align="end" sideOffset={6}>
          <Menu.Popup className="menu-popup" aria-label={t.themeLabel}>
            <Menu.RadioGroup
              value={theme}
              onValueChange={(value) => {
                onTheme(value as Theme)
                setOpen(false)
              }}
              aria-label={t.themeLabel}
            >
              <Menu.RadioItem className="menu-item" value="light">
                <SunIcon />
                <span>{t.themeLight}</span>
                <Menu.RadioItemIndicator className="check">
                  <CheckIcon />
                </Menu.RadioItemIndicator>
              </Menu.RadioItem>
              <Menu.RadioItem className="menu-item" value="dark">
                <MoonIcon />
                <span>{t.themeDark}</span>
                <Menu.RadioItemIndicator className="check">
                  <CheckIcon />
                </Menu.RadioItemIndicator>
              </Menu.RadioItem>
              <Menu.RadioItem className="menu-item" value="system">
                <MonitorIcon />
                <span>{t.themeSystem}</span>
                <Menu.RadioItemIndicator className="check">
                  <CheckIcon />
                </Menu.RadioItemIndicator>
              </Menu.RadioItem>
            </Menu.RadioGroup>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  )
}

const links = [
  ["navFeatures", "#features"],
  ["navFiles", "#files"],
  ["navDownload", "#download"],
] as const

export function MobileMenu({ t }: { t: Dict }) {
  return (
    <Menu.Root>
      <Menu.Trigger className="icon-btn mobile-only" aria-label={t.menuLabel}>
        <MenuIcon />
      </Menu.Trigger>
      <Menu.Portal>
        <Menu.Positioner className="menu-positioner" align="end" sideOffset={6}>
          <Menu.Popup className="menu-popup mobile-menu" aria-label={t.menuLabel}>
            {links.map(([key, href]) => (
              <Menu.Item key={key} className="menu-item" render={<a href={href} />}>
                {t[key]}
              </Menu.Item>
            ))}
            <Menu.Item className="menu-item" render={<a href="https://github.com/ox0pine/mojian" />}>
              GitHub
            </Menu.Item>
          </Menu.Popup>
        </Menu.Positioner>
      </Menu.Portal>
    </Menu.Root>
  )
}
