import { useEffect, useState } from 'react'

export function useEmbeddedViewportHeight(enabled: boolean): number {
  const [height, setHeight] = useState(() => (enabled ? window.innerHeight : 0))

  useEffect(() => {
    if (!enabled) return

    const sync = () => {
      setHeight(window.innerHeight)
    }

    sync()
    window.addEventListener('resize', sync)

    const visualViewport = window.visualViewport
    visualViewport?.addEventListener('resize', sync)

    return () => {
      window.removeEventListener('resize', sync)
      visualViewport?.removeEventListener('resize', sync)
    }
  }, [enabled])

  return height
}
