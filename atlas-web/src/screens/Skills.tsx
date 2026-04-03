import { useState, useEffect, useRef } from 'preact/hooks'
import { api, SkillRecord, FsRoot } from '../api/client'
import { PageHeader } from '../components/PageHeader'
import { ErrorBanner } from '../components/ErrorBanner'

/* ── Badge helpers ──────────────────────────────────────── */

function riskBadge(level: string) {
  switch (level.toLowerCase()) {
    case 'low':    return <span class="badge badge-green">{level}</span>
    case 'medium': return <span class="badge badge-yellow">{level}</span>
    case 'high':   return <span class="badge badge-red">{level}</span>
    default:       return <span class="badge badge-gray">{level}</span>
  }
}

function permissionBadge(level: string) {
  switch (level.toLowerCase()) {
    case 'read':    return <span class="badge badge-green">{level}</span>
    case 'draft':   return <span class="badge badge-yellow">{level}</span>
    case 'execute': return <span class="badge badge-red">{level}</span>
    default:        return <span class="badge badge-gray">{level}</span>
  }
}

/* ── Icons ──────────────────────────────────────────────── */

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
const SaveIcon = () => (
  <svg width="13" height="13" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <path d="M11 12H3a1 1 0 01-1-1V3a1 1 0 011-1h6.5L12 4.5V11a1 1 0 01-1 1z" />
    <rect x="4.5" y="7.5" width="5" height="4.5" rx=".5" /><path d="M4.5 2v3h4" />
  </svg>
)
const EditIcon = () => (
  <svg width="13" height="13" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <path d="M9.5 2.5l2 2L4 12H2v-2L9.5 2.5z" />
  </svg>
)

/* ── Skill Memory parsers ────────────────────────────────── */

function parsePrinciples(content: string): string[] {
  const match = content.match(/##\s+Orchestration Principles\s*\n([\s\S]*?)(?=\n##|\n---|\s*$)/)
  if (!match) return []
  return match[1].split('\n').map(l => l.trim()).filter(l => l.length > 0 && !l.startsWith('_'))
}

function parseDontWork(content: string): string[] {
  const match = content.match(/##\s+Things That Don't Work\s*\n([\s\S]*?)(?=\n##|\n---|\s*$)/)
  if (!match) return []
  return match[1].split('\n').map(l => l.replace(/^-\s*/, '').trim()).filter(l => l.length > 0 && !l.startsWith('_'))
}

interface Routine { name: string; triggers: string[]; steps: string[]; learned: string }

function parseRoutines(content: string): Routine[] {
  const section = content.match(/##\s+Learned Routines\s*\n([\s\S]*?)(?=\n##\s+[^#]|\n---\s*$|\s*$)/)
  if (!section) return []
  return section[1].split(/\n###\s+/).filter(b => b.trim()).map(block => {
    const lines = block.split('\n')
    const name = lines[0].trim()
    const triggers: string[] = []; const steps: string[] = []; let learned = ''
    for (const line of lines.slice(1)) {
      const t = line.match(/\*\*Triggers:\*\*\s*(.+)/)
      if (t) triggers.push(...t[1].split(',').map(x => x.replace(/"/g, '').trim()).filter(Boolean))
      const s = line.match(/^\d+\.\s+(.+)/); if (s) steps.push(s[1].trim())
      const l = line.match(/\*\*Learned:\*\*\s*(.+)/); if (l) learned = l[1].trim()
    }
    return { name, triggers, steps, learned }
  }).filter(r => r.name)
}

/* ── Policy labels ──────────────────────────────────────── */

const POLICY_LABELS: Record<string, string> = {
  auto_approve: 'Auto-approve',
  ask_once:     'Ask once',
  always_ask:   'Always ask',
}

/* ── Skill grouping ─────────────────────────────────────── */

type SkillGroupKey = 'capabilities' | 'system' | 'automation' | 'custom' | 'diagnostics'

const SKILL_GROUPS: Array<{ key: SkillGroupKey; label: string; sub: string }> = [
  { key: 'capabilities', label: 'Capabilities',    sub: 'What Atlas can do for you' },
  { key: 'system',       label: 'System Skills',    sub: 'File access and system automation' },
  { key: 'automation',   label: 'Automation',       sub: 'Scheduled task management' },
  { key: 'custom',       label: 'Custom Skills',    sub: 'User-installed skill extensions' },
]

function classifySkill(skill: SkillRecord): SkillGroupKey | 'hidden' {
  const { id, isUserVisible, category, source } = skill.manifest
  if (!isUserVisible || id === 'websearch-api') return 'hidden'
  // Both user-installed and forge-generated custom skills land in the custom group.
  // Forge-generated skills show the purple Forge badge; user-installed show teal Custom badge.
  if (source === 'custom' || source === 'forge') return 'custom'
  if (id === 'gremlin-management') return 'automation'
  if (id === 'atlas.info') return 'diagnostics'
  if (category === 'system' || category === 'productivity') return 'system'
  if (category === 'automation') return 'automation'
  return 'capabilities'
}

const RISK_ORDER: Record<string, number> = { critical: 0, high: 1, medium: 2, low: 3 }
function sortByRisk(a: SkillRecord, b: SkillRecord) {
  return (RISK_ORDER[a.manifest.riskLevel] ?? 99) - (RISK_ORDER[b.manifest.riskLevel] ?? 99)
}

/* ── Main component ─────────────────────────────────────── */

export function Skills() {
  // Skills state
  const [skills, setSkills] = useState<SkillRecord[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [acting, setActing] = useState<Set<string>>(new Set())
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [policies, setPolicies] = useState<Record<string, string>>({})

  // Custom skill install state
  const [customInstalling, setCustomInstalling] = useState(false)
  const [customInstallMsg, setCustomInstallMsg] = useState<string | null>(null)
  const [customInstallErr, setCustomInstallErr] = useState<string | null>(null)
  const [customRemoving, setCustomRemoving] = useState<Set<string>>(new Set())

  const installCustomSkill = async () => {
    setCustomInstalling(true); setCustomInstallMsg(null); setCustomInstallErr(null)
    try {
      const result = await api.pickFsFolder()
      if (!result?.path) { setCustomInstalling(false); return }
      const res = await api.installCustomSkill(result.path)
      setCustomInstallMsg(res.message ?? 'Skill installed. Restart Atlas to activate it.')
      await loadSkills()
    } catch (e: unknown) {
      setCustomInstallErr(e instanceof Error ? e.message : 'Install failed.')
    } finally {
      setCustomInstalling(false)
    }
  }

  const removeCustomSkill = async (id: string) => {
    setCustomRemoving(prev => new Set(prev).add(id))
    try {
      await api.removeCustomSkill(id)
      await loadSkills()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to remove skill.')
    } finally {
      setCustomRemoving(prev => { const s = new Set(prev); s.delete(id); return s })
    }
  }

  // File system roots state
  const [fsRoots, setFsRoots] = useState<FsRoot[]>([])
  const [fsRootAdding, setFsRootAdding] = useState(false)
  const [fsRootError, setFsRootError] = useState<string | null>(null)

  const loadFsRoots = async () => {
    try { setFsRoots(await api.fsRoots()) } catch { /* non-fatal */ }
  }

  const browseFsFolder = async () => {
    setFsRootAdding(true); setFsRootError(null)
    try {
      const result = await api.pickFsFolder()
      if (result?.path) { await api.addFsRoot(result.path); await loadFsRoots() }
    } catch { /* user cancelled — 204, ignore */ } finally { setFsRootAdding(false) }
  }

  const removeFsRoot = async (id: string) => {
    try { await api.removeFsRoot(id); await loadFsRoots() }
    catch (e: unknown) { setFsRootError(e instanceof Error ? e.message : 'Failed to remove folder.') }
  }

  // Memory state
  const [memContent, setMemContent] = useState<string | null>(null)
  const [editMode, setEditMode] = useState(false)
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saveOk, setSaveOk] = useState(false)
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  const loadSkills = async () => {
    try {
      const [skillsResult, policiesResult] = await Promise.allSettled([api.skills(), api.actionPolicies()])
      if (skillsResult.status === 'fulfilled') { setSkills(skillsResult.value); setError(null) }
      else throw skillsResult.reason
      if (policiesResult.status === 'fulfilled') setPolicies(policiesResult.value)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load skills.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    loadSkills()
    loadFsRoots()
    api.skillsMemory().then(r => { setMemContent(r.content); setDraft(r.content) }).catch(() => setMemContent(''))
  }, [])

  const toggleExpand = (id: string) => {
    setExpanded(prev => { const s = new Set(prev); s.has(id) ? s.delete(id) : s.add(id); return s })
  }

  const toggleEnable = async (skill: SkillRecord) => {
    const id = skill.manifest.id
    setActing(prev => new Set(prev).add(id))
    try {
      skill.manifest.lifecycleState === 'enabled' ? await api.disableSkill(id) : await api.enableSkill(id)
      await loadSkills()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to toggle skill.')
    } finally {
      setActing(prev => { const s = new Set(prev); s.delete(id); return s })
    }
  }

  const changePolicy = async (actionID: string, policy: string) => {
    setPolicies(prev => ({ ...prev, [actionID]: policy }))
    try {
      const updated = await api.setActionPolicy(actionID, policy)
      setPolicies(updated)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to update policy.')
      await loadSkills()
    }
  }

  const validate = async (id: string) => {
    setActing(prev => new Set(prev).add(`v:${id}`))
    try {
      await api.validateSkill(id); await loadSkills()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to validate skill.')
    } finally {
      setActing(prev => { const s = new Set(prev); s.delete(`v:${id}`); return s })
    }
  }

  const handleEdit = () => {
    setDraft(memContent ?? ''); setEditMode(true); setSaveError(null); setSaveOk(false)
    setTimeout(() => textareaRef.current?.focus(), 50)
  }

  const handleSave = async () => {
    setSaving(true); setSaveError(null); setSaveOk(false)
    try {
      await api.updateSkillsMemory(draft); setMemContent(draft); setEditMode(false)
      setSaveOk(true); setTimeout(() => setSaveOk(false), 2000)
    } catch (e: any) {
      setSaveError(e?.message ?? 'Save failed')
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div class="screen">
        <PageHeader title="Skills" subtitle="Capabilities available to Atlas" />
        <div style={{ display: 'flex', justifyContent: 'center', padding: '48px' }}><span class="spinner" /></div>
      </div>
    )
  }

  const principles = memContent ? parsePrinciples(memContent) : []
  const routines   = memContent ? parseRoutines(memContent) : []
  const dontWork   = memContent ? parseDontWork(memContent) : []

  return (
    <div class="screen">
      <PageHeader
        title="Skills"
        subtitle="Capabilities available to Atlas"
        actions={<><button class="btn btn-primary btn-sm" onClick={loadSkills}><RefreshIcon /> Refresh</button></>}
      />

      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      {/* Skills list */}
      {skills.length === 0 && !error ? (
        <div class="empty-state">
          <svg class="empty-icon" viewBox="0 0 36 36" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
            <polygon points="18,3 22,13 33,13 24,20 27,31 18,24 9,31 12,20 3,13 14,13" />
          </svg>
          <h3>No skills registered</h3>
          <p>Skills will appear here once the daemon bootstraps</p>
        </div>
      ) : (() => {
        const grouped = skills.reduce<Record<string, SkillRecord[]>>((acc, skill) => {
          const key = classifySkill(skill)
          if (key === 'hidden') return acc
          ;(acc[key] ??= []).push(skill)
          return acc
        }, {})
        Object.values(grouped).forEach(g => g.sort(sortByRisk))

        const renderSkillRow = (skill: SkillRecord, i: number, total: number) => {
          const id = skill.manifest.id
          const isEnabled = skill.manifest.lifecycleState === 'enabled'
          const isExpanded = expanded.has(id)
          return (
            <div key={id} style={{ borderBottom: isExpanded || i < total - 1 ? '1px solid var(--border)' : 'none' }}>
              <div class="row" style={{ borderBottom: 'none' }}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div class="skill-name" style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    {skill.manifest.name}
                    {riskBadge(skill.manifest.riskLevel)}
                    {skill.manifest.source === 'forge' && (
                      <span class="badge" style={{ background: 'rgba(139,92,246,0.15)', color: 'rgb(139,92,246)', border: '1px solid rgba(139,92,246,0.3)' }}>Forge</span>
                    )}
                    {skill.manifest.source === 'custom' && (
                      <span class="badge" style={{ background: 'rgba(20,184,166,0.15)', color: 'rgb(20,184,166)', border: '1px solid rgba(20,184,166,0.3)' }}>Custom</span>
                    )}
                    {skill.validation && (
                      <span class={`badge ${skill.validation.status === 'passed' || skill.validation.status === 'warning' ? 'badge-green' : 'badge-red'}`}>
                        {skill.validation.status}
                      </span>
                    )}
                  </div>
                  <div class="skill-meta">
                    v{skill.manifest.version} · {skill.actions.length} action{skill.actions.length !== 1 ? 's' : ''}
                    {skill.manifest.description && <> · {skill.manifest.description}</>}
                  </div>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <button class="btn btn-sm btn-icon" disabled={acting.has(`v:${id}`)} onClick={() => validate(id)} title="Re-validate">
                    {acting.has(`v:${id}`) ? <span class="spinner" style={{ width: '11px', height: '11px' }} /> : <RefreshIcon />}
                  </button>
                  {skill.manifest.source === 'custom' && (
                    <button
                      class="btn btn-sm btn-ghost"
                      style={{ color: 'var(--c-red)', fontSize: '11px', padding: '2px 7px' }}
                      disabled={customRemoving.has(id)}
                      onClick={() => removeCustomSkill(id)}
                      title="Remove this custom skill"
                    >
                      {customRemoving.has(id) ? <span class="spinner" style={{ width: '11px', height: '11px' }} /> : 'Remove'}
                    </button>
                  )}
                  {skill.actions.length > 0 && (
                    <button class="btn btn-sm btn-icon" onClick={() => toggleExpand(id)} title="Show actions">
                      {isExpanded ? <ChevronUp /> : <ChevronDown />}
                    </button>
                  )}
                  <label class="toggle" title={isEnabled ? 'Disable skill' : 'Enable skill'}>
                    <input type="checkbox" checked={isEnabled} disabled={acting.has(id)} onChange={() => toggleEnable(skill)} />
                    <span class="toggle-track" />
                  </label>
                </div>
              </div>
              {isExpanded && skill.actions.length > 0 && (
                <div class="skill-actions-list">
                  <div class="skill-actions-header">
                    <span class="col-name">Name</span>
                    <span class="col-desc">Description</span>
                    <span class="col-level">Level</span>
                    <span class="col-approval">Approval</span>
                  </div>
                  {skill.actions.map(action => (
                    <div class="skill-action-row" key={action.id}>
                      <span class="col-name skill-action-name">{action.name}</span>
                      <span class="col-desc skill-action-desc">{action.description ?? '—'}</span>
                      <span class="col-level">{permissionBadge(action.permissionLevel)}</span>
                      <span class="col-approval">
                        <select class="policy-select" value={policies[action.id] ?? action.approvalPolicy}
                          onChange={e => changePolicy(action.id, (e.target as HTMLSelectElement).value)}>
                          {Object.entries(POLICY_LABELS).map(([val, label]) => <option key={val} value={val}>{label}</option>)}
                        </select>
                      </span>
                    </div>
                  ))}
                  {id === 'file-system' && (
                    <div style={{ borderTop: '1px solid var(--border)', padding: '14px 16px' }}>
                      <div style={{ fontSize: '12px', fontWeight: 600, color: 'var(--text-2)', marginBottom: '10px' }}>
                        Approved Folders
                      </div>
                      {fsRoots.length === 0
                        ? <div style={{ fontSize: '12.5px', color: 'var(--text-2)', marginBottom: '10px' }}>No folders approved yet. Atlas cannot read or write any files until at least one folder is added.</div>
                        : <div style={{ marginBottom: '10px' }}>
                            {fsRoots.map(root => (
                              <div key={root.id} style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '5px 0', borderBottom: '1px solid var(--border)' }}>
                                <span style={{ flex: 1, fontSize: '12.5px', fontFamily: 'monospace', color: 'var(--text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{root.path}</span>
                                <button class="btn btn-sm btn-ghost" style={{ color: 'var(--c-red)', flexShrink: 0 }} onClick={() => removeFsRoot(root.id)}>Remove</button>
                              </div>
                            ))}
                          </div>
                      }
                      {fsRootError && <div style={{ fontSize: '12px', color: 'var(--c-red)', marginBottom: '8px' }}>{fsRootError}</div>}
                      <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
                        <button class="btn btn-primary btn-sm" disabled={fsRootAdding} onClick={browseFsFolder}>
                          {fsRootAdding ? <span class="spinner" style={{ width: '11px', height: '11px' }} /> : 'Add Folder'}
                        </button>
                      </div>
                    </div>
                  )}
                </div>
              )}
            </div>
          )
        }

        return (
          <>
            {SKILL_GROUPS.map(group => {
              const groupSkills = grouped[group.key] ?? []
              const isCustomGroup = group.key === 'custom'

              // Custom group always renders so the install panel is always visible.
              if (!groupSkills.length && !isCustomGroup) return null

              return (
                <div key={group.key} style={{ marginBottom: '20px' }}>
                  <div class="skill-group-header">
                    <span>{group.label}</span>
                    {group.sub && <p class="skill-group-sub">{group.sub}</p>}
                  </div>

                  {/* Install feedback */}
                  {isCustomGroup && customInstallMsg && (
                    <div style={{ fontSize: '12.5px', color: 'var(--c-green)', marginBottom: '10px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                      <span>{customInstallMsg}</span>
                      <button class="btn btn-sm btn-ghost" onClick={() => setCustomInstallMsg(null)}>✕</button>
                    </div>
                  )}
                  {isCustomGroup && customInstallErr && (
                    <div style={{ fontSize: '12.5px', color: 'var(--c-red)', marginBottom: '10px', display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                      <span>{customInstallErr}</span>
                      <button class="btn btn-sm btn-ghost" onClick={() => setCustomInstallErr(null)}>✕</button>
                    </div>
                  )}

                  {isCustomGroup && groupSkills.length === 0 ? (
                    <div class="card" style={{ padding: '24px 20px', textAlign: 'center' }}>
                      <div style={{ fontSize: '13px', fontWeight: 500, color: 'var(--text)', marginBottom: '6px' }}>No custom skills installed</div>
                      <div style={{ fontSize: '12.5px', color: 'var(--text-2)', marginBottom: '16px', maxWidth: '400px', margin: '0 auto 16px' }}>
                        Custom skills are executables in their own folder with a <code style={{ fontFamily: 'monospace', fontSize: '11.5px' }}>skill.json</code> manifest.
                        Forge-generated skills also appear here once installed.
                      </div>
                      <button class="btn btn-primary btn-sm" disabled={customInstalling} onClick={installCustomSkill}>
                        {customInstalling ? <span class="spinner" style={{ width: '11px', height: '11px' }} /> : 'Install from Folder'}
                      </button>
                    </div>
                  ) : (
                    <div class="card">
                      {groupSkills.map((skill, i) => renderSkillRow(skill, i, groupSkills.length))}
                    </div>
                  )}
                </div>
              )
            })}
            {(grouped['diagnostics'] ?? []).length > 0 && (
              <div style={{ marginBottom: '20px', opacity: 0.55 }}>
                <div class="skill-group-header">
                  <span>Diagnostics</span>
                </div>
                <div class="card">
                  {(grouped['diagnostics'] ?? []).map((skill, i) => renderSkillRow(skill, i, grouped['diagnostics']!.length))}
                </div>
              </div>
            )}
          </>
        )
      })()}

      {/* Memory section */}
      <div class="section-divider">
        <div class="section-divider-label">
          <span>Memory</span>
          <p class="section-divider-sub">How Atlas has learned to use skills for you</p>
        </div>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
          {saveOk   && <span style={{ color: 'var(--c-green)', fontSize: '12px' }}>Saved</span>}
          {saveError && <span style={{ color: 'var(--c-red)',   fontSize: '12px' }}>{saveError}</span>}
          {editMode ? (
            <>
              <button class="btn btn-ghost" onClick={() => setEditMode(false)} disabled={saving}>Cancel</button>
              <button class="btn btn-primary" onClick={handleSave} disabled={saving}>
                <SaveIcon />{saving ? 'Saving…' : 'Save'}
              </button>
            </>
          ) : (
            <button class="btn btn-ghost" onClick={handleEdit}><EditIcon />Edit SKILLS.md</button>
          )}
        </div>
      </div>

      {memContent === null ? (
        <div style={{ padding: '24px 0', color: 'var(--text-2)', fontSize: '13px' }}>Loading…</div>
      ) : editMode ? (
        <textarea
          ref={textareaRef}
          class="mind-raw-editor"
          value={draft}
          onInput={e => setDraft((e.target as HTMLTextAreaElement).value)}
          style={{ width: '100%', minHeight: '360px', marginTop: '4px' }}
        />
      ) : (
        <div class="card">
          {/* Orchestration Principles */}
          <div>
            <div style={{ padding: '14px 20px 10px', fontSize: '12px', fontWeight: 600, color: 'var(--text-2)' }}>Orchestration Principles</div>
            {principles.length === 0
              ? <div style={{ padding: '0 20px 16px', color: 'var(--text-2)', fontSize: '12.5px' }}>No principles yet — they'll appear as Atlas learns your preferences.</div>
              : <div style={{ padding: '0 20px 16px' }}>
                  {principles.map((p, i) => (
                    <div key={i} style={{ padding: '6px 0', borderBottom: i < principles.length - 1 ? '1px solid var(--border)' : 'none', fontSize: '13px', color: 'var(--text)' }}>{p}</div>
                  ))}
                </div>
            }
          </div>

          {/* Learned Routines */}
          <div style={{ borderTop: '1px solid var(--border)' }}>
            <div style={{ padding: '14px 20px 10px', fontSize: '12px', fontWeight: 600, color: 'var(--text-2)' }}>Learned Routines</div>
            {routines.length === 0
              ? <div style={{ padding: '0 20px 16px', color: 'var(--text-2)', fontSize: '12.5px' }}>No routines yet — Atlas learns them after you repeat the same skill sequence 3 times.</div>
              : <div style={{ padding: '0 20px 16px' }}>
                  {routines.map((r, i) => (
                    <div key={i} style={{ padding: '8px 0', borderBottom: i < routines.length - 1 ? '1px solid var(--border)' : 'none' }}>
                      <div style={{ fontWeight: 500, fontSize: '13px', marginBottom: '4px' }}>{r.name}</div>
                      {r.triggers.length > 0 && (
                        <div style={{ display: 'flex', gap: '4px', flexWrap: 'wrap', marginBottom: '4px' }}>
                          {r.triggers.map((t, j) => <span key={j} class="badge badge-gray">"{t}"</span>)}
                        </div>
                      )}
                      {r.steps.length > 0 && (
                        <ol style={{ margin: '4px 0 0 16px', fontSize: '12.5px', color: 'var(--text-2)' }}>
                          {r.steps.map((s, j) => <li key={j}>{s}</li>)}
                        </ol>
                      )}
                      {r.learned && <div style={{ marginTop: '4px', fontSize: '12px', color: 'var(--text-2)', fontStyle: 'italic' }}>{r.learned}</div>}
                    </div>
                  ))}
                </div>
            }
          </div>

          {/* Things That Don't Work */}
          <div style={{ borderTop: '1px solid var(--border)' }}>
            <div style={{ padding: '14px 20px 10px', fontSize: '12px', fontWeight: 600, color: 'var(--text-2)' }}>Things That Don't Work</div>
            {dontWork.length === 0
              ? <div style={{ padding: '0 20px 16px', color: 'var(--text-2)', fontSize: '12.5px' }}>Nothing logged yet.</div>
              : <div style={{ padding: '0 20px 16px' }}>
                  {dontWork.map((d, i) => (
                    <div key={i} style={{ padding: '6px 0', borderBottom: i < dontWork.length - 1 ? '1px solid var(--border)' : 'none', fontSize: '13px', color: 'var(--text)' }}>{d}</div>
                  ))}
                </div>
            }
          </div>
        </div>
      )}
    </div>
  )
}
