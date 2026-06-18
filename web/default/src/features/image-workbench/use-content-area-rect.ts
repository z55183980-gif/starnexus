import { useEffect, useState } from 'react'

export type ContentAreaRect = {
  top: number
  left: number
  width: number
  height: number
}

function measureContentAreaRect(): ContentAreaRect | null {
  const inset = document.querySelector('[data-slot="sidebar-inset"]')
  if (inset) {
    const rect = inset.getBoundingClientRect()
    return {
      top: rect.top,
      left: rect.left,
      width: rect.width,
      height: rect.height,
    }
  }

  const headerHeight = Number.parseFloat(
    getComputedStyle(document.documentElement).getPropertyValue('--app-header-height')
  )

  const top = Number.isFinite(headerHeight) ? headerHeight : 48
  return {
    top,
    left: 0,
    width: window.innerWidth,
    height: Math.max(window.innerHeight - top, 320),
  }
}

export function useContentAreaRect(enabled = true): ContentAreaRect | null {
  const [rect, setRect] = useState<ContentAreaRect | null>(() =>
    enabled ? measureContentAreaRect() : null
  )

  useEffect(() => {
    if (!enabled) {
      setRect(null)
      return
    }

    const sync = () => {
      setRect(measureContentAreaRect())
    }

    sync()

    const inset = document.querySelector('[data-slot="sidebar-inset"]')
    const observer = inset ? new ResizeObserver(sync) : null
    if (inset) observer?.observe(inset)

    window.addEventListener('resize', sync)
    window.visualViewport?.addEventListener('resize', sync)

    return () => {
      observer?.disconnect()
      window.removeEventListener('resize', sync)
      window.visualViewport?.removeEventListener('resize', sync)
    }
  }, [enabled])

  return rect
}
