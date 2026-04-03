import { useState, useEffect } from 'preact/hooks'
import { api, Approval } from '../api/client'
import { PageHeader } from '../components/PageHeader'
import { ErrorBanner } from '../components/ErrorBanner'

interface Props {
  onBadgeChange?: (count: number) => void
  onApproved?: () => void
}

// ── Lifecycle ────────────────────────────────────────────────────────────────

type LifecycleStatus = 'pending' | 'approved' | 'running' | 'completed' | 'failed' | 'denied' | 'cancelled'

function lifecycleStatus(a: Approval): LifecycleStatus {
  const ds = a.deferredExecutionStatus
  if (ds) {
    switch (ds) {
      case 'pending_approval': return 'pending'
      case 'approved':        return 'approved'
      case 'running':         return 'running'
      case 'completed':       return 'completed'
      case 'failed':          return 'failed'
      case 'denied':          return 'denied'
      case 'cancelled':       return 'cancelled'
    }
  }
  switch (a.status) {
    case 'pending':  return 'pending'
    case 'approved': return 'approved'
    case 'denied':   return 'denied'
    default:         return 'pending'
  }
}

function isActionable(a: Approval): boolean {
  return lifecycleStatus(a) === 'pending'
}

function isVisible(a: Approval): boolean {
  const ls = lifecycleStatus(a)
  return ls !== 'completed' && ls !== 'cancelled' && ls !== 'denied'
}

// ── Display helpers ──────────────────────────────────────────────────────────

function lifecycleBadge(a: Approval) {
  const ls = lifecycleStatus(a)
  switch (ls) {
    case 'pending':   return <span class="badge badge-yellow">Pending</span>
    case 'approved':  return <span class="badge badge-blue">Approved</span>
    case 'running':   return <span class="badge badge-blue">Running</span>
    case 'completed': return <span class="badge badge-green">Completed</span>
    case 'failed':    return <span class="badge badge-red">Failed</span>
    case 'denied':    return <span class="badge badge-gray">Denied</span>
    case 'cancelled': return <span class="badge badge-gray">Cancelled</span>
  }
}

function permAccentClass(level: string): string {
  switch (level) {
    case 'read':    return 'perm-accent-read'
    case 'draft':   return 'perm-accent-draft'
    case 'execute': return 'perm-accent-execute'
    default:        return ''
  }
}

function actionSummary(a: Approval): string {
  const name = a.toolCall.toolName
  let args: Record<string, unknown> = {}
  try { args = JSON.parse(a.toolCall.argumentsJSON) } catch { /* ignore */ }

  if ((name.includes('fs__write_file') || name === 'fs.write_file') && args.path)
    return `Write ${lastName(String(args.path))}`
  if ((name.includes('fs__patch_file') || name === 'fs.patch_file') && args.path)
    return `Patch ${lastName(String(args.path))}`
  if ((name.includes('fs__create_directory') || name === 'fs.create_directory') && args.path)
    return `Create directory ${lastName(String(args.path))}`
  if (name.includes('system_open_app') && args.appName)
    return `Open ${args.appName}`
  if (name.includes('system_open_file') && args.path)
    return `Open ${lastName(String(args.path))}`
  if (name.includes('system_open_folder') && args.path)
    return `Open folder ${lastName(String(args.path))}`
  if (name.includes('system_reveal_in_finder') && args.path)
    return `Reveal ${lastName(String(args.path))} in Finder`
  if (name.includes('system_copy_to_clipboard') && args.text)
    return `Copy ${String(args.text).length} characters to clipboard`
  if (name.includes('system_send_notification') && args.title)
    return `Send notification: ${args.title}`

  const lower = a.toolCall.argumentsJSON.toLowerCase()
  if (lower.includes('path')) return 'Review file access'
  if (lower.includes('url'))  return 'Review network access'
  if (lower.includes('text')) return 'Review text action'
  return 'Review requested action'
}

// ── DiffViewer ───────────────────────────────────────────────────────────────

function DiffViewer({ diff }: { diff: string }) {
  const lines = diff.split('\n')
  // Trim a trailing empty line from the final \n
  const trimmed = lines[lines.length - 1] === '' ? lines.slice(0, -1) : lines
  return (
    <div class="diff-viewer">
      {trimmed.map((line, i) => {
        const cls =
          line.startsWith('+') && !line.startsWith('+++') ? 'diff-add' :
          line.startsWith('-') && !line.startsWith('---') ? 'diff-del' :
          line.startsWith('@@')                           ? 'diff-hunk' :
          (line.startsWith('---') || line.startsWith('+++')) ? 'diff-file' :
          'diff-ctx'
        return (
          <div key={i} class={`diff-line ${cls}`}>
            {line || '\u00a0'}
          </div>
        )
      })}
    </div>
  )
}

function lastName(path: string): string {
  const parts = path.split('/').filter(Boolean)
  return parts[parts.length - 1] ?? path
}

function timeAgo(iso: string): string {
  const s = Math.floor((Date.now() - new Date(iso).getTime()) / 1000)
  if (s < 60)    return `${s}s ago`
  if (s < 3600)  return `${Math.floor(s / 60)}m ago`
  if (s < 86400) return `${Math.floor(s / 3600)}h ago`
  return `${Math.floor(s / 86400)}d ago`
}

function formatJSON(json: string): string {
  try { return JSON.stringify(JSON.parse(json), null, 2) } catch { return json }
}

function toolLabel(toolName: string): string {
  return toolName.replace(/^skill__/, '').replace(/__/g, ' › ')
}

// ── Row component ────────────────────────────────────────────────────────────

interface RowProps {
  approval: Approval
  selected: boolean
  acting: boolean
  onSelect: () => void
  onApprove: () => void
  onDeny: () => void
  last: boolean
}

function ApprovalRow({ approval: a, selected, acting, onSelect, onApprove, onDeny, last }: RowProps) {
  const [expanded, setExpanded] = useState(false)
  const actionable = isActionable(a)
  const ls = lifecycleStatus(a)

  return (
    <div class={`approval-row-wrap ${permAccentClass(a.toolCall.permissionLevel)}${ls === 'failed' ? ' approval-row-failed' : ''}${last && !expanded ? ' last' : ''}`}>
      <div class="approval-row">

        {/* Selection checkbox — only for actionable */}
        <div class="approval-check-col">
          <button
            class={`approval-check${selected ? ' checked' : ''}`}
            onClick={onSelect}
            disabled={!actionable}
            title={actionable ? (selected ? 'Deselect' : 'Select') : undefined}
          >
            {selected
              ? <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><polyline points="1.5,5 4,7.5 8.5,2.5" /></svg>
              : null}
          </button>
        </div>

        {/* Main content */}
        <div class="approval-row-body">
          <div class="approval-row-title-row">
            <span class="approval-title">{actionSummary(a)}</span>
            {lifecycleBadge(a)}
          </div>
          <div class="approval-tool-label">{toolLabel(a.toolCall.toolName)}</div>
          <div class="approval-timestamp">
            {timeAgo(a.createdAt)}
            {a.resolvedAt && <> · resolved {timeAgo(a.resolvedAt)}</>}
            {a.conversationID && <> · <span style={{ fontFamily: 'var(--font-mono)' }}>{a.conversationID.slice(0, 8)}</span></>}
          </div>
          {a.lastError && <div class="approval-error-text">{a.lastError}</div>}
        </div>

        {/* Actions */}
        <div class="approval-row-actions">
          <button
            class={`btn btn-sm btn-icon btn-ghost${expanded ? ' btn-active' : ''}`}
            onClick={() => setExpanded(v => !v)}
            title="View parameters"
          >
            <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round">
              {expanded
                ? <polyline points="2,8 6,4 10,8" />
                : <polyline points="2,4 6,8 10,4" />}
            </svg>
          </button>

          {actionable && (
            <>
              <button class="btn btn-sm btn-primary" disabled={acting} onClick={onApprove}>
                {acting ? <span class="spinner spinner-sm" /> : 'Approve'}
              </button>
              <button class="btn btn-sm btn-ghost btn-deny" disabled={acting} onClick={onDeny}>
                Deny
              </button>
            </>
          )}
        </div>
      </div>

      {/* Expanded parameters / diff */}
      {expanded && (
        <div class="approval-params-panel">
          {a.previewDiff ? (
            <>
              <div class="surface-eyebrow">Changes</div>
              <DiffViewer diff={a.previewDiff} />
            </>
          ) : (
            <>
              <div class="surface-eyebrow">Parameters</div>
              <pre class="args-block">{formatJSON(a.toolCall.argumentsJSON)}</pre>
            </>
          )}
        </div>
      )}
    </div>
  )
}

// ── Main screen ──────────────────────────────────────────────────────────────

export function Approvals({ onBadgeChange, onApproved }: Props) {
  const [approvals, setApprovals]             = useState<Approval[]>([])
  const [loading, setLoading]                 = useState(true)
  const [error, setError]                     = useState<string | null>(null)
  const [acting, setActing]                   = useState<Set<string>>(new Set())
  const [selected, setSelected]               = useState<Set<string>>(new Set())
  const [resolvedExpanded, setResolvedExpanded] = useState(false)
  const [confirmBulkApprove, setConfirmBulkApprove] = useState(false)
  const [confirmBulkDeny, setConfirmBulkDeny]       = useState(false)

  const load = async () => {
    try {
      const data = await api.approvals()
      setApprovals(data)
      onBadgeChange?.(data.filter(a => a.status === 'pending').length)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load approvals.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
    const interval = setInterval(load, 5000)
    return () => clearInterval(interval)
  }, [])

  const act = async (toolCallID: string, action: 'approve' | 'deny') => {
    setActing(prev => new Set(prev).add(toolCallID))
    try {
      action === 'approve' ? await api.approve(toolCallID) : await api.deny(toolCallID)
      await load()
      if (action === 'approve') onApproved?.()
    } catch (err) {
      setError(err instanceof Error ? err.message : `Failed to ${action}.`)
    } finally {
      setActing(prev => { const s = new Set(prev); s.delete(toolCallID); return s })
    }
  }

  const bulkAct = async (action: 'approve' | 'deny') => {
    const targets = pendingItems.filter(a => selected.has(a.id))
    for (const a of targets) {
      try {
        action === 'approve' ? await api.approve(a.toolCall.id) : await api.deny(a.toolCall.id)
      } catch { /* continue */ }
    }
    setSelected(new Set())
    await load()
  }

  const clearFailed = async () => {
    for (const a of approvals.filter(a => lifecycleStatus(a) === 'failed')) {
      try { await api.deny(a.toolCall.id) } catch { /* continue */ }
    }
    await load()
  }

  const clearAll = async () => {
    for (const a of approvals.filter(a => {
      const ls = lifecycleStatus(a)
      return ls !== 'completed' && ls !== 'cancelled' && ls !== 'denied' &&
             ls !== 'running' && ls !== 'approved'
    })) {
      try { await api.deny(a.toolCall.id) } catch { /* continue */ }
    }
    setSelected(new Set())
    await load()
  }

  const toggleSelect = (id: string) => {
    setSelected(prev => {
      const s = new Set(prev)
      s.has(id) ? s.delete(id) : s.add(id)
      return s
    })
  }

  const toggleSelectAll = () => {
    setSelected(selected.size === pendingItems.length
      ? new Set()
      : new Set(pendingItems.map(a => a.id)))
  }

  const visible         = approvals.filter(isVisible)
  const pendingItems    = visible.filter(a => isActionable(a))
  const resolvedItems   = visible.filter(a => !isActionable(a))
  const selectedPending = pendingItems.filter(a => selected.has(a.id))

  if (loading) {
    return (
      <div class="screen">
        <PageHeader title="Approvals" subtitle="Review actions Atlas needs your sign-off on" />
        <div style={{ display: 'flex', justifyContent: 'center', padding: '48px' }}>
          <span class="spinner" />
        </div>
      </div>
    )
  }

  return (
    <div class="screen">
      <PageHeader title="Approvals" subtitle="Review actions Atlas needs your sign-off on" />

      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      {visible.length === 0 && (
        <div class="empty-state">
          <svg class="empty-icon" viewBox="0 0 36 36" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="18" cy="18" r="13" />
            <path d="M12 18l4 4 8-8" />
          </svg>
          <h3>All clear</h3>
          <p>When Atlas needs your approval for an action, it will appear here.</p>
        </div>
      )}

      {/* Pending section */}
      {pendingItems.length > 0 && (
        <div>
          <div class="section-label approvals-section-head">
            <span>Pending · {pendingItems.length}</span>
            <div class="approvals-section-actions">
              <button class="btn btn-sm btn-ghost approvals-head-btn" onClick={toggleSelectAll}>
                {selected.size === pendingItems.length ? 'Deselect all' : 'Select all'}
              </button>
              <button class="btn btn-sm btn-ghost approvals-head-btn approvals-clear-btn" onClick={clearAll}>
                Clear all
              </button>
            </div>
          </div>

          <div class="card approvals-list-card">
            {pendingItems.map((a, i) => (
              <ApprovalRow
                key={a.id}
                approval={a}
                selected={selected.has(a.id)}
                acting={acting.has(a.toolCall.id)}
                last={i === pendingItems.length - 1}
                onSelect={() => toggleSelect(a.id)}
                onApprove={() => act(a.toolCall.id, 'approve')}
                onDeny={() => act(a.toolCall.id, 'deny')}
              />
            ))}
          </div>

          {/* Bulk action bar — inline confirm replaces the buttons in place */}
          {selectedPending.length > 0 && (
            <div class="approvals-bulk-bar">
              {confirmBulkApprove ? (
                <div class="approvals-confirm-strip">
                  <span class="approvals-confirm-label">Approve {selectedPending.length} item{selectedPending.length !== 1 ? 's' : ''}?</span>
                  <button class="btn btn-sm btn-primary" onClick={() => { setConfirmBulkApprove(false); bulkAct('approve') }}>Confirm</button>
                  <button class="btn btn-sm btn-ghost" onClick={() => setConfirmBulkApprove(false)}>Cancel</button>
                </div>
              ) : confirmBulkDeny ? (
                <div class="approvals-confirm-strip">
                  <span class="approvals-confirm-label">Deny {selectedPending.length} item{selectedPending.length !== 1 ? 's' : ''}?</span>
                  <button class="btn btn-sm btn-danger" onClick={() => { setConfirmBulkDeny(false); bulkAct('deny') }}>Confirm</button>
                  <button class="btn btn-sm btn-ghost" onClick={() => setConfirmBulkDeny(false)}>Cancel</button>
                </div>
              ) : (
                <>
                  <span class="approvals-selected-count">{selectedPending.length} selected</span>
                  <button class="btn btn-sm btn-primary" onClick={() => setConfirmBulkApprove(true)}>
                    Approve {selectedPending.length}
                  </button>
                  <button class="btn btn-sm btn-ghost btn-deny" onClick={() => setConfirmBulkDeny(true)}>
                    Deny {selectedPending.length}
                  </button>
                </>
              )}
            </div>
          )}
        </div>
      )}

      {/* Resolved section — collapsible, collapsed by default */}
      {resolvedItems.length > 0 && (
        <div style={{ marginTop: pendingItems.length > 0 ? '8px' : '0' }}>
          <div class="section-label approvals-section-head">
            <button class="approvals-section-toggle" onClick={() => setResolvedExpanded(v => !v)}>
              <svg width="11" height="11" viewBox="0 0 11 11" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round">
                {resolvedExpanded
                  ? <polyline points="1,7.5 5.5,3 10,7.5" />
                  : <polyline points="1,3.5 5.5,8 10,3.5" />}
              </svg>
              Resolved · {resolvedItems.length}
            </button>
            {resolvedExpanded && resolvedItems.some(a => lifecycleStatus(a) === 'failed') && (
              <button class="btn btn-sm btn-ghost approvals-head-btn" onClick={clearFailed}>
                Clear failed
              </button>
            )}
          </div>
          {resolvedExpanded && (
            <div class="card approvals-list-card">
              {resolvedItems.map((a, i) => (
                <ApprovalRow
                  key={a.id}
                  approval={a}
                  selected={false}
                  acting={false}
                  last={i === resolvedItems.length - 1}
                  onSelect={() => {}}
                  onApprove={() => {}}
                  onDeny={() => {}}
                />
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
