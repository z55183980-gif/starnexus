export function isEmbeddedPlayground(): boolean {
  if (typeof window === 'undefined') return false

  const params = new URLSearchParams(window.location.search)
  if (params.get('embedded') === '1') return true

  try {
    return window.self !== window.top
  } catch {
    return true
  }
}

export function markEmbeddedPlayground(): void {
  if (!isEmbeddedPlayground()) return
  document.documentElement.classList.add('is-embedded')
}
