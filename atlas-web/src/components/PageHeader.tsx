import { ComponentChildren } from 'preact'

interface PageHeaderProps {
  title: string
  subtitle: string
  actions?: ComponentChildren
}

/**
 * Shared top-bar used by every screen.
 * When rendered as a direct child of .screen it negates the screen padding
 * so the border-bottom spans edge-to-edge. Outside .screen (Chat) it sits
 * naturally as a flex sibling.
 */
export function PageHeader({ title, subtitle, actions }: PageHeaderProps) {
  return (
    <div class="page-header">
      <div class="page-header-left">
        <div class="page-header-title">
          <span class="page-header-prefix">Atlas</span>
          <span class="page-header-sep"> — </span>
          {title}
        </div>
        <div class="page-header-sub">{subtitle}</div>
      </div>
      {actions && <div class="page-header-actions">{actions}</div>}
    </div>
  )
}
