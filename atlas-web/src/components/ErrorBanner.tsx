import type { JSX } from 'preact/jsx-runtime'

interface ErrorBannerProps {
  error: string | null
  onDismiss?: () => void
  small?: boolean
}

export function ErrorBanner({ error, onDismiss, small }: ErrorBannerProps): JSX.Element | null {
  if (!error) return null
  return (
    <div class={`banner banner-error${small ? ' banner-sm' : ''}`}>
      <span class="banner-message">{error}</span>
      {onDismiss && (
        <button class="banner-dismiss" onClick={onDismiss} title="Dismiss">✕</button>
      )}
    </div>
  )
}
