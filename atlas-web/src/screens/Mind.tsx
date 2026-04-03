import { useState, useEffect } from 'preact/hooks'
import { api } from '../api/client'
import { PageHeader } from '../components/PageHeader'
import { ErrorBanner } from '../components/ErrorBanner'

// ── Icons ─────────────────────────────────────────────────────────────────────

const EditIcon = () => (
  <svg width="13" height="13" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <path d="M10 2l2 2-7 7H3v-2L10 2z" />
  </svg>
)

// ── MIND.md renderer ──────────────────────────────────────────────────────────

interface MindSection { title: string; body: string; special: string | null }

function parseMindSections(content: string): MindSection[] {
  const sections: MindSection[] = []
  const parts = content.split(/\n(?=## )/)
  for (const part of parts) {
    const lines = part.split('\n')
    const first = lines[0]
    if (first.startsWith('# ') || !first.startsWith('## ')) continue // skip doc title
    const title = first.slice(3).trim()
    const body = lines.slice(1).join('\n').trim()
    let special: string | null = null
    if (title === "Today's Read") special = 'todays-read'
    if (title === 'Active Theories') special = 'theories'
    sections.push({ title, body, special })
  }
  return sections
}

function BodyText({ text }: { text: string }) {
  const paras = text.split(/\n\n+/).map(p => p.trim()).filter(p => p && p !== '---')
  if (!paras.length) return null
  return (
    <>
      {paras.map((p, i) => (
        <p key={i} style={{ fontSize: '13.5px', lineHeight: 1.65, color: 'var(--text)', marginBottom: i < paras.length - 1 ? '8px' : 0 }}>{p}</p>
      ))}
    </>
  )
}

function TheoriesBlock({ body }: { body: string }) {
  const lines = body.split('\n').map(l => l.replace(/^-\s*/, '').trim()).filter(Boolean)
  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
      {lines.map((line, i) => {
        const isTest    = /\(testing\)/i.test(line)
        const isLikely  = /\(likely\)/i.test(line)
        const isConf    = /\(confirmed\)/i.test(line)
        const isRefuted = /\(refuted\)/i.test(line)
        const clean = line.replace(/\((testing|likely|confirmed|refuted)\)/gi, '').trim()

        let badge: preact.ComponentChild = null
        if (isTest)    badge = <span class="theory-badge testing">testing</span>
        if (isLikely)  badge = <span class="theory-badge likely">likely</span>
        if (isConf)    badge = <span class="theory-badge confirmed">confirmed</span>
        if (isRefuted) badge = <span class="theory-badge refuted">refuted</span>

        return (
          <div key={i} style={{ display: 'flex', alignItems: 'flex-start', gap: '8px', fontSize: '13.5px', lineHeight: 1.5, color: isRefuted ? 'var(--text-3)' : 'var(--text)', textDecoration: isRefuted ? 'line-through' : 'none' }}>
            {badge}<span>{clean}</span>
          </div>
        )
      })}
    </div>
  )
}

function MindSectionCard({ section }: { section: MindSection }) {
  if (section.special === 'todays-read') {
    return (
      <div style={{ borderLeft: '2px solid var(--border-2)', paddingLeft: '14px', opacity: 0.7 }}>
        <div class="mind-section-label">{section.title}</div>
        <BodyText text={section.body} />
      </div>
    )
  }

  return (
    <div class="card">
      <div style={{ padding: '14px 20px 12px', borderBottom: '1px solid var(--border)' }}>
        <span class="mind-section-label">{section.title}</span>
      </div>
      <div style={{ padding: '14px 20px 16px' }}>
        {section.special === 'theories'
          ? <TheoriesBlock body={section.body} />
          : <BodyText text={section.body} />
        }
      </div>
    </div>
  )
}

// ── SKILLS.md parsers ─────────────────────────────────────────────────────────

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

// ── Main screen ───────────────────────────────────────────────────────────────

export function Mind() {
  // MIND.md
  const [content, setContent]   = useState('')
  const [loading, setLoading]   = useState(true)
  const [error, setError]       = useState<string | null>(null)
  const [editing, setEditing]   = useState(false)
  const [editText, setEditText] = useState('')
  const [saving, setSaving]     = useState(false)

  // SKILLS.md
  const [skillsMem, setSkillsMem]         = useState<string | null>(null)
  const [skillsEditing, setSkillsEditing] = useState(false)
  const [skillsDraft, setSkillsDraft]     = useState('')
  const [skillsSaving, setSkillsSaving]   = useState(false)
  const [skillsSaveOk, setSkillsSaveOk]   = useState(false)
  const [skillsError, setSkillsError]     = useState<string | null>(null)

  async function load() {
    setError(null)
    try {
      const data = await api.mind()
      setContent(data.content)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to load MIND.md.')
    } finally {
      setLoading(false)
    }
  }

  async function loadSkillsMem() {
    try {
      const data = await api.skillsMemory()
      setSkillsMem(data.content)
      setSkillsDraft(data.content)
    } catch { setSkillsMem('') }
  }

  async function saveEdit() {
    setSaving(true); setError(null)
    try {
      await api.updateMind(editText)
      setContent(editText)
      setEditing(false)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Save failed.')
    } finally { setSaving(false) }
  }

  async function saveSkillsMem() {
    setSkillsSaving(true); setSkillsError(null)
    try {
      await api.updateSkillsMemory(skillsDraft)
      setSkillsMem(skillsDraft)
      setSkillsEditing(false)
      setSkillsSaveOk(true); setTimeout(() => setSkillsSaveOk(false), 2000)
    } catch (e: unknown) {
      setSkillsError(e instanceof Error ? e.message : 'Save failed.')
    } finally { setSkillsSaving(false) }
  }

  useEffect(() => {
    load()
    loadSkillsMem()
  }, [])

  const sections = content ? parseMindSections(content) : []
  const principles = skillsMem ? parsePrinciples(skillsMem) : []
  const routines   = skillsMem ? parseRoutines(skillsMem)   : []
  const dontWork   = skillsMem ? parseDontWork(skillsMem)   : []
  const hasSkills  = principles.length > 0 || routines.length > 0 || dontWork.length > 0

  return (
    <div class="screen">
      <PageHeader
        title="Mind"
        subtitle="Atlas's living inner world — updated after every conversation"
        actions={!editing && (
          <button class="btn btn-ghost btn-sm" onClick={() => { setEditText(content); setEditing(true) }}>
            <EditIcon /> Edit MIND.md
          </button>
        )}
      />

      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      {/* Loading */}
      {loading && (
        <div style={{ display: 'flex', justifyContent: 'center', padding: '48px' }}>
          <span class="spinner" />
        </div>
      )}

      {/* Empty */}
      {!loading && !content && !editing && (
        <div class="empty-state">
          <p>MIND.md is empty — Atlas will seed it on the next daemon start.</p>
        </div>
      )}

      {/* ── MIND.md edit mode ── */}
      {editing && (
        <>
          <textarea
            class="mind-raw-editor"
            value={editText}
            onInput={(e) => setEditText((e.target as HTMLTextAreaElement).value)}
            style={{ width: '100%', minHeight: '520px' }}
          />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: '8px', marginTop: '10px' }}>
            <button class="btn btn-ghost btn-sm" onClick={() => setEditing(false)} disabled={saving}>Cancel</button>
            <button class="btn btn-primary btn-sm" onClick={saveEdit} disabled={saving}>
              {saving ? 'Saving…' : 'Save'}
            </button>
          </div>
        </>
      )}

      {/* ── MIND.md sections ── */}
      {!loading && content && !editing && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
          {sections.map((s, i) => <MindSectionCard key={i} section={s} />)}
        </div>
      )}

      {/* ── Skill Memory ── */}
      {!loading && !editing && (
        <>
          <div class="section-divider" style={{ marginTop: '32px' }}>
            <div class="section-divider-label">
              <span>Skill Memory</span>
              <p class="section-divider-sub">How Atlas has learned to use its tools for you</p>
            </div>
            <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
              {skillsSaveOk && <span style={{ fontSize: '12px', color: 'var(--green)' }}>Saved</span>}
              {skillsError  && <span style={{ fontSize: '12px', color: 'var(--red)' }}>{skillsError}</span>}
              {skillsEditing ? (
                <>
                  <button class="btn btn-ghost btn-sm" onClick={() => setSkillsEditing(false)} disabled={skillsSaving}>Cancel</button>
                  <button class="btn btn-primary btn-sm" onClick={saveSkillsMem} disabled={skillsSaving}>
                    {skillsSaving ? 'Saving…' : 'Save'}
                  </button>
                </>
              ) : (
                <button class="btn btn-ghost btn-sm" onClick={() => { setSkillsDraft(skillsMem ?? ''); setSkillsEditing(true) }}>
                  <EditIcon /> Edit SKILLS.md
                </button>
              )}
            </div>
          </div>

          {skillsEditing ? (
            <textarea
              class="mind-raw-editor"
              value={skillsDraft}
              onInput={e => setSkillsDraft((e.target as HTMLTextAreaElement).value)}
              style={{ width: '100%', minHeight: '320px' }}
            />
          ) : !hasSkills ? (
            <div style={{ padding: '8px 0 24px', fontSize: '13px', color: 'var(--text-2)' }}>
              Nothing learned yet — Atlas builds this automatically as it uses skills for you.
            </div>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
              {principles.length > 0 && (
                <div class="card">
                  <div style={{ padding: '12px 20px 10px', borderBottom: '1px solid var(--border)' }}>
                    <span class="mind-section-label">Orchestration Principles</span>
                  </div>
                  <div>
                    {principles.map((p, i) => (
                      <div key={i} class="row" style={{ padding: '10px 20px', fontSize: '13.5px', color: 'var(--text)', borderBottom: i < principles.length - 1 ? '1px solid var(--border)' : 'none' }}>
                        {p}
                      </div>
                    ))}
                  </div>
                </div>
              )}
              {routines.length > 0 && (
                <div class="card">
                  <div style={{ padding: '12px 20px 10px', borderBottom: '1px solid var(--border)' }}>
                    <span class="mind-section-label">Learned Routines</span>
                  </div>
                  <div>
                    {routines.map((r, i) => (
                      <div key={i} style={{ padding: '12px 20px', borderBottom: i < routines.length - 1 ? '1px solid var(--border)' : 'none' }}>
                        <div style={{ fontWeight: 500, fontSize: '13.5px', color: 'var(--text)', marginBottom: '6px' }}>{r.name}</div>
                        {r.triggers.length > 0 && (
                          <div style={{ display: 'flex', gap: '4px', flexWrap: 'wrap', marginBottom: '6px' }}>
                            {r.triggers.map((t, j) => <span key={j} class="badge badge-gray">"{t}"</span>)}
                          </div>
                        )}
                        {r.steps.length > 0 && (
                          <ol style={{ margin: '0 0 0 16px', fontSize: '13px', color: 'var(--text-2)', lineHeight: 1.6 }}>
                            {r.steps.map((s, j) => <li key={j}>{s}</li>)}
                          </ol>
                        )}
                        {r.learned && (
                          <div style={{ marginTop: '6px', fontSize: '12.5px', color: 'var(--text-2)', fontStyle: 'italic' }}>{r.learned}</div>
                        )}
                      </div>
                    ))}
                  </div>
                </div>
              )}
              {dontWork.length > 0 && (
                <div class="card">
                  <div style={{ padding: '12px 20px 10px', borderBottom: '1px solid var(--border)' }}>
                    <span class="mind-section-label">Things That Don't Work</span>
                  </div>
                  <div>
                    {dontWork.map((d, i) => (
                      <div key={i} class="row" style={{ padding: '10px 20px', fontSize: '13.5px', color: 'var(--text-2)', borderBottom: i < dontWork.length - 1 ? '1px solid var(--border)' : 'none' }}>
                        {d}
                      </div>
                    ))}
                  </div>
                </div>
              )}
            </div>
          )}
        </>
      )}
    </div>
  )
}
