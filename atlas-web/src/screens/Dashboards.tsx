import { useState, useEffect, useCallback } from 'preact/hooks'
import { api, DashboardProposal, DashboardSpec } from '../api/client'
import { PageHeader } from '../components/PageHeader'
import { ErrorBanner } from '../components/ErrorBanner'

/* ── Helpers ─────────────────────────────────────────────── */

function relativeTime(iso: string): string {
  const diff = Date.now() - new Date(iso).getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return 'just now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

/* ── Icons ──────────────────────────────────────────────── */

const SpinnerIcon = () => (
  <span class="spinner" style={{ width: '11px', height: '11px' }} />
)

const RefreshIcon = () => (
  <svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <path d="M2.5 8a5.5 5.5 0 0 1 9.5-3.8" />
    <polyline points="13.5,2.5 13.5,6 10,6" />
    <path d="M13.5 8a5.5 5.5 0 0 1-9.5 3.8" />
    <polyline points="2.5,13.5 2.5,10 6,10" />
  </svg>
)

const GridIcon = () => (
  <svg width="28" height="28" viewBox="0 0 28 28" fill="none" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" stroke-linejoin="round">
    <rect x="3" y="3" width="9" height="9" rx="1.5" />
    <rect x="16" y="3" width="9" height="9" rx="1.5" />
    <rect x="3" y="16" width="9" height="9" rx="1.5" />
    <rect x="16" y="16" width="9" height="9" rx="1.5" />
  </svg>
)

const EmptyGridIcon = () => (
  <svg class="empty-icon" viewBox="0 0 36 36" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
    <rect x="3" y="3" width="12" height="12" rx="2" />
    <rect x="21" y="3" width="12" height="12" rx="2" />
    <rect x="3" y="21" width="12" height="12" rx="2" />
    <rect x="21" y="21" width="12" height="12" rx="2" />
  </svg>
)

/* ── Section Header ──────────────────────────────────────── */

function SectionHeader({ label, sub, count }: { label: string; sub: string; count?: number }) {
  return (
    <div class="skill-group-header" style={{ marginBottom: '10px' }}>
      <span>
        {label}
        {count !== undefined && count > 0 && (
          <span style={{
            marginLeft: '8px',
            background: 'var(--bg-2, rgba(255,255,255,0.08))',
            border: '1px solid var(--border)',
            borderRadius: '10px',
            padding: '1px 7px',
            fontSize: '11px',
            fontWeight: 600,
            color: 'var(--text-2)'
          }}>{count}</span>
        )}
      </span>
      {sub && <p class="skill-group-sub">{sub}</p>}
    </div>
  )
}

/* ── Empty State ─────────────────────────────────────────── */

function EmptyState({ message }: { message: string }) {
  return (
    <div style={{ padding: '20px 24px', color: 'var(--text-2)', fontSize: '13px', fontStyle: 'italic' }}>
      {message}
    </div>
  )
}

/* ── Proposal Card ───────────────────────────────────────── */

interface ProposalCardProps {
  proposal: DashboardProposal
  onInstall: (proposalID: string) => Promise<void>
  onReject: (proposalID: string) => Promise<void>
  acting: boolean
}

function DashboardProposalCard({ proposal, onInstall, onReject, acting }: ProposalCardProps) {
  return (
    <div class="card" style={{ marginBottom: '12px', overflow: 'hidden' }}>
      <div style={{ padding: '16px 20px 0' }}>
        {/* Header row */}
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '12px' }}>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap', marginBottom: '4px' }}>
              <span style={{ fontSize: '15px', marginRight: '2px' }}>{proposal.spec.icon}</span>
              <span style={{ fontWeight: 600, fontSize: '14px', color: 'var(--text)' }}>
                {proposal.spec.title}
              </span>
              <span class="badge" style={{
                background: 'rgba(99, 102, 241, 0.15)',
                color: '#a5b4fc',
                border: '1px solid rgba(99, 102, 241, 0.25)',
                borderRadius: '4px',
                padding: '1px 6px',
                fontSize: '10px',
                fontWeight: 600,
              }}>Dashboard</span>
            </div>
            <div style={{ fontSize: '12px', color: 'var(--text-2)', marginBottom: '10px' }}>
              {proposal.spec.description}
            </div>
          </div>
          <div style={{ fontSize: '11px', color: 'var(--text-2)', whiteSpace: 'nowrap', flexShrink: 0 }}>
            {relativeTime(proposal.createdAt)}
          </div>
        </div>

        {/* Summary */}
        <div style={{
          fontSize: '13px',
          color: 'var(--text)',
          background: 'rgba(255,255,255,0.04)',
          border: '1px solid var(--border)',
          borderRadius: '6px',
          padding: '10px 12px',
          marginBottom: '12px',
          lineHeight: '1.5'
        }}>
          {proposal.summary}
          {proposal.rationale && (
            <div style={{ marginTop: '6px', fontSize: '12px', color: 'var(--text-2)', fontStyle: 'italic' }}>
              {proposal.rationale}
            </div>
          )}
        </div>

        {/* Metadata pills */}
        <div style={{ display: 'flex', gap: '16px', flexWrap: 'wrap', marginBottom: '14px' }}>
          <div style={{ fontSize: '12px', color: 'var(--text-2)' }}>
            <span style={{ color: 'var(--text-3, var(--text-2))' }}>▸</span>{' '}
            {proposal.spec.widgets.length} widget{proposal.spec.widgets.length !== 1 ? 's' : ''}
          </div>
          {proposal.spec.sourceSkillIDs.length > 0 && (
            <div style={{ fontSize: '12px', color: 'var(--text-2)' }}>
              <span>▸</span>{' '}
              Skills: {proposal.spec.sourceSkillIDs.join(', ')}
            </div>
          )}
        </div>

        {/* Action row */}
        <div style={{ display: 'flex', gap: '8px', flexWrap: 'wrap', paddingBottom: '16px' }}>
          <button
            class="btn btn-primary btn-sm"
            disabled={acting}
            onClick={() => onInstall(proposal.proposalID)}
          >
            {acting ? <SpinnerIcon /> : null}
            Install Dashboard
          </button>
          <button
            class="btn btn-ghost btn-sm"
            disabled={acting}
            onClick={() => onReject(proposal.proposalID)}
            style={{ color: 'var(--c-red, #f87171)' }}
          >
            Reject
          </button>
        </div>
      </div>
    </div>
  )
}

/* ── Installed Dashboard Card ────────────────────────────── */

interface InstalledCardProps {
  spec: DashboardSpec
  onOpen: (id: string) => void
  onRemove: (id: string) => Promise<void>
  acting: boolean
}

function InstalledDashboardCard({ spec, onOpen, onRemove, acting }: InstalledCardProps) {
  const [confirmRemove, setConfirmRemove] = useState(false)

  return (
    <div class="card" style={{ marginBottom: '12px', overflow: 'hidden' }}>
      <div style={{ padding: '16px 20px' }}>
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '12px' }}>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap', marginBottom: '6px' }}>
              <span style={{ fontSize: '18px' }}>{spec.icon}</span>
              <span style={{ fontWeight: 600, fontSize: '14px', color: 'var(--text)' }}>{spec.title}</span>
            </div>
            <div style={{ fontSize: '12px', color: 'var(--text-2)', marginBottom: '10px' }}>
              {spec.description}
            </div>
            {/* Skill chips */}
            <div style={{ display: 'flex', gap: '6px', flexWrap: 'wrap' }}>
              {spec.sourceSkillIDs.map(skillID => (
                <span key={skillID} style={{
                  fontSize: '11px',
                  padding: '2px 7px',
                  background: 'rgba(255,255,255,0.06)',
                  border: '1px solid var(--border)',
                  borderRadius: '10px',
                  color: 'var(--text-2)',
                  fontWeight: 500,
                }}>
                  {skillID}
                </span>
              ))}
            </div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '6px', flexShrink: 0, alignItems: 'flex-end' }}>
            <button class="btn btn-primary btn-sm" onClick={() => onOpen(spec.id)}>
              Open
            </button>
            <button
              class="btn btn-ghost btn-sm"
              style={{ fontSize: '11px', color: 'var(--c-red, #f87171)' }}
              onClick={() => setConfirmRemove(true)}
            >
              Remove
            </button>
          </div>
        </div>

        {/* Widget count footer */}
        <div style={{ marginTop: '10px', fontSize: '11px', color: 'var(--text-3, var(--text-2))' }}>
          {spec.widgets.length} widget{spec.widgets.length !== 1 ? 's' : ''}
        </div>
      </div>

      {confirmRemove && (
        <div style={{
          padding: '10px 20px 14px',
          background: 'rgba(248, 113, 113, 0.06)',
          borderTop: '1px solid rgba(248, 113, 113, 0.2)',
          display: 'flex',
          alignItems: 'center',
          gap: '10px',
          flexWrap: 'wrap',
        }}>
          <span style={{ fontSize: '12px', color: 'var(--text)', flex: 1 }}>
            Remove <strong>{spec.title}</strong>? This cannot be undone without a new proposal.
          </span>
          <button
            class="btn btn-sm"
            style={{ background: 'rgba(248, 113, 113, 0.15)', color: '#f87171', border: '1px solid rgba(248, 113, 113, 0.3)' }}
            disabled={acting}
            onClick={() => { setConfirmRemove(false); onRemove(spec.id) }}
          >
            {acting ? <SpinnerIcon /> : null}
            Confirm Remove
          </button>
          <button class="btn btn-ghost btn-sm" onClick={() => setConfirmRemove(false)}>
            Cancel
          </button>
        </div>
      )}
    </div>
  )
}

/* ── Create Proposal Modal ───────────────────────────────── */

function CreateProposalModal({ onCreated, onClose }: { onCreated: () => void; onClose: () => void }) {
  const [intent, setIntent]     = useState('')
  const [skillIDs, setSkillIDs] = useState('')
  const [creating, setCreating] = useState(false)
  const [error, setError]       = useState<string | null>(null)

  const handleCreate = async () => {
    const trimmed = intent.trim()
    if (!trimmed) return
    setCreating(true)
    setError(null)
    try {
      const ids = skillIDs.split(',').map(s => s.trim()).filter(Boolean)
      await api.createDashboardProposal(trimmed, ids)
      onCreated()
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to create proposal.')
    } finally {
      setCreating(false)
    }
  }

  return (
    <div class="modal-overlay" onClick={(e) => { if ((e.target as HTMLElement).classList.contains('modal-overlay')) onClose() }}>
      <div class="modal automation-modal" style={{ maxWidth: 560, width: '92vw' }}>
        <div class="modal-header">
          <div class="automation-modal-title-wrap">
            <div class="surface-eyebrow">Dashboard</div>
            <h3 class="automation-modal-title">Create dashboard</h3>
          </div>
          <button class="btn btn-ghost btn-sm" onClick={onClose}>✕</button>
        </div>
        <div class="modal-body automation-modal-body" style={{ maxHeight: 'calc(85vh - 130px)', overflowY: 'auto' }}>
          {error && <p class="error-banner">{error}</p>}

          <div class="automation-field-group">
            <label class="field-label">Intent</label>
            <span class="workflow-field-hint">Describe the dashboard you want Atlas to design</span>
            <textarea
              class="field-input workflow-textarea"
              placeholder="A weather dashboard showing current conditions and a 7-day forecast"
              value={intent}
              onInput={e => setIntent((e.target as HTMLTextAreaElement).value)}
            />
          </div>

          <div class="automation-field-group">
            <label class="field-label">Skill IDs <span class="automation-optional-label">(optional)</span></label>
            <span class="workflow-field-hint">Comma-separated skill IDs to scope the dashboard</span>
            <input
              class="field-input"
              type="text"
              placeholder="weather, web, atlas.info"
              value={skillIDs}
              onInput={e => setSkillIDs((e.target as HTMLInputElement).value)}
            />
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-ghost btn-sm" onClick={onClose} disabled={creating}>Cancel</button>
          <button class="btn btn-primary btn-sm" disabled={creating || !intent.trim()} onClick={handleCreate}>
            {creating ? <><SpinnerIcon /> Planning…</> : 'Generate Proposal'}
          </button>
        </div>
      </div>
    </div>
  )
}

/* ── Main Dashboards Screen ──────────────────────────────── */

interface DashboardsProps {
  onOpenDashboard: (id: string) => void
}

export function Dashboards({ onOpenDashboard }: DashboardsProps) {
  const [proposals, setProposals]   = useState<DashboardProposal[]>([])
  const [installed, setInstalled]   = useState<DashboardSpec[]>([])
  const [loading, setLoading]       = useState(true)
  const [error, setError]           = useState<string | null>(null)
  const [acting, setActing]         = useState<Set<string>>(new Set())
  const [showCreate, setShowCreate] = useState(false)

  const load = useCallback(async () => {
    try {
      const [p, i] = await Promise.allSettled([
        api.dashboardProposals(),
        api.installedDashboards(),
      ])
      if (p.status === 'fulfilled') setProposals(p.value)
      if (i.status === 'fulfilled') setInstalled(i.value)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load dashboard data.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
    const interval = setInterval(load, 5000)
    return () => clearInterval(interval)
  }, [load])

  const handleInstall = async (proposalID: string) => {
    setActing(prev => new Set(prev).add(proposalID))
    try {
      await api.installDashboard(proposalID)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Install failed.')
    } finally {
      setActing(prev => { const s = new Set(prev); s.delete(proposalID); return s })
    }
  }

  const handleReject = async (proposalID: string) => {
    setActing(prev => new Set(prev).add(proposalID))
    try {
      await api.rejectDashboard(proposalID)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Reject failed.')
    } finally {
      setActing(prev => { const s = new Set(prev); s.delete(proposalID); return s })
    }
  }

  const handleRemove = async (dashboardID: string) => {
    setActing(prev => new Set(prev).add(dashboardID))
    try {
      await api.removeDashboard(dashboardID)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Remove failed.')
    } finally {
      setActing(prev => { const s = new Set(prev); s.delete(dashboardID); return s })
    }
  }

  if (loading) {
    return (
      <div class="screen">
        <PageHeader title="Dashboard Builder" subtitle="Schema-driven data views" />
        <div style={{ display: 'flex', justifyContent: 'center', padding: '48px' }}>
          <span class="spinner" />
        </div>
      </div>
    )
  }

  const pending = proposals.filter(p => p.status === 'pending')

  return (
    <div class="screen">
      <PageHeader
        title="Dashboard Builder"
        subtitle="Propose, review, and install dashboards"
        actions={
          <>
            <button class="btn btn-primary btn-sm" onClick={() => setShowCreate(true)}>+ New</button>
            <button class="btn btn-primary btn-sm" onClick={load}><RefreshIcon /> Refresh</button>
          </>
        }
      />

      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      {/* ── Pending Proposals ── */}
      {pending.length > 0 && (
        <div style={{ marginBottom: '24px' }}>
          <SectionHeader
            label="Pending Proposals"
            sub="Dashboards awaiting your decision"
            count={pending.length}
          />
          {pending.map(proposal => (
            <DashboardProposalCard
              key={proposal.proposalID}
              proposal={proposal}
              onInstall={handleInstall}
              onReject={handleReject}
              acting={acting.has(proposal.proposalID)}
            />
          ))}
        </div>
      )}

      {/* ── Empty state ── */}
      {installed.length === 0 && pending.length === 0 && (
        <div class="empty-state">
          <svg class="empty-icon" viewBox="0 0 36 36" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
            <rect x="5" y="5" width="10" height="10" rx="1.5" />
            <rect x="21" y="5" width="10" height="10" rx="1.5" />
            <rect x="5" y="21" width="10" height="10" rx="1.5" />
            <rect x="21" y="21" width="10" height="10" rx="1.5" />
          </svg>
          <h3>No dashboards installed yet</h3>
          <p>Click <strong>+ New</strong> and Atlas will design a dashboard from your intent and installed skills.</p>
        </div>
      )}

      {/* ── Installed Dashboards ── */}
      {installed.length > 0 && (
        <div style={{ marginBottom: '24px' }}>
          <SectionHeader
            label="Installed"
            sub="Your active dashboards"
            count={installed.length}
          />
          {installed.map(spec => (
            <InstalledDashboardCard
              key={spec.id}
              spec={spec}
              onOpen={onOpenDashboard}
              onRemove={handleRemove}
              acting={acting.has(spec.id)}
            />
          ))}
        </div>
      )}

      {showCreate && (
        <CreateProposalModal
          onCreated={load}
          onClose={() => setShowCreate(false)}
        />
      )}
    </div>
  )
}
