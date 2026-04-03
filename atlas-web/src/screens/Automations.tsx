import { useState, useEffect, useRef } from 'preact/hooks'
import { api, CommunicationChannel, GremlinItem, GremlinRun, WorkflowDefinition } from '../api/client'
import { PageHeader } from '../components/PageHeader'

// ── Icons ────────────────────────────────────────────────────────────────────

const RefreshIcon = () => (
  <svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <path d="M2.5 8a5.5 5.5 0 0 1 9.5-3.8" />
    <polyline points="13.5,2.5 13.5,6 10,6" />
    <path d="M13.5 8a5.5 5.5 0 0 1-9.5 3.8" />
    <polyline points="2.5,13.5 2.5,10 6,10" />
  </svg>
)

const PlayIcon = () => (
  <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <polygon points="3,1.5 10.5,6 3,10.5" fill="currentColor" stroke="none" />
  </svg>
)

const TrashIcon = () => (
  <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
    <path d="M2 3h8M4.5 3V2h3v1M10 3l-.75 7.5H2.75L2 3" />
  </svg>
)

// ── Emoji picker ─────────────────────────────────────────────────────────────

const EMOJI_OPTIONS = [
  '⚡', '🔁', '📋', '🗓', '📬', '🧠', '🚀', '🔔',
  '📊', '🔍', '📝', '✅', '🎯', '🔧', '⏰', '📁',
  '💡', '🤖', '🌐', '📤', '📥', '🔗', '🛠', '📌',
  '🏷', '💬', '📣', '⚙️', '🧩', '🌟', '🔐', '💾',
  '🖥', '📱', '🎉', '✉️', '📈', '🧪', '🔥', '💎',
]

function EmojiPicker({ value, onChange }: { value: string; onChange: (e: string) => void }) {
  const [open, setOpen] = useState(false)
  const [pos, setPos] = useState({ top: 0, left: 0 })
  const triggerRef = useRef<HTMLButtonElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)

  function handleToggle() {
    if (!open && triggerRef.current) {
      const r = triggerRef.current.getBoundingClientRect()
      setPos({ top: r.bottom + 6, left: r.left })
    }
    setOpen(o => !o)
  }

  useEffect(() => {
    if (!open) return
    function onDown(e: MouseEvent) {
      const t = e.target as Node
      if (!triggerRef.current?.contains(t) && !popoverRef.current?.contains(t)) setOpen(false)
    }
    document.addEventListener('mousedown', onDown)
    return () => document.removeEventListener('mousedown', onDown)
  }, [open])

  return (
    <div class="emoji-picker-wrap">
      <button ref={triggerRef} type="button" class="field-input emoji-picker-trigger" onClick={handleToggle} title="Choose emoji">
        {value}
      </button>
      {open && (
        <div ref={popoverRef} class="emoji-picker-popover" style={{ top: `${pos.top}px`, left: `${pos.left}px` }}>
          {EMOJI_OPTIONS.map(e => (
            <button
              key={e}
              type="button"
              class={`emoji-picker-option${value === e ? ' selected' : ''}`}
              onClick={() => { onChange(e); setOpen(false) }}
            >
              {e}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// ── Helpers ──────────────────────────────────────────────────────────────────

function statusBadge(status: string) {
  switch (status) {
    case 'success': return <span class="badge badge-green">{status}</span>
    case 'failed':  return <span class="badge badge-red">{status}</span>
    case 'running': return <span class="badge badge-yellow">{status}</span>
    case 'skipped': return <span class="badge badge-gray">{status}</span>
    default:        return <span class="badge badge-gray">{status}</span>
  }
}

function formatDate(iso?: string) {
  if (!iso) return '—'
  try { return new Date(iso).toLocaleString() } catch { return iso }
}

// ── Sub-component: Run history modal ─────────────────────────────────────────

function RunsPanel({ gremlin, onClose }: { gremlin: GremlinItem; onClose: () => void }) {
  const [runs, setRuns] = useState<GremlinRun[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    api.automationRuns(gremlin.id)
      .then(setRuns)
      .catch(() => setRuns([]))
      .finally(() => setLoading(false))
  }, [gremlin.id])

  return (
    <div class="modal-overlay" onClick={(e) => { if ((e.target as HTMLElement).classList.contains('modal-overlay')) onClose() }}>
      <div class="modal automation-modal" style={{ maxWidth: 640, width: '90vw' }}>
        <div class="modal-header">
          <div class="automation-modal-title-wrap">
            <div class="surface-eyebrow">Automation History</div>
            <h3 class="automation-modal-title">{gremlin.emoji} {gremlin.name}</h3>
          </div>
          <button class="btn btn-ghost btn-sm" onClick={onClose}>✕</button>
        </div>
        <div class="modal-body" style={{ maxHeight: 400, overflowY: 'auto' }}>
          {loading && <p class="empty-state">Loading…</p>}
          {!loading && runs.length === 0 && <p class="empty-state">No runs yet.</p>}
          {!loading && runs.map(run => (
            <div key={run.id} class="run-row automation-run-card">
              <div class="run-row-header">
                {statusBadge(run.status)}
                <span class="run-time">{formatDate(run.startedAt)}</span>
              </div>
              {(run.output || run.errorMessage) && (
                <pre class="run-output">{run.output ?? run.errorMessage}</pre>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

// ── Sub-component: Edit / Create modal ───────────────────────────────────────

interface EditModalProps {
  gremlin?: GremlinItem
  onSave: (item: GremlinItem) => Promise<void>
  onClose: () => void
}

function EditModal({ gremlin, onSave, onClose }: EditModalProps) {
  const [name, setName]               = useState(gremlin?.name ?? '')
  const [emoji, setEmoji]             = useState(gremlin?.emoji ?? '⚡')
  const [prompt, setPrompt]           = useState(gremlin?.prompt ?? '')
  const [schedule, setSchedule]       = useState(gremlin?.scheduleRaw ?? '')
  const [workflowID, setWorkflowID]   = useState(gremlin?.workflowID ?? '')
  const [workflowInputValues, setWorkflowInputValues] = useState(
    gremlin?.workflowInputValues ? JSON.stringify(gremlin.workflowInputValues, null, 2) : ''
  )
  const [destinationID, setDestinationID] = useState(
    gremlin?.communicationDestination?.id
      ?? (gremlin?.telegramChatID != null ? `telegram:${gremlin.telegramChatID}` : '')
  )
  const [knownChannels, setKnownChannels] = useState<CommunicationChannel[]>([])
  const [workflows, setWorkflows]     = useState<WorkflowDefinition[]>([])
  const [saving, setSaving]           = useState(false)
  const [error, setError]             = useState<string | null>(null)

  useEffect(() => {
    api.communicationChannels().then(setKnownChannels).catch(() => setKnownChannels([]))
    api.workflows().then(setWorkflows).catch(() => setWorkflows([]))
  }, [])

  async function handleSave() {
    if (!name.trim() || !schedule.trim()) {
      setError('Name and schedule are required.')
      return
    }
    const selectedChannel = knownChannels.find(channel => channel.id === destinationID)
    const selectedDestination = selectedChannel
      ? {
          id: selectedChannel.id,
          platform: selectedChannel.platform,
          channelID: selectedChannel.channelID,
          channelName: selectedChannel.channelName,
          userID: selectedChannel.userID,
        }
      : (gremlin?.communicationDestination?.id === destinationID ? gremlin.communicationDestination : undefined)
    let parsedWorkflowInputs: Record<string, string> | undefined
    if (workflowInputValues.trim()) {
      try {
        parsedWorkflowInputs = JSON.parse(workflowInputValues)
      } catch {
        setError('Workflow inputs must be valid JSON, for example {"path":"README.md"}.')
        return
      }
    }
    if (!workflowID && !prompt.trim()) {
      setError('Add either a saved workflow or a prompt.')
      return
    }
    setSaving(true)
    setError(null)
    try {
      const fmt = new Date().toISOString().slice(0, 10)
      const slugify = (s: string) =>
        s.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '')
      const item: GremlinItem = {
        id: gremlin?.id ?? slugify(name),
        name: name.trim(),
        emoji: emoji.trim() || '⚡',
        prompt: prompt.trim(),
        scheduleRaw: schedule.trim(),
        isEnabled: gremlin?.isEnabled ?? true,
        sourceType: gremlin?.sourceType ?? 'web',
        createdAt: gremlin?.createdAt ?? fmt,
        ...(workflowID ? { workflowID } : {}),
        ...(parsedWorkflowInputs ? { workflowInputValues: parsedWorkflowInputs } : {}),
        ...(selectedDestination ? { communicationDestination: selectedDestination } : {}),
      }
      await onSave(item)
      onClose()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Save failed.')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div class="modal-overlay" onClick={(e) => { if ((e.target as HTMLElement).classList.contains('modal-overlay')) onClose() }}>
      <div class="modal automation-modal" style={{ maxWidth: 820, width: '94vw' }}>
        <div class="modal-header">
          <div class="automation-modal-title-wrap">
            <div class="surface-eyebrow">Automation</div>
            <h3 class="automation-modal-title">{gremlin ? gremlin.name : 'Create automation'}</h3>
          </div>
          <button class="btn btn-ghost btn-sm" onClick={onClose}>✕</button>
        </div>
        <div class="modal-body automation-modal-body" style={{ maxHeight: 'calc(85vh - 130px)', overflowY: 'auto' }}>
          {error && <p class="error-banner">{error}</p>}

          {/* Emoji + Name — pinned-width emoji button beside full-width name input */}
          <div class="automation-form-grid" style={{ gridTemplateColumns: 'min-content minmax(0, 1fr)' }}>
            <div class="automation-field-group">
              <label class="field-label">Emoji</label>
              <EmojiPicker value={emoji} onChange={setEmoji} />
            </div>
            <div class="automation-field-group">
              <label class="field-label">Name</label>
              <input class="field-input" value={name} onInput={(e) => setName((e.target as HTMLInputElement).value)} placeholder="Daily brief" />
            </div>
          </div>

          {/* Schedule */}
          <div class="automation-field-group">
            <label class="field-label">Schedule</label>
            <span class="workflow-field-hint">daily 08:00 · weekly monday 09:00 · every 30 min</span>
            <input class="field-input" value={schedule} onInput={(e) => setSchedule((e.target as HTMLInputElement).value)} placeholder="daily 08:00" />
          </div>

          {/* Prompt */}
          <div class="automation-field-group">
            <label class="field-label">Prompt</label>
            <span class="workflow-field-hint">What Atlas should do each run. Workflow runs first if a workflow is bound.</span>
            <textarea
              class="field-input workflow-textarea"
              value={prompt}
              onInput={(e) => setPrompt((e.target as HTMLTextAreaElement).value)}
              placeholder="What should Atlas do when this automation runs?"
            />
          </div>

          {/* Saved Workflow + Delivery Destination — flat row-sharing grid */}
          <div class="workflow-aligned-grid">
            <label class="field-label">Saved Workflow</label>
            <label class="field-label">Delivery Destination <span class="automation-optional-label">(optional)</span></label>
            <span class="workflow-field-hint">Bind a reusable workflow to run first</span>
            <span class="workflow-field-hint">
              {knownChannels.length > 0 ? 'Channel that receives results after each run' : 'No channels found — configure Telegram or Discord first'}
            </span>
            <select class="field-input" value={workflowID} onChange={(e) => setWorkflowID((e.target as HTMLSelectElement).value)}>
              <option value="">— Prompt only —</option>
              {workflows.map(workflow => (
                <option key={workflow.id} value={workflow.id}>{workflow.name}</option>
              ))}
            </select>
            {knownChannels.length > 0 ? (
              <select class="field-input" value={destinationID} onChange={(e) => setDestinationID((e.target as HTMLSelectElement).value)}>
                <option value="">— None —</option>
                {knownChannels.map(channel => (
                  <option key={channel.id} value={channel.id}>
                    {channel.platform} · {channel.channelName ?? channel.channelID}
                  </option>
                ))}
              </select>
            ) : (
              <input class="field-input" value="" disabled placeholder="No channels discovered yet" />
            )}
          </div>

          {/* Workflow Inputs JSON */}
          <div class="automation-field-group">
            <label class="field-label">Workflow Inputs JSON <span class="automation-optional-label">(optional)</span></label>
            <span class="workflow-field-hint">Variable values passed into the workflow at runtime</span>
            <textarea
              class="field-input workflow-textarea"
              value={workflowInputValues}
              onInput={(e) => setWorkflowInputValues((e.target as HTMLTextAreaElement).value)}
              placeholder='{"path":"README.md"}'
            />
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-ghost btn-sm" onClick={onClose} disabled={saving}>Cancel</button>
          <button class="btn btn-primary btn-sm" onClick={handleSave} disabled={saving}>
            {saving ? 'Saving…' : (gremlin ? 'Save Changes' : 'Create')}
          </button>
        </div>
      </div>
    </div>
  )
}

// ── Main screen ──────────────────────────────────────────────────────────────

export function Automations() {
  const [items, setItems]           = useState<GremlinItem[]>([])
  const [loading, setLoading]       = useState(true)
  const [error, setError]           = useState<string | null>(null)
  const [editTarget, setEditTarget] = useState<GremlinItem | 'new' | null>(null)
  const [runsTarget, setRunsTarget] = useState<GremlinItem | null>(null)
  const [runningID, setRunningID]   = useState<string | null>(null)
  const [togglingID, setTogglingID] = useState<string | null>(null)

  async function load() {
    setLoading(true)
    setError(null)
    try {
      const data = await api.automations()
      setItems(data)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to load automations.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  async function handleToggle(item: GremlinItem) {
    setTogglingID(item.id)
    try {
      const updated = item.isEnabled
        ? await api.disableAutomation(item.id)
        : await api.enableAutomation(item.id)
      setItems(prev => prev.map(i => i.id === item.id ? updated : i))
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Toggle failed.')
    } finally {
      setTogglingID(null)
    }
  }

  async function handleRunNow(item: GremlinItem) {
    setRunningID(item.id)
    setError(null)
    try {
      await api.runAutomationNow(item.id)
      await load()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Run failed.')
    } finally {
      setRunningID(null)
    }
  }

  async function handleDelete(item: GremlinItem) {
    if (!confirm(`Delete "${item.name}"? This cannot be undone.`)) return
    try {
      await api.deleteAutomation(item.id)
      setItems(prev => prev.filter(i => i.id !== item.id))
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Delete failed.')
    }
  }

  async function handleSave(item: GremlinItem) {
    if (editTarget === 'new') {
      const created = await api.createAutomation(item)
      setItems(prev => [...prev, created])
    } else {
      const updated = await api.updateAutomation(item)
      setItems(prev => prev.map(i => i.id === item.id ? updated : i))
    }
  }

  return (
    <div class="screen">
      <PageHeader
        title="Automations"
        subtitle="Scheduled prompts Atlas runs automatically."
        actions={<>
          <button class="btn btn-primary btn-sm" onClick={() => setEditTarget('new')}>+ New</button>
          <button class="btn btn-primary btn-sm" onClick={load}><RefreshIcon /> Refresh</button>
        </>}
      />

      {error && <p class="error-banner">{error}</p>}

      {loading && <p class="empty-state">Loading automations…</p>}

      {!loading && items.length === 0 && (
        <div class="empty-state">
          <svg class="empty-icon" viewBox="0 0 36 36" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="18" cy="18" r="13" />
            <path d="M18 11v7l4 4" />
          </svg>
          <h3>No automations yet</h3>
          <p>Click <strong>+ New</strong> to create a schedule and Atlas will run it automatically.</p>
        </div>
      )}

      {!loading && items.length > 0 && (
        <div class="automation-list">
          {items.map(item => (
            <div key={item.id} class={`card automation-card${item.isEnabled ? '' : ' disabled'}`}>
              <div class="automation-card-header">
                <span class="automation-emoji">{item.emoji}</span>
                <div class="automation-meta">
                  <div style={{ display: 'flex', alignItems: 'center', gap: 6 }}>
                    <span class="automation-name">{item.name}</span>
                    {item.communicationDestination != null && (
                      <span title={`Notifies ${item.communicationDestination.platform} channel ${item.communicationDestination.channelID}`} style={{ fontSize: 12, opacity: 0.7 }}>📨</span>
                    )}
                  </div>
                  <span class="automation-schedule">{item.scheduleRaw}</span>
                </div>
                <div class="automation-actions">
                  <button
                    class={`btn btn-sm automation-action-btn automation-toggle-btn${item.isEnabled ? ' enabled' : ''}`}
                    onClick={() => handleToggle(item)}
                    disabled={togglingID === item.id}
                    title={item.isEnabled ? 'Disable' : 'Enable'}
                  >
                    {togglingID === item.id ? '…' : (item.isEnabled ? 'On' : 'Off')}
                  </button>
                  <button
                    class="btn btn-sm btn-icon automation-action-btn automation-action-icon"
                    onClick={() => handleRunNow(item)}
                    disabled={runningID === item.id}
                    title="Run now"
                  >
                    {runningID === item.id ? '…' : <PlayIcon />}
                  </button>
                  <button
                    class="btn btn-sm automation-action-btn"
                    onClick={() => setRunsTarget(item)}
                    title="View run history"
                  >
                    Runs
                  </button>
                  <button
                    class="btn btn-sm automation-action-btn"
                    onClick={() => setEditTarget(item)}
                    title="Edit"
                  >
                    Edit
                  </button>
                  <button
                    class="btn btn-sm btn-icon automation-action-btn automation-action-icon automation-action-danger"
                    onClick={() => handleDelete(item)}
                    title="Delete"
                  >
                    <TrashIcon />
                  </button>
                </div>
              </div>
              <p class="automation-prompt">{item.prompt}</p>
              {item.workflowID && (
                <p class="automation-last-run">Workflow: <strong>{item.workflowID}</strong></p>
              )}
              {item.lastRunAt && (
                <p class="automation-last-run">
                  Last run: {formatDate(item.lastRunAt)}
                  {item.lastRunStatus && <> — {statusBadge(item.lastRunStatus)}</>}
                </p>
              )}
            </div>
          ))}
        </div>
      )}

      {editTarget !== null && (
        <EditModal
          gremlin={editTarget === 'new' ? undefined : editTarget}
          onSave={handleSave}
          onClose={() => setEditTarget(null)}
        />
      )}

      {runsTarget && (
        <RunsPanel
          gremlin={runsTarget}
          onClose={() => setRunsTarget(null)}
        />
      )}
    </div>
  )
}
