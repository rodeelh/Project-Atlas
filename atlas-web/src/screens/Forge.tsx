import { useState, useEffect, useCallback } from 'preact/hooks'
import { api, ForgeProposalRecord, ForgeResearchingItem, SkillRecord } from '../api/client'
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

function riskBadge(level: string) {
  switch (level.toLowerCase()) {
    case 'low':    return <span class="badge badge-green">{level}</span>
    case 'medium': return <span class="badge badge-yellow">{level}</span>
    case 'high':   return <span class="badge badge-red">{level}</span>
    default:       return <span class="badge badge-gray">{level}</span>
  }
}

function statusBadge(status: string) {
  switch (status) {
    case 'pending':   return <span class="badge badge-yellow">Pending</span>
    case 'installed': return <span class="badge badge-gray">Installed</span>
    case 'enabled':   return <span class="badge badge-green">Enabled</span>
    case 'rejected':  return <span class="badge badge-red">Rejected</span>
    default:          return <span class="badge badge-gray">{status}</span>
  }
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

const ChevronDown = () => (
  <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round">
    <polyline points="2,4 6,8 10,4" />
  </svg>
)

const ChevronUp = () => (
  <svg width="12" height="12" viewBox="0 0 12 12" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round">
    <polyline points="2,8 6,4 10,8" />
  </svg>
)

const PulseIcon = () => (
  <svg width="8" height="8" viewBox="0 0 8 8" fill="currentColor">
    <circle cx="4" cy="4" r="4" />
  </svg>
)

/* ── Technical Details Panel ─────────────────────────────── */

function TechnicalDetails({ proposal }: { proposal: ForgeProposalRecord }) {
  let spec: unknown = null
  let plans: unknown = null
  let contract: unknown = null
  try { spec = JSON.parse(proposal.specJSON) } catch { /* keep null */ }
  try { plans = JSON.parse(proposal.plansJSON) } catch { /* keep null */ }
  if (proposal.contractJSON) {
    try { contract = JSON.parse(proposal.contractJSON) } catch { /* keep null */ }
  }

  const fmtJSON = (v: unknown) => JSON.stringify(v, null, 2)

  const preStyle = {
    background: 'var(--bg-2, rgba(255,255,255,0.04))',
    border: '1px solid var(--border)',
    borderRadius: '6px',
    padding: '10px 12px',
    fontSize: '11px',
    overflowX: 'auto' as const,
    margin: 0,
    color: 'var(--text)',
    whiteSpace: 'pre-wrap' as const,
    wordBreak: 'break-word' as const
  }

  return (
    <div style={{ padding: '12px 20px 16px', borderTop: '1px solid var(--border)' }}>
      <div style={{ fontSize: '11px', fontWeight: 600, color: 'var(--text-2)', marginBottom: '10px', textTransform: 'uppercase', letterSpacing: '0.04em' }}>
        Technical Details
      </div>

      {contract && (
        <div style={{ marginBottom: '12px' }}>
          <div style={{ fontSize: '11px', color: 'var(--text-2)', marginBottom: '4px' }}>Research Contract</div>
          <pre style={preStyle}>{fmtJSON(contract)}</pre>
        </div>
      )}

      <div style={{ marginBottom: '12px' }}>
        <div style={{ fontSize: '11px', color: 'var(--text-2)', marginBottom: '4px' }}>Skill Spec</div>
        <pre style={preStyle}>{fmtJSON(spec)}</pre>
      </div>

      <div>
        <div style={{ fontSize: '11px', color: 'var(--text-2)', marginBottom: '4px' }}>Execution Plans</div>
        <pre style={preStyle}>{fmtJSON(plans)}</pre>
      </div>
    </div>
  )
}

/* ── Proposal Card ───────────────────────────────────────── */

interface ProposalCardProps {
  proposal: ForgeProposalRecord
  onInstall: (id: string, enable: boolean) => Promise<void>
  onReject: (id: string) => Promise<void>
  acting: boolean
}

function ProposalCard({ proposal, onInstall, onReject, acting }: ProposalCardProps) {
  const [showDetails, setShowDetails] = useState(false)

  return (
    <div class="card forge-proposal-card">
      {/* Header row */}
      <div class="forge-proposal-body">
        <div style={{ display: 'flex', alignItems: 'flex-start', justifyContent: 'space-between', gap: '12px' }}>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap', marginBottom: '4px' }}>
              <span style={{ fontWeight: 600, fontSize: '14px', color: 'var(--text)' }}>
                {proposal.name}
              </span>
              {riskBadge(proposal.riskLevel)}
              <span class="badge" style={{
                background: 'rgba(139, 92, 246, 0.15)',
                color: '#a78bfa',
                border: '1px solid rgba(139, 92, 246, 0.25)',
                borderRadius: '4px',
                padding: '1px 6px',
                fontSize: '10px',
                fontWeight: 600,
              }}>Forge</span>
            </div>
            <div style={{ fontSize: '12px', color: 'var(--text-2)', marginBottom: '10px' }}>
              {proposal.description}
            </div>
          </div>
          <div style={{ fontSize: '11px', color: 'var(--text-2)', whiteSpace: 'nowrap', flexShrink: 0 }}>
            {relativeTime(proposal.createdAt)}
          </div>
        </div>

        {/* Summary */}
        <div class="forge-proposal-summary" style={{
          fontSize: '13px',
          color: 'var(--text)',
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
          {proposal.actionNames.length > 0 && (
            <div style={{ fontSize: '12px', color: 'var(--text-2)' }}>
              <span style={{ color: 'var(--text-3, var(--text-2))' }}>▸</span>{' '}
              {proposal.actionNames.length === 1
                ? `Action: ${proposal.actionNames[0]}`
                : `${proposal.actionNames.length} actions`}
            </div>
          )}
          {proposal.domains.length > 0 && (
            <div style={{ fontSize: '12px', color: 'var(--text-2)' }}>
              <span>▸</span>{' '}
              Calls: {proposal.domains.join(', ')}
            </div>
          )}
          {proposal.requiredSecrets.length > 0 ? (
            <div style={{ fontSize: '12px', color: 'var(--c-yellow, #facc15)' }}>
              <span>▸</span>{' '}
              Requires: {proposal.requiredSecrets.join(', ')}
            </div>
          ) : (
            <div style={{ fontSize: '12px', color: 'var(--text-2)' }}>
              <span>▸</span>{' '}
              No secrets required
            </div>
          )}
        </div>

        {/* Action row */}
        <div style={{
          display: 'flex',
          gap: '8px',
          flexWrap: 'wrap',
          paddingBottom: '16px',
          borderBottom: showDetails ? '1px solid var(--border)' : 'none'
        }}>
          <button
            class="btn btn-primary btn-sm"
            disabled={acting}
            onClick={() => onInstall(proposal.id, true)}
          >
            {acting ? <SpinnerIcon /> : null}
            Install & Enable
          </button>
          <button
            class="btn btn-sm"
            disabled={acting}
            onClick={() => onInstall(proposal.id, false)}
          >
            Install Only
          </button>
          <button
            class="btn btn-ghost btn-sm"
            disabled={acting}
            onClick={() => onReject(proposal.id)}
            style={{ color: 'var(--c-red, #f87171)' }}
          >
            Reject
          </button>
          <button
            class="btn btn-ghost btn-sm"
            style={{ marginLeft: 'auto' }}
            onClick={() => setShowDetails(v => !v)}
          >
            {showDetails ? <ChevronUp /> : <ChevronDown />}
            Details
          </button>
        </div>
      </div>

      {/* Technical details (expandable) */}
      {showDetails && <TechnicalDetails proposal={proposal} />}
    </div>
  )
}

/* ── Researching Row ─────────────────────────────────────── */

function ResearchingRow({ item }: { item: ForgeResearchingItem }) {
  return (
    <div class="row" style={{ gap: '12px', alignItems: 'center' }}>
      <span style={{ color: 'var(--c-blue, #60a5fa)', animation: 'pulse 1.8s ease-in-out infinite', flexShrink: 0 }}>
        <PulseIcon />
      </span>
      <div style={{ flex: 1, minWidth: 0 }}>
        <span style={{ fontSize: '13px', fontWeight: 500, color: 'var(--text)' }}>{item.title}</span>
        <span style={{ fontSize: '12px', color: 'var(--text-2)', marginLeft: '8px' }}>{item.message}</span>
      </div>
      <span style={{ fontSize: '11px', color: 'var(--text-2)', flexShrink: 0 }}>
        {relativeTime(item.startedAt)}
      </span>
      <span class="badge badge-gray" style={{ fontSize: '10px' }}>Researching</span>
    </div>
  )
}

/* ── Installed Row ───────────────────────────────────────── */

interface InstalledRowProps {
  skill: SkillRecord
  isLast: boolean
  onEnable: (skillID: string) => Promise<void>
  onDisable: (skillID: string) => Promise<void>
  onUninstall: (skillID: string) => Promise<void>
  acting: boolean
}

function InstalledRow({ skill, isLast, onEnable, onDisable, onUninstall, acting }: InstalledRowProps) {
  const [confirmUninstall, setConfirmUninstall] = useState(false)
  const state = skill.manifest.lifecycleState
  const isEnabled = state === 'enabled'
  const stateColor = isEnabled ? 'var(--c-green, #4ade80)'
    : state === 'disabled' ? 'var(--c-red, #f87171)'
    : 'var(--text-2)'

  return (
    <div style={{ borderBottom: isLast ? 'none' : '1px solid var(--border)' }}>
      <div class="row" style={{ gap: '10px', alignItems: 'center' }}>
        <div style={{ flex: 1, minWidth: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
            <span style={{ fontSize: '13px', fontWeight: 500, color: 'var(--text)' }}>
              {skill.manifest.name}
            </span>
            <span class="badge" style={{
              background: 'rgba(139, 92, 246, 0.15)',
              color: '#a78bfa',
              border: '1px solid rgba(139, 92, 246, 0.25)',
              borderRadius: '4px',
              padding: '1px 6px',
              fontSize: '10px',
              fontWeight: 600,
            }}>Forge</span>
          </div>
          <div style={{ fontSize: '12px', color: 'var(--text-2)', marginTop: '2px' }}>
            {skill.actions.length} action{skill.actions.length !== 1 ? 's' : ''}
            {skill.manifest.category ? ` · ${skill.manifest.category}` : ''}
            {skill.manifest.description ? ` · ${skill.manifest.description}` : ''}
          </div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexShrink: 0 }}>
          {riskBadge(skill.manifest.riskLevel)}
          <span style={{ fontSize: '12px', color: stateColor, fontWeight: 500 }}>{state}</span>
          <button
            class="btn btn-sm"
            disabled={acting}
            onClick={() => isEnabled ? onDisable(skill.manifest.id) : onEnable(skill.manifest.id)}
          >
            {acting ? <SpinnerIcon /> : null}
            {isEnabled ? 'Disable' : 'Enable'}
          </button>
          <button
            class="btn btn-ghost btn-sm"
            disabled={acting}
            onClick={() => setConfirmUninstall(true)}
            style={{ color: 'var(--c-red, #f87171)' }}
          >
            Uninstall
          </button>
        </div>
      </div>
      {confirmUninstall && (
        <div class="forge-danger-panel" style={{
          borderTopLeftRadius: 0,
          borderTopRightRadius: 0,
          display: 'flex',
          alignItems: 'center',
          gap: '10px',
          flexWrap: 'wrap',
        }}>
          <span style={{ fontSize: '12px', color: 'var(--text)', flex: 1 }}>
            Remove <strong>{skill.manifest.name}</strong> from Atlas? This cannot be undone without a new Forge proposal.
          </span>
          <button
            class="btn btn-sm"
            style={{ background: 'rgba(248, 113, 113, 0.15)', color: '#f87171', border: '1px solid rgba(248, 113, 113, 0.3)' }}
            disabled={acting}
            onClick={() => { setConfirmUninstall(false); onUninstall(skill.manifest.id) }}
          >
            {acting ? <SpinnerIcon /> : null}
            Confirm Uninstall
          </button>
          <button class="btn btn-ghost btn-sm" onClick={() => setConfirmUninstall(false)}>
            Cancel
          </button>
        </div>
      )}
    </div>
  )
}

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

/* ── Main Forge Screen ───────────────────────────────────── */

export function Forge() {
  const [researching, setResearching] = useState<ForgeResearchingItem[]>([])
  const [proposals,   setProposals]   = useState<ForgeProposalRecord[]>([])
  const [installed,   setInstalled]   = useState<SkillRecord[]>([])
  const [loading,     setLoading]     = useState(true)
  const [error,       setError]       = useState<string | null>(null)
  const [acting,      setActing]      = useState<Set<string>>(new Set())

  const load = useCallback(async () => {
    try {
      const [r, p, i] = await Promise.allSettled([
        api.forgeResearching(),
        api.forgeProposals(),
        api.forgeInstalled(),
      ])
      if (r.status === 'fulfilled') setResearching(r.value)
      if (p.status === 'fulfilled') setProposals(p.value)
      if (i.status === 'fulfilled') setInstalled(i.value)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load Forge data.')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
    const interval = setInterval(load, 5000)
    return () => clearInterval(interval)
  }, [load])

  const handleInstall = async (id: string, enable: boolean) => {
    setActing(prev => new Set(prev).add(id))
    try {
      enable ? await api.forgeInstallEnable(id) : await api.forgeInstall(id)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Install failed.')
    } finally {
      setActing(prev => { const s = new Set(prev); s.delete(id); return s })
    }
  }

  const handleReject = async (id: string) => {
    setActing(prev => new Set(prev).add(id))
    try {
      await api.forgeReject(id)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Reject failed.')
    } finally {
      setActing(prev => { const s = new Set(prev); s.delete(id); return s })
    }
  }

  const handleEnable = async (skillID: string) => {
    setActing(prev => new Set(prev).add(skillID))
    try {
      await api.enableSkill(skillID)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Enable failed.')
    } finally {
      setActing(prev => { const s = new Set(prev); s.delete(skillID); return s })
    }
  }

  const handleDisable = async (skillID: string) => {
    setActing(prev => new Set(prev).add(skillID))
    try {
      await api.disableSkill(skillID)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Disable failed.')
    } finally {
      setActing(prev => { const s = new Set(prev); s.delete(skillID); return s })
    }
  }

  const handleUninstall = async (skillID: string) => {
    setActing(prev => new Set(prev).add(skillID))
    try {
      await api.forgeUninstall(skillID)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Uninstall failed.')
    } finally {
      setActing(prev => { const s = new Set(prev); s.delete(skillID); return s })
    }
  }

  if (loading) {
    return (
      <div class="screen">
        <PageHeader title="Forge" subtitle="Propose, review, and install AI-generated skills" />
        <div style={{ display: 'flex', justifyContent: 'center', padding: '48px' }}>
          <span class="spinner" />
        </div>
      </div>
    )
  }

  const pendingProposals   = proposals.filter(p => p.status === 'pending')
  const completedProposals = proposals.filter(p => p.status !== 'pending')

  return (
    <div class="screen">
      <PageHeader
        title="Forge"
        subtitle="Propose, review, and install AI-generated skills"
        actions={<><button class="btn btn-primary btn-sm" onClick={load}><RefreshIcon /> Refresh</button></>}
      />

      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      {/* ── Section 1: Researching ── */}
      <div style={{ marginBottom: '24px' }}>
        <SectionHeader
          label="Researching"
          sub="Skills Atlas is currently exploring"
          count={researching.length}
        />
        {researching.length === 0 ? (
          <div class="card forge-empty-card">
            <EmptyState message="Atlas is not currently researching any skills." />
          </div>
        ) : (
          <div class="card forge-list-card">
            {researching.map((item, i) => (
              <div key={item.id} class={`forge-research-wrap${i === researching.length - 1 ? ' last' : ''}`}>
                <ResearchingRow item={item} />
              </div>
            ))}
          </div>
        )}
      </div>

      {/* ── Section 2: Proposed ── */}
      <div style={{ marginBottom: '24px' }}>
        <SectionHeader
          label="Proposed"
          sub="Skills awaiting your decision"
          count={pendingProposals.length}
        />
        {pendingProposals.length === 0 ? (
          <div class="card forge-empty-card">
            <EmptyState message="No pending proposals. Atlas will surface new skills here when it identifies useful capabilities." />
          </div>
        ) : (
          pendingProposals.map(proposal => (
            <ProposalCard
              key={proposal.id}
              proposal={proposal}
              onInstall={handleInstall}
              onReject={handleReject}
              acting={acting.has(proposal.id)}
            />
          ))
        )}
      </div>

      {/* ── Section 3: Installed ── */}
      <div style={{ marginBottom: '24px' }}>
        <SectionHeader
          label="Installed"
          sub="Forge skills in your skill registry"
          count={installed.length}
        />
        {installed.length === 0 ? (
          <div class="card forge-empty-card">
            <EmptyState message="No Forge skills installed yet. Approve a proposal above to install one." />
          </div>
        ) : (
          <div class="card forge-list-card">
            {installed.map((skill, i) => (
              <div key={skill.manifest.id} class={`forge-installed-wrap${i === installed.length - 1 ? ' last' : ''}`}>
                <InstalledRow
                  skill={skill}
                  isLast={i === installed.length - 1}
                  onEnable={handleEnable}
                  onDisable={handleDisable}
                  onUninstall={handleUninstall}
                  acting={acting.has(skill.manifest.id)}
                />
              </div>
            ))}
          </div>
        )}
      </div>

      {/* ── Previously decided proposals ── */}
      {completedProposals.length > 0 && (
        <div style={{ marginBottom: '24px', opacity: 0.6 }}>
          <SectionHeader label="History" sub="Previously decided proposals" />
          <div class="card forge-list-card">
            {completedProposals.map((p, i) => (
              <div key={p.id} class={`row forge-history-wrap${i === completedProposals.length - 1 ? ' last' : ''}`} style={{ borderBottom: 'none', gap: '10px' }}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <span style={{ fontSize: '13px', color: 'var(--text)' }}>{p.name}</span>
                  <span style={{ fontSize: '12px', color: 'var(--text-2)', marginLeft: '8px' }}>{p.description}</span>
                </div>
                <div style={{ display: 'flex', gap: '8px', flexShrink: 0 }}>
                  {statusBadge(p.status)}
                  <span style={{ fontSize: '11px', color: 'var(--text-2)' }}>{relativeTime(p.updatedAt)}</span>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  )
}
