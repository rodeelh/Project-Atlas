import { useState, useEffect, useCallback } from 'preact/hooks'
import { api, DashboardDisplayItem, DashboardDisplayPayload, DashboardSpec, DashboardWidget, WidgetExecutionResult } from '../api/client'
import { PageHeader } from '../components/PageHeader'
import { ErrorBanner } from '../components/ErrorBanner'

/* ── Shared widget card shell ─────────────────────────────── */

function WidgetCard({
  title,
  meta,
  children,
}: {
  title: string
  meta?: string
  children: preact.ComponentChild
}) {
  return (
    <div class="surface-card dashboard-widget-card">
      <div class="surface-eyebrow">{title}</div>
      {children}
      {meta && (
        <div class="surface-meta">{meta}</div>
      )}
    </div>
  )
}

function WidgetLoading() {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '4px 0' }}>
      <span class="spinner" style={{ width: '13px', height: '13px' }} />
      <span style={{ fontSize: '12px', color: 'var(--text-2)' }}>Loading…</span>
    </div>
  )
}

function WidgetError({ message }: { message?: string | null }) {
  return (
    <ErrorBanner error={message ?? 'Failed to load widget data.'} small />
  )
}

function summarizeListItem(item: unknown): string {
  if (typeof item === 'string') return item
  if (typeof item === 'number' || typeof item === 'boolean') return String(item)
  if (!item || typeof item !== 'object') return String(item ?? '')

  const record = item as Record<string, unknown>
  const title = typeof record.title === 'string' ? record.title : undefined
  const name = typeof record.name === 'string' ? record.name : undefined
  const summary = typeof record.summary === 'string' ? record.summary : undefined
  const snippet = typeof record.snippet === 'string' ? record.snippet : undefined
  const domain = typeof record.domain === 'string' ? record.domain : undefined
  const url = typeof record.url === 'string' ? record.url : undefined

  if (title && domain) return `${title} · ${domain}`
  if (title && snippet) return `${title} — ${snippet}`
  if (title) return title
  if (name && summary) return `${name} — ${summary}`
  if (name) return name
  if (summary) return summary
  if (snippet) return snippet
  if (url) return url

  return JSON.stringify(item)
}

function extractListItems(text: string): string[] {
  try {
    const parsed = JSON.parse(text)
    if (Array.isArray(parsed)) {
      return parsed.map(summarizeListItem).filter(Boolean)
    }

    if (parsed && typeof parsed === 'object') {
      const record = parsed as Record<string, unknown>
      for (const key of ['results', 'items', 'sources', 'keyPoints', 'caveats']) {
        const candidate = record[key]
        if (Array.isArray(candidate)) {
          return candidate.map(summarizeListItem).filter(Boolean)
        }
      }

      const firstArray = Object.values(record).find(Array.isArray)
      if (Array.isArray(firstArray)) {
        return firstArray.map(summarizeListItem).filter(Boolean)
      }
    }
  } catch {
    // Plain text fallback below.
  }

  return text ? [text] : []
}

function fallbackDisplayPayload(result: WidgetExecutionResult | null, widget: DashboardWidget): DashboardDisplayPayload | null {
  if (!result) return null
  if (result.displayPayload) return result.displayPayload

  switch (widget.type) {
    case 'stat_card':
      return { value: result.extractedValue ?? result.rawOutput }
    case 'summary':
      return { summary: result.extractedValue ?? result.rawOutput }
    case 'list': {
      const text = result.extractedValue ?? result.rawOutput
      const items = extractListItems(text).map(primaryText => ({ primaryText }))
      return items.length > 0 ? { items } : null
    }
    case 'table': {
      const text = result.extractedValue ?? result.rawOutput
      try {
        const parsed = JSON.parse(text)
        if (Array.isArray(parsed)) {
          return {
            rows: parsed.map(row => ({
              values: Array.isArray(row) ? row.map(String) : [String(row)],
            })),
          }
        }
      } catch {
        return text ? { rows: [{ values: [text] }] } : null
      }
      return null
    }
    default:
      return null
  }
}

function renderListItem(item: DashboardDisplayItem) {
  const content = (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '3px' }}>
      <span>{item.primaryText}</span>
      {item.secondaryText && (
        <span style={{ color: 'var(--text-2)', fontSize: '12px' }}>{item.secondaryText}</span>
      )}
      {item.tertiaryText && (
        <span style={{ color: 'var(--text-3)', fontSize: '11px' }}>{item.tertiaryText}</span>
      )}
    </div>
  )

  if (item.linkURL) {
    return (
      <a
        href={item.linkURL}
        target="_blank"
        rel="noreferrer"
        style={{ color: 'inherit', textDecoration: 'none', display: 'block' }}
      >
        {content}
      </a>
    )
  }
  return content
}

/* ── Hook: auto-fetch widget data on mount ───────────────── */

function useWidgetData(dashboardID: string, widget: DashboardWidget) {
  const [loading, setLoading] = useState(true)
  const [result, setResult] = useState<WidgetExecutionResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setError(null)
    api.executeWidget(dashboardID, widget.id).then(r => {
      if (!cancelled) { setResult(r); setLoading(false) }
    }).catch((e: Error) => {
      if (!cancelled) { setError(e.message); setLoading(false) }
    })
    return () => { cancelled = true }
  }, [dashboardID, widget.id])

  return { loading, result, error }
}

/* ── Auto-fetch widget renderers ─────────────────────────── */

function StatCardWidget({ widget, dashboardID }: { widget: DashboardWidget; dashboardID: string }) {
  const { loading, result, error } = useWidgetData(dashboardID, widget)
  const meta = `via ${widget.skillID}${widget.action ? ` · ${widget.action}` : ''}`

  if (loading) {
    return (
      <WidgetCard title={widget.title} meta={meta}>
        <WidgetLoading />
      </WidgetCard>
    )
  }
  if (error || !result?.success) {
    return (
      <WidgetCard title={widget.title} meta={meta}>
        <WidgetError message={error ?? result?.error} />
      </WidgetCard>
    )
  }

  const displayValue = result.displayPayload?.value ?? result.extractedValue ?? result.rawOutput

  return (
    <WidgetCard title={widget.title} meta={meta}>
      <div style={{ fontSize: '28px', fontWeight: 600, color: 'var(--text)', lineHeight: 1 }}>
        {displayValue || widget.emptyMessage || '—'}
      </div>
    </WidgetCard>
  )
}

function SummaryWidget({ widget, dashboardID }: { widget: DashboardWidget; dashboardID: string }) {
  const { loading, result, error } = useWidgetData(dashboardID, widget)
  const meta = `via ${widget.skillID}${widget.action ? ` · ${widget.action}` : ''}`

  return (
    <div class="surface-card dashboard-widget-card">
      <div class="surface-title" style={{ marginBottom: '8px' }}>{widget.title}</div>
      {loading ? (
        <WidgetLoading />
      ) : (error || !result?.success) ? (
        <WidgetError message={error ?? result?.error} />
      ) : (
        <div class="surface-copy">
          {(result.displayPayload?.summary ?? result.extractedValue ?? result.rawOutput) || (widget.emptyMessage ?? 'No data available.')}
        </div>
      )}
      <div class="surface-meta" style={{ marginTop: '10px' }}>
        {meta}
      </div>
    </div>
  )
}

function ListWidget({ widget, dashboardID }: { widget: DashboardWidget; dashboardID: string }) {
  const { loading, result, error } = useWidgetData(dashboardID, widget)
  const meta = `via ${widget.skillID}${widget.action ? ` · ${widget.action}` : ''}`

  const payload = fallbackDisplayPayload(result, widget)
  const items = payload?.items ?? []

  return (
    <div class="surface-card dashboard-widget-shell">
      <div style={{ padding: '14px 16px 10px', borderBottom: '1px solid var(--border)' }}>
        <span class="surface-title">{widget.title}</span>
      </div>
      {loading ? (
        <div style={{ padding: '12px 16px' }}><WidgetLoading /></div>
      ) : (error || !result?.success) ? (
        <div style={{ padding: '12px 16px' }}><WidgetError message={error ?? result?.error} /></div>
      ) : items.length > 0 ? (
        <div>
          {items.map((item, i) => (
            <div key={i} style={{
              padding: '9px 16px',
              fontSize: '13px',
              color: 'var(--text)',
              borderBottom: i < items.length - 1 ? '1px solid var(--border)' : 'none',
            }}>
              {renderListItem(item)}
            </div>
          ))}
        </div>
      ) : (
        <div style={{ padding: '12px 16px', color: 'var(--text-2)', fontSize: '12px', fontStyle: 'italic' }}>
          {widget.emptyMessage ?? 'No items to display.'}
        </div>
      )}
      <div class="surface-meta" style={{ padding: '6px 16px 12px' }}>
        {meta}
      </div>
    </div>
  )
}

function TableWidget({ widget, dashboardID }: { widget: DashboardWidget; dashboardID: string }) {
  const { loading, result, error } = useWidgetData(dashboardID, widget)
  const columns = widget.columns ?? []
  const meta = `via ${widget.skillID}${widget.action ? ` · ${widget.action}` : ''}`

  // Try to parse result as an array of rows
  const payload = fallbackDisplayPayload(result, widget)
  const rows = payload?.rows?.map(row => row.values) ?? []

  return (
    <div class="surface-card dashboard-widget-shell">
      <div style={{ padding: '14px 16px 10px', borderBottom: '1px solid var(--border)' }}>
        <span class="surface-title">{widget.title}</span>
      </div>
      {columns.length > 0 && (
        <div style={{
          display: 'grid',
          gridTemplateColumns: `repeat(${columns.length}, 1fr)`,
          borderBottom: '1px solid var(--border)',
          background: 'color-mix(in srgb, var(--surface) 88%, transparent)',
        }}>
          {columns.map(col => (
            <div key={col} style={{
              padding: '8px 12px',
              fontSize: '11px',
              fontWeight: 600,
              color: 'var(--text-2)',
              textTransform: 'uppercase',
              letterSpacing: '0.04em',
            }}>
              {col}
            </div>
          ))}
        </div>
      )}
      {loading ? (
        <div style={{ padding: '12px 16px' }}><WidgetLoading /></div>
      ) : (error || !result?.success) ? (
        <div style={{ padding: '12px 16px' }}><WidgetError message={error ?? result?.error} /></div>
      ) : rows.length > 0 ? (
        rows.map((row, i) => (
          <div key={i} style={{
            display: 'grid',
            gridTemplateColumns: `repeat(${Math.max(columns.length, row.length)}, 1fr)`,
            borderBottom: i < rows.length - 1 ? '1px solid var(--border)' : 'none',
          }}>
            {row.map((cell, j) => (
              <div key={j} style={{ padding: '8px 12px', fontSize: '13px', color: 'var(--text)' }}>
                {cell}
              </div>
            ))}
          </div>
        ))
      ) : (
        <div style={{ padding: '12px 16px', color: 'var(--text-2)', fontSize: '12px', fontStyle: 'italic' }}>
          {widget.emptyMessage ?? 'No data available yet.'}
        </div>
      )}
      <div class="surface-meta" style={{ padding: '6px 16px 12px' }}>
        {meta}
      </div>
    </div>
  )
}

/* ── Interactive widget renderers ────────────────────────── */

function FormWidget({ widget, dashboardID }: { widget: DashboardWidget; dashboardID: string }) {
  const fields = widget.fields ?? []
  const [inputs, setInputs] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<WidgetExecutionResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  const handleSubmit = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const r = await api.executeWidget(dashboardID, widget.id, inputs)
      setResult(r)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [dashboardID, widget.id, inputs])

  return (
    <div class="surface-card dashboard-widget-card">
      <div class="surface-title" style={{ marginBottom: '14px' }}>{widget.title}</div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
        {fields.map(field => (
          <div key={field.key}>
            <label style={{ fontSize: '12px', color: 'var(--text-2)', display: 'block', marginBottom: '4px' }}>
              {field.label}
              {field.required && <span style={{ color: 'var(--c-red, #f87171)', marginLeft: '2px' }}>*</span>}
            </label>
            {field.type === 'select' && field.options ? (
              <select
                value={inputs[field.key] ?? ''}
                onChange={(e) => setInputs(prev => ({ ...prev, [field.key]: (e.target as HTMLSelectElement).value }))}
                style={{
                  width: '100%',
                  padding: '7px 10px',
                  background: 'var(--bg-2, rgba(255,255,255,0.04))',
                  border: '1px solid var(--border)',
                  borderRadius: '6px',
                  fontSize: '13px',
                  color: 'var(--text)',
                }}
              >
                <option value="">Select {field.label}…</option>
                {field.options.map(o => <option key={o} value={o}>{o}</option>)}
              </select>
            ) : (
              <input
                type={field.type === 'number' ? 'number' : field.type === 'date' ? 'date' : 'text'}
                value={inputs[field.key] ?? ''}
                onInput={(e) => setInputs(prev => ({ ...prev, [field.key]: (e.target as HTMLInputElement).value }))}
                placeholder={`Enter ${field.label.toLowerCase()}…`}
                style={{
                  width: '100%',
                  padding: '7px 10px',
                  background: 'var(--bg-2, rgba(255,255,255,0.04))',
                  border: '1px solid var(--border)',
                  borderRadius: '6px',
                  fontSize: '13px',
                  color: 'var(--text)',
                }}
              />
            )}
          </div>
        ))}
      </div>
      <button
        class="btn btn-primary btn-sm"
        onClick={handleSubmit}
        disabled={loading}
        style={{ marginTop: '14px' }}
      >
        {loading ? <span class="spinner spinner-sm" /> : 'Submit'}
      </button>
      {error && (
        <ErrorBanner error={error} onDismiss={() => setError(null)} small />
      )}
      {result && (
        <div style={{
          marginTop: '12px',
          padding: '10px 12px',
          background: 'color-mix(in srgb, var(--surface-2) 92%, transparent)',
          border: '1px solid color-mix(in srgb, var(--border) 90%, transparent)',
          borderRadius: '10px',
          fontSize: '13px',
          color: 'var(--text)',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
        }}>
          {(result.extractedValue ?? result.rawOutput) || (widget.emptyMessage ?? 'No result.')}
        </div>
      )}
      <div class="surface-meta" style={{ marginTop: '10px' }}>
        via {widget.skillID}{widget.action && ` · ${widget.action}`}
      </div>
    </div>
  )
}

function SearchWidget({ widget, dashboardID }: { widget: DashboardWidget; dashboardID: string }) {
  const fields = widget.fields ?? []
  const primaryField = fields[0]
  const [query, setQuery] = useState('')
  const [loading, setLoading] = useState(false)
  const [result, setResult] = useState<WidgetExecutionResult | null>(null)
  const [error, setError] = useState<string | null>(null)

  const handleSearch = useCallback(async () => {
    if (!query.trim()) return
    setLoading(true)
    setError(null)
    try {
      const inputs: Record<string, string> = {}
      if (primaryField) inputs[primaryField.key] = query
      else inputs['query'] = query
      const r = await api.executeWidget(dashboardID, widget.id, inputs)
      setResult(r)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setLoading(false)
    }
  }, [dashboardID, widget.id, query, primaryField])

  const handleKeyDown = useCallback((e: KeyboardEvent) => {
    if (e.key === 'Enter') handleSearch()
  }, [handleSearch])

  return (
    <div class="surface-card dashboard-widget-card">
      <div class="surface-title" style={{ marginBottom: '12px' }}>{widget.title}</div>
      <div style={{ display: 'flex', gap: '8px', marginBottom: '12px' }}>
        <input
          type="text"
          value={query}
          onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
          onKeyDown={handleKeyDown}
          placeholder={primaryField ? `Search ${primaryField.label.toLowerCase()}…` : 'Search…'}
          style={{
            flex: 1,
            padding: '7px 10px',
            background: 'var(--bg-2, rgba(255,255,255,0.04))',
            border: '1px solid var(--border)',
            borderRadius: '6px',
            fontSize: '13px',
            color: 'var(--text)',
          }}
        />
        <button
          class="btn btn-sm"
          onClick={handleSearch}
          disabled={loading || !query.trim()}
        >
          {loading ? <span class="spinner spinner-sm" /> : 'Search'}
        </button>
      </div>
      {error && (
        <ErrorBanner error={error} onDismiss={() => setError(null)} small />
      )}
      {result ? (
        <div style={{
          padding: '10px 12px',
          background: 'color-mix(in srgb, var(--surface-2) 92%, transparent)',
          border: '1px solid color-mix(in srgb, var(--border) 90%, transparent)',
          borderRadius: '10px',
          fontSize: '13px',
          color: 'var(--text)',
          whiteSpace: 'pre-wrap',
          wordBreak: 'break-word',
          maxHeight: '200px',
          overflowY: 'auto',
        }}>
          {(result.extractedValue ?? result.rawOutput) || (widget.emptyMessage ?? 'No results found.')}
        </div>
      ) : !loading && (
        <div style={{ color: 'var(--text-2)', fontSize: '12px', fontStyle: 'italic' }}>
          {widget.emptyMessage ?? 'Search results will appear here.'}
        </div>
      )}
      <div class="surface-meta" style={{ marginTop: '10px' }}>
        via {widget.skillID}{widget.action && ` · ${widget.action}`}
      </div>
    </div>
  )
}

/* ── Widget dispatcher ───────────────────────────────────── */

function WidgetRenderer({ widget, dashboardID }: { widget: DashboardWidget; dashboardID: string }) {
  switch (widget.type) {
    case 'stat_card': return <StatCardWidget widget={widget} dashboardID={dashboardID} />
    case 'summary':   return <SummaryWidget widget={widget} dashboardID={dashboardID} />
    case 'list':      return <ListWidget widget={widget} dashboardID={dashboardID} />
    case 'table':     return <TableWidget widget={widget} dashboardID={dashboardID} />
    case 'form':      return <FormWidget widget={widget} dashboardID={dashboardID} />
    case 'search':    return <SearchWidget widget={widget} dashboardID={dashboardID} />
    default: return (
      <div class="surface-card dashboard-widget-card" style={{ color: 'var(--text-2)', fontSize: '12px' }}>
        Unknown widget type: {(widget as DashboardWidget).type}
      </div>
    )
  }
}

/* ── Main DashboardView Screen ───────────────────────────── */

interface DashboardViewProps {
  dashboardID: string
  onBack: () => void
}

export function DashboardView({ dashboardID, onBack }: DashboardViewProps) {
  const [spec, setSpec]       = useState<DashboardSpec | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError]     = useState<string | null>(null)
  const [pinning, setPinning] = useState(false)

  useEffect(() => {
    let cancelled = false
    const load = async () => {
      try {
        const dashboards = await api.installedDashboards()
        const found = dashboards.find(d => d.id === dashboardID)
        if (!cancelled) {
          if (found) {
            setSpec(found)
          } else {
            setError(`Dashboard '${dashboardID}' not found.`)
          }
          setLoading(false)
        }
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : 'Failed to load dashboard.')
          setLoading(false)
        }
      }
    }
    load()
    return () => { cancelled = true }
  }, [dashboardID])

  const handleTogglePin = useCallback(async () => {
    if (!spec || pinning) return
    setPinning(true)
    try {
      const updated = await api.toggleDashboardPin(spec.id)
      setSpec(updated)
    } catch {
      // silently ignore — pin state already shown optimistically
    } finally {
      setPinning(false)
    }
  }, [spec, pinning])

  if (loading) {
    return (
      <div class="screen">
        <PageHeader title="Dashboard" subtitle="Loading…" />
        <div style={{ display: 'flex', justifyContent: 'center', padding: '48px' }}>
          <span class="spinner" />
        </div>
      </div>
    )
  }

  if (error || !spec) {
    return (
      <div class="screen">
        <PageHeader
          title="Dashboard"
          subtitle="Not found"
          actions={<button class="btn btn-sm" onClick={onBack}>← Back</button>}
        />
        <ErrorBanner error={error ?? 'Dashboard not found.'} />
      </div>
    )
  }

  return (
    <div class="screen">
      <PageHeader
        title={spec.title}
        subtitle={spec.description}
        actions={
          <>
            <button
              class={`btn btn-sm${spec.isPinned ? ' btn-active' : ''}`}
              onClick={handleTogglePin}
              disabled={pinning}
              title={spec.isPinned ? 'Unpin from sidebar' : 'Pin to sidebar'}
              style={{ display: 'flex', alignItems: 'center', gap: '5px' }}
            >
              <svg width="12" height="12" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <line x1="8" y1="1" x2="8" y2="11" />
                <path d="M4 7l4 4 4-4" />
                <line x1="4" y1="15" x2="12" y2="15" />
              </svg>
              {spec.isPinned ? 'Unpin' : 'Pin'}
            </button>
            <button class="btn btn-sm" onClick={onBack}>← Dashboards</button>
          </>
        }
      />

      {/* Widget grid — 2 columns on wide, 1 on narrow */}
      <div style={{
        display: 'grid',
        gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
        gap: '16px',
        marginTop: '4px',
      }}>
        {spec.widgets.map(widget => (
          <WidgetRenderer key={widget.id} widget={widget} dashboardID={spec.id} />
        ))}
      </div>

      {spec.widgets.length === 0 && (
        <div style={{ padding: '40px 0', textAlign: 'center', color: 'var(--text-2)', fontSize: '13px', fontStyle: 'italic' }}>
          {spec.emptyState ?? 'This dashboard has no widgets.'}
        </div>
      )}
    </div>
  )
}
