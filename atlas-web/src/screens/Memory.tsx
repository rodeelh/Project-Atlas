import { useState, useEffect, useRef } from 'preact/hooks'
import { api, MemoryItem } from '../api/client'
import { PageHeader } from '../components/PageHeader'
import { ErrorBanner } from '../components/ErrorBanner'

type Category = 'all' | 'profile' | 'preference' | 'project' | 'workflow' | 'episodic'

const CATEGORIES: Category[] = ['all', 'profile', 'preference', 'project', 'workflow', 'episodic']

function categoryBadge(cat: string) {
  switch (cat.toLowerCase()) {
    case 'profile':    return <span class="badge badge-blue">{cat}</span>
    case 'preference': return <span class="badge badge-green">{cat}</span>
    case 'project':    return <span class="badge badge-yellow">{cat}</span>
    case 'workflow':   return <span class="badge badge-gray">{cat}</span>
    case 'episodic':   return <span class="badge badge-gray">{cat}</span>
    default:           return <span class="badge badge-gray">{cat}</span>
  }
}

const SearchIcon = () => (
  <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
    <circle cx="6" cy="6" r="4.5" />
    <line x1="9.5" y1="9.5" x2="13" y2="13" />
  </svg>
)

export function Memory() {
  const [memories, setMemories] = useState<MemoryItem[]>([])
  const [filtered, setFiltered] = useState<MemoryItem[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [category, setCategory] = useState<Category>('all')
  const [query, setQuery] = useState('')
  const [searching, setSearching] = useState(false)
  const [deleting, setDeleting] = useState<Set<string>>(new Set())
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null)
  const searchTimeout = useRef<ReturnType<typeof setTimeout> | null>(null)

  const load = async () => {
    try {
      const data = await api.memories()
      setMemories(data)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load memories.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  useEffect(() => {
    if (query.trim()) return
    const cat = category === 'all' ? null : category
    setFiltered(cat ? memories.filter(m => m.category.toLowerCase() === cat) : memories)
  }, [memories, category, query])

  useEffect(() => {
    if (!query.trim()) { setSearching(false); return }
    if (searchTimeout.current) clearTimeout(searchTimeout.current)
    searchTimeout.current = setTimeout(async () => {
      setSearching(true)
      try {
        const results = await api.searchMemories(query.trim())
        const cat = category === 'all' ? null : category
        setFiltered(cat ? results.filter(m => m.category.toLowerCase() === cat) : results)
      } catch { /* silent */ } finally {
        setSearching(false)
      }
    }, 350)
  }, [query, category])

  const deleteMemory = async (id: string) => {
    setDeleting(prev => new Set(prev).add(id))
    try {
      await api.deleteMemory(id)
      setMemories(prev => prev.filter(m => m.id !== id))
      setFiltered(prev => prev.filter(m => m.id !== id))
      setConfirmDelete(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to delete memory.')
    } finally {
      setDeleting(prev => { const s = new Set(prev); s.delete(id); return s })
    }
  }

  if (loading) {
    return (
      <div class="screen">
        <PageHeader title="Memory" subtitle="Facts Atlas has learned from your conversations" />
        <div style={{ display: 'flex', justifyContent: 'center', padding: '48px' }}>
          <span class="spinner" />
        </div>
      </div>
    )
  }

  return (
    <div class="screen">
      <PageHeader
        title="Memory"
        subtitle="Facts Atlas has learned from your conversations"
        actions={
          <span class="surface-chip">
            {filtered.length} {filtered.length === 1 ? 'item' : 'items'}
            {category !== 'all' && ` · ${category}`}
          </span>
        }
      />

      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      <div class="card memory-toolbar-card">
        <div style={{ position: 'relative' }}>
          <span style={{ position: 'absolute', left: '10px', top: '50%', transform: 'translateY(-50%)', color: 'var(--text-3)', pointerEvents: 'none' }}>
            <SearchIcon />
          </span>
          <input
            class="input"
            type="search"
            placeholder="Search memories…"
            value={query}
            onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
            style={{ paddingLeft: '32px' }}
          />
        </div>

        <div class="filter-bar">
          {CATEGORIES.map(cat => (
            <button
              key={cat}
              class={`tab-btn${category === cat ? ' active' : ''}`}
              onClick={() => setCategory(cat)}
            >
              {cat.charAt(0).toUpperCase() + cat.slice(1)}
            </button>
          ))}
        </div>
      </div>

      {searching && (
        <div class="memory-searching">
          <span class="spinner" />
          Searching…
        </div>
      )}

      {!searching && filtered.length === 0 && (
        <div class="card memory-empty-card empty-state">
          <svg class="empty-icon" viewBox="0 0 36 36" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
            <ellipse cx="18" cy="9" rx="11" ry="4" />
            <path d="M7 9v6c0 2.2 4.9 4 11 4s11-1.8 11-4V9" />
            <path d="M7 15v6c0 2.2 4.9 4 11 4s11-1.8 11-4v-6" />
          </svg>
          <h3>No memories found</h3>
          <p>{query ? `No results for "${query}"` : 'Atlas will save facts here as you chat.'}</p>
        </div>
      )}

      {!searching && filtered.length > 0 && (
        <div class="card memory-list-card">
          {filtered.map((m, i) => (
            <div key={m.id} style={{ borderBottom: i < filtered.length - 1 ? '1px solid var(--border)' : 'none' }}>
              <div class="row memory-row" style={{ borderBottom: 'none', alignItems: 'flex-start' }}>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div class="memory-title">{m.title}</div>
                  <div class="memory-content">{m.content}</div>
                  <div class="memory-footer">
                    {categoryBadge(m.category)}
                    {m.isUserConfirmed
                      ? <span class="badge badge-green">confirmed</span>
                      : <span class="badge badge-gray">inferred</span>}
                    {m.isSensitive && <span class="badge badge-red">sensitive</span>}
                    {m.tags.map(t => (
                      <span key={t} class="badge badge-gray">{t}</span>
                    ))}
                  </div>
                </div>
                <div style={{ flexShrink: 0, paddingTop: '2px' }}>
                  {confirmDelete === m.id ? (
                    <div style={{ display: 'flex', gap: '6px' }}>
                      <button
                        class="btn btn-sm btn-danger"
                        disabled={deleting.has(m.id)}
                        onClick={() => deleteMemory(m.id)}
                      >
                        {deleting.has(m.id)
                          ? <span class="spinner" style={{ width: '11px', height: '11px' }} />
                          : 'Confirm'}
                      </button>
                      <button class="btn btn-sm" onClick={() => setConfirmDelete(null)}>
                        Cancel
                      </button>
                    </div>
                  ) : (
                    <button
                      class="btn btn-sm btn-danger"
                      onClick={() => setConfirmDelete(m.id)}
                    >
                      Delete
                    </button>
                  )}
                </div>
              </div>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}
