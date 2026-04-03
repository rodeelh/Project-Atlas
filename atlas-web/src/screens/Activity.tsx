import { useState, useEffect } from 'preact/hooks'
import { api, LogEntry, RuntimeStatus, RuntimeConfig, EngineStatus } from '../api/client'
import { PageHeader } from '../components/PageHeader'
import { ErrorBanner } from '../components/ErrorBanner'
import { formatAtlasModelName } from '../modelName'

// ── Formatters ─────────────────────────────────────────────────────────────────

function formatTime(iso: string): string {
  try {
    return new Date(iso).toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch { return iso }
}

function formatUptime(startedAt: string): string {
  const s = Math.floor((Date.now() - new Date(startedAt).getTime()) / 1000)
  if (s < 60) return `${s}s`
  const m = Math.floor(s / 60)
  if (m < 60) return `${m}m`
  const h = Math.floor(m / 60)
  const rm = m % 60
  if (h < 24) return rm > 0 ? `${h}h ${rm}m` : `${h}h`
  const d = Math.floor(h / 24)
  return `${d}d ${h % 24}h`
}

function formatRelative(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  if (diff < 60000) return 'just now'
  if (diff < 3600000) return `${Math.floor(diff / 60000)}m ago`
  if (diff < 86400000) return `${Math.floor(diff / 3600000)}h ago`
  return `${Math.floor(diff / 86400000)}d ago`
}

function levelClass(level: string): string {
  switch (level.toLowerCase()) {
    case 'debug':                return 'debug'
    case 'info':                 return 'info'
    case 'warning': case 'warn': return 'warn'
    case 'error': case 'fault':  return 'error'
    default:                     return 'info'
  }
}

function stateBadge(state: string) {
  switch (state.toLowerCase()) {
    case 'ready':    return <span class="badge badge-green">{state}</span>
    case 'starting': return <span class="badge badge-yellow">{state}</span>
    case 'degraded': return <span class="badge badge-yellow">{state}</span>
    case 'stopped':  return <span class="badge badge-red">{state}</span>
    default:         return <span class="badge badge-gray">{state}</span>
  }
}

function activeModelName(cfg: RuntimeConfig | null): string {
  if (!cfg) return '—'
  switch (cfg.activeAIProvider) {
    case 'anthropic':    return cfg.selectedAnthropicModel       || 'claude'
    case 'gemini':       return cfg.selectedGeminiModel          || 'gemini'
    case 'lm_studio':   return cfg.selectedLMStudioModel         || 'LM Studio'
    case 'ollama':       return cfg.selectedOllamaModel          || 'ollama'
    case 'atlas_engine': return formatAtlasModelName(cfg.selectedAtlasEngineModel || '') || 'Engine LM'
    default:             return cfg.selectedOpenAIPrimaryModel   || 'gpt-4.1-mini'
  }
}

type LogFilter = 'all' | 'info' | 'warn' | 'error'

// ── Component ──────────────────────────────────────────────────────────────────

export function Activity() {
  const [logs, setLogs]               = useState<LogEntry[]>([])
  const [status, setStatus]           = useState<RuntimeStatus | null>(null)
  const [config, setConfig]           = useState<RuntimeConfig | null>(null)
  const [engineStatus, setEngineStatus] = useState<EngineStatus | null>(null)
  const [loading, setLoading]         = useState(true)
  const [error, setError]             = useState<string | null>(null)
  const [logFilter, setLogFilter]     = useState<LogFilter>('all')

  const load = async () => {
    try {
      const [logData, statusData, configData, engineData] = await Promise.allSettled([
        api.logs(200), api.status(), api.config(), api.engineStatus(),
      ])
      if (logData.status === 'fulfilled')    setLogs(logData.value)
      if (statusData.status === 'fulfilled')  setStatus(statusData.value)
      if (configData.status === 'fulfilled')  setConfig(configData.value)
      if (engineData.status === 'fulfilled')  setEngineStatus(engineData.value)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load activity.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    const interval = setInterval(load, 10000)
    return () => clearInterval(interval)
  }, [])

  const filteredLogs = [...logs].reverse().filter(entry => {
    if (logFilter === 'all') return true
    const lv = entry.level.toLowerCase()
    if (logFilter === 'warn')  return lv === 'warn' || lv === 'warning'
    if (logFilter === 'error') return lv === 'error' || lv === 'fault'
    return lv === logFilter
  })

  if (loading) {
    return (
      <div class="screen">
        <PageHeader title="Activity" subtitle="Daemon health and event log" />
        <div style={{ display: 'flex', justifyContent: 'center', padding: '48px' }}>
          <span class="spinner" />
        </div>
      </div>
    )
  }

  return (
    <div class="screen">
      <PageHeader
        title="Activity"
        subtitle="Daemon health and event log"
        actions={
          <button class="btn btn-ghost btn-sm btn-icon" onClick={load} title="Refresh">
            <svg width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
              <path d="M1 4a7 7 0 0 1 13 2" /><path d="M15 12a7 7 0 0 1-13-2" />
              <polyline points="1,1 1,4 4,4" /><polyline points="15,15 15,12 12,12" />
            </svg>
          </button>
        }
      />

      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      {/* ── Stats ── */}
      <div class="card">
        <div class="stat-grid">
          {/* System */}
          <div class="stat-cell">
            <div class="stat-label">State</div>
            <div class="stat-value">{status ? stateBadge(status.state) : '—'}</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">Uptime</div>
            <div class="stat-value">{status?.startedAt ? formatUptime(status.startedAt) : '—'}</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">Port</div>
            <div class="stat-value" style={{ fontFamily: 'var(--font-mono)', fontSize: '13.5px' }}>{status?.runtimePort ?? '—'}</div>
          </div>
          {/* AI */}
          <div class="stat-cell">
            <div class="stat-label">Model</div>
            <div class="stat-value" style={{ fontFamily: 'var(--font-mono)', fontSize: '12px' }}>{activeModelName(config)}</div>
            <div class="stat-note">{config?.activeAIProvider ?? '—'}</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">Tokens In</div>
            <div class="stat-value" style={{ fontFamily: 'var(--font-mono)', fontSize: '13.5px' }}>
              {status?.tokensIn != null ? status.tokensIn.toLocaleString() : '—'}
            </div>
            <div class="stat-note">since restart</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">Tokens Out</div>
            <div class="stat-value" style={{ fontFamily: 'var(--font-mono)', fontSize: '13.5px' }}>
              {status?.tokensOut != null ? status.tokensOut.toLocaleString() : '—'}
            </div>
            <div class="stat-note">since restart</div>
          </div>
          {/* Engine LM — only when running */}
          {engineStatus?.running && (
            <div class="stat-cell">
              <div class="stat-label">Engine tok/s</div>
              <div class="stat-value" style={{ fontFamily: 'var(--font-mono)', fontSize: '13.5px' }}>
                {engineStatus.lastTPS != null && engineStatus.lastTPS > 0
                  ? engineStatus.lastTPS.toFixed(1)
                  : '—'}
              </div>
              <div class="stat-note">decode speed</div>
            </div>
          )}
          {engineStatus?.running && (
            <div class="stat-cell">
              <div class="stat-label">Engine ctx</div>
              <div class="stat-value" style={{ fontFamily: 'var(--font-mono)', fontSize: '13.5px' }}>
                {engineStatus.contextTokens != null && engineStatus.contextTokens > 0
                  ? engineStatus.contextTokens.toLocaleString()
                  : '—'}
              </div>
              <div class="stat-note">tokens in use</div>
            </div>
          )}
          {/* Activity */}
          <div class="stat-cell">
            <div class="stat-label">Conversations</div>
            <div class="stat-value">{status?.activeConversationCount ?? '—'}</div>
            <div class="stat-note">active</div>
          </div>
          <div class="stat-cell">
            <div class="stat-label">Pending Approvals</div>
            <div class="stat-value">{status?.pendingApprovalCount ?? '—'}</div>
            <div class="stat-note">awaiting review</div>
          </div>
        </div>
        {status?.lastError && (
          <div style={{ padding: '12px 20px' }}>
            <ErrorBanner error={status.lastError} />
          </div>
        )}
      </div>

      {/* ── Logs ── */}
      <div style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column' }}>
        <div class="section-label activity-log-header">
          <span>Logs</span>
          <div class="log-filter-tabs">
            {(['all', 'info', 'warn', 'error'] as LogFilter[]).map(f => (
              <button
                key={f}
                class={`log-filter-tab${logFilter === f ? ' active' : ''}`}
                onClick={() => setLogFilter(f)}
              >
                {f === 'all' ? 'All' : f.charAt(0).toUpperCase() + f.slice(1)}
              </button>
            ))}
          </div>
          <span class="activity-live">
            <span class="activity-live-dot" />
            live
          </span>
        </div>
        <div class="card" style={{ flex: 1, minHeight: 0, display: 'flex', flexDirection: 'column', overflow: 'hidden' }}>
          {filteredLogs.length === 0 ? (
            <div style={{ padding: '24px', textAlign: 'center', color: 'var(--text-3)', fontSize: '13px' }}>
              {logFilter === 'all' ? 'Send a message to start seeing activity logs' : `No ${logFilter} entries`}
            </div>
          ) : (
            <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '8px 0' }}>
              {filteredLogs.map(entry => {
                const isError = entry.level === 'error' || entry.level === 'fault'
                const metaStr = entry.metadata && Object.keys(entry.metadata).length > 0
                  ? '  ' + Object.entries(entry.metadata).map(([k, v]) => `${k}=${v}`).join('  ')
                  : ''
                return (
                  <div class={`log-entry${isError ? ' log-entry-error' : ''}`} key={entry.id}>
                    <span class="log-time">{formatTime(entry.timestamp)}</span>
                    <span class={`log-level-dot ${levelClass(entry.level)}`} title={entry.level} style={{ marginTop: '7px' }} />
                    <span class="log-message" title={entry.message + metaStr}>{entry.message}</span>
                  </div>
                )
              })}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
