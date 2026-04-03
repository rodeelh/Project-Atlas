import { useState, useEffect, useRef } from 'preact/hooks'
import { api } from '../api/client'
import { PageHeader } from '../components/PageHeader'

// ── Helpers ──────────────────────────────────────────────────────────────────

function formatMarkdown(content: string): preact.ComponentChild[] {
  const sections = content.split(/\n(?=## )/)
  return sections.map((section, i) => {
    const lines = section.split('\n')
    const firstLine = lines[0]

    if (firstLine.startsWith('# ')) {
      return (
        <div key={i} class="mind-doc-title">
          <h1>{firstLine.slice(2)}</h1>
          <div class="mind-doc-meta">{lines.slice(1).filter(l => l.trim()).join(' ')}</div>
        </div>
      )
    }

    if (firstLine.startsWith('## ')) {
      const title = firstLine.slice(3)
      const body = lines.slice(1).join('\n').trim()
      const isTodaysRead = title === "Today's Read"
      const isWhoIAm = title === 'Who I Am'
      const isTheories = title === 'Active Theories'

      return (
        <div key={i} class={`mind-section${isTodaysRead ? ' todays-read' : ''}${isWhoIAm ? ' who-i-am' : ''}`}>
          <h2 class="mind-section-title">{title}</h2>
          {isTheories
            ? <TheoriesBlock content={body} />
            : <div class="mind-section-body">{renderBody(body)}</div>
          }
        </div>
      )
    }

    return <div key={i} class="mind-section-body">{renderBody(section)}</div>
  })
}

function renderBody(text: string): preact.ComponentChild {
  if (!text.trim()) return null
  const paragraphs = text.split(/\n\n+/)
  return (
    <>
      {paragraphs.map((para, i) => {
        const trimmed = para.trim()
        if (!trimmed || trimmed === '---') return null
        return <p key={i}>{trimmed}</p>
      })}
    </>
  )
}

function TheoriesBlock({ content }: { content: string }) {
  const lines = content.split('\n').filter(l => l.trim())
  return (
    <div class="theories-list">
      {lines.map((line, i) => {
        const testingMatch = line.match(/\(testing\)/i)
        const likelyMatch  = line.match(/\(likely\)/i)
        const confirmedMatch = line.match(/\(confirmed\)/i)
        const refutedMatch = line.match(/\(refuted\)/i)

        let badge: preact.ComponentChild = null
        if (testingMatch)  badge = <span class="theory-badge testing">testing</span>
        if (likelyMatch)   badge = <span class="theory-badge likely">likely</span>
        if (confirmedMatch) badge = <span class="theory-badge confirmed">confirmed</span>
        if (refutedMatch)  badge = <span class="theory-badge refuted">refuted</span>

        const cleanLine = line
          .replace(/\(testing\)/gi, '')
          .replace(/\(likely\)/gi, '')
          .replace(/\(confirmed\)/gi, '')
          .replace(/\(refuted\)/gi, '')
          .trim()

        return (
          <div key={i} class={`theory-item${refutedMatch ? ' refuted' : ''}`}>
            {badge}
            <span>{cleanLine}</span>
          </div>
        )
      })}
    </div>
  )
}

// ── Icons ────────────────────────────────────────────────────────────────────

const RefreshIcon = () => (
  <svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <path d="M2.5 8a5.5 5.5 0 0 1 9.5-3.8" />
    <polyline points="13.5,2.5 13.5,6 10,6" />
    <path d="M13.5 8a5.5 5.5 0 0 1-9.5 3.8" />
    <polyline points="2.5,13.5 2.5,10 6,10" />
  </svg>
)

const EditIcon = () => (
  <svg width="13" height="13" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <path d="M10 2l2 2-7 7H3v-2L10 2z" />
  </svg>
)

// ── Main screen ──────────────────────────────────────────────────────────────

export function Mind() {
  const [content, setContent]   = useState('')
  const [loading, setLoading]   = useState(true)
  const [error, setError]       = useState<string | null>(null)
  const [editing, setEditing]   = useState(false)
  const [editText, setEditText] = useState('')
  const [saving, setSaving]     = useState(false)
  const intervalRef = useRef<number | null>(null)

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

  useEffect(() => {
    load()
    intervalRef.current = setInterval(load, 30000) as unknown as number
    return () => { if (intervalRef.current) clearInterval(intervalRef.current) }
  }, [])

  function openEdit() {
    setEditText(content)
    setEditing(true)
  }

  async function saveEdit() {
    setSaving(true)
    setError(null)
    try {
      await api.updateMind(editText)
      setContent(editText)
      setEditing(false)
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Save failed.')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div class="screen mind-screen">
      <PageHeader
        title="Mind"
        subtitle="Atlas's living inner world — updated after every conversation."
        actions={<>
          <button class="btn btn-ghost btn-sm" onClick={openEdit}><EditIcon /> Edit</button>
          <button class="btn btn-primary btn-sm" onClick={load}><RefreshIcon /> Refresh</button>
        </>}
      />

      {error && <p class="error-banner">{error}</p>}

      {loading && <p class="empty-state">Loading MIND.md…</p>}

      {!loading && !content && (
        <p class="empty-state">MIND.md is empty. Atlas will seed it on the next daemon start.</p>
      )}

      {!loading && content && !editing && (
        <div class="mind-document">
          {formatMarkdown(content)}
        </div>
      )}

      {editing && (
        <div class="mind-editor">
          <textarea
            class="mind-raw-editor"
            value={editText}
            onInput={(e) => setEditText((e.target as HTMLTextAreaElement).value)}
            rows={30}
          />
          <div class="mind-editor-footer">
            <button class="btn btn-ghost btn-sm" onClick={() => setEditing(false)} disabled={saving}>Cancel</button>
            <button class="btn btn-primary btn-sm" onClick={saveEdit} disabled={saving}>
              {saving ? 'Saving…' : 'Save'}
            </button>
          </div>
        </div>
      )}
    </div>
  )
}
