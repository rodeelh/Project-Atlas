import { useState, useEffect, useRef } from 'preact/hooks'
import { api, APIKeyStatus } from '../api/client'
import { PageHeader } from '../components/PageHeader'
import { ErrorBanner } from '../components/ErrorBanner'

const PROVIDERS = [
  { id: 'openai',      label: 'OpenAI',              sublabel: 'API key for OpenAI models (GPT-4.1 etc.)',                            key: 'openAIKeySet'      },
  { id: 'anthropic',   label: 'Anthropic',           sublabel: 'API key for Claude models (Sonnet, Opus, Haiku)',                     key: 'anthropicKeySet'   },
  { id: 'gemini',      label: 'Google Gemini',       sublabel: 'API key for Gemini models (Flash, Pro etc.)',                        key: 'geminiKeySet'      },
  { id: 'lm_studio',   label: 'LM Studio',           sublabel: 'Optional Bearer token for LM Studio v0.4.8+ authentication',         key: 'lmStudioKeySet'    },
  { id: 'telegram',    label: 'Telegram Bot',        sublabel: 'Required for Telegram integration',                                   key: 'telegramTokenSet'  },
  { id: 'discord',     label: 'Discord Bot',         sublabel: 'Connects Atlas through your Discord bot token',                      key: 'discordTokenSet'   },
  { id: 'slackBot',    label: 'Slack Bot Token',     sublabel: 'Use the Bot User OAuth Token (xoxb-) for Slack DMs and @mentions',   key: 'slackBotTokenSet'  },
  { id: 'slackApp',    label: 'Slack App Token',     sublabel: 'Use the App-Level Token (xapp-) for Slack Socket Mode connectivity', key: 'slackAppTokenSet'  },
  { id: 'braveSearch', label: 'Brave Search',        sublabel: 'Enables the Web Search skill (websearch.query)',                      key: 'braveSearchKeySet' },
  { id: 'finnhub',     label: 'Finnhub',             sublabel: 'Enables real-time stock quotes via the Finance skill (optional — falls back to Yahoo Finance)', key: 'finnhubKeySet'     },
] as const

export function APIKeys() {
  const [keyStatus, setKeyStatus] = useState<APIKeyStatus | null>(null)
  const [loading, setLoading]     = useState(true)
  const [error, setError]         = useState<string | null>(null)
  const [addingNew, setAddingNew] = useState(false)
  const loadingRef                = useRef(false)

  const loadKeys = () => {
    if (loadingRef.current) return
    loadingRef.current = true
    api.apiKeys()
      .then(s => setKeyStatus({ ...s, customKeys: s.customKeys ?? [] }))
      .catch(err => setError(err instanceof Error ? err.message : 'Failed to load API key status.'))
      .finally(() => { setLoading(false); loadingRef.current = false })
  }

  // Initial load
  useEffect(() => { loadKeys() }, [])

  // Re-fetch when the tab regains focus so keys stored via the native macOS settings
  // app are reflected without requiring a page reload.
  useEffect(() => {
    const onFocus = () => loadKeys()
    window.addEventListener('focus', onFocus)
    return () => window.removeEventListener('focus', onFocus)
  }, [])

  const handleSaved = (updated: APIKeyStatus) =>
    setKeyStatus({ ...updated, customKeys: updated.customKeys ?? [] })

  if (loading) {
    return (
      <div class="screen">
        <PageHeader title="Credentials" subtitle="Keys, tokens, and provider credentials Atlas uses to operate." />
        <div style={{ display: 'flex', justifyContent: 'center', padding: '48px' }}>
          <span class="spinner" />
        </div>
      </div>
    )
  }

  const customKeys = keyStatus?.customKeys ?? []

  return (
    <div class="screen">
      <PageHeader title="Credentials" subtitle="Keys, tokens, and provider credentials Atlas uses to operate." />

      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      {/* Built-in providers */}
      <div>
        <div class="section-label">Providers</div>
        <div class="card settings-group">
          {PROVIDERS.map((p, i) => (
            <KeyRow
              key={p.id}
              providerID={p.id}
              label={p.label}
              sublabel={p.sublabel}
              configured={keyStatus?.[p.key] ?? false}
              last={i === PROVIDERS.length - 1}
              onSaved={handleSaved}
            />
          ))}
        </div>
      </div>

      {/* Custom keys */}
      <div>
        <div class="section-label" style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
          <span>Custom Keys</span>
          {!addingNew && (
            <button
              class="btn btn-sm"
              style={{ textTransform: 'none', letterSpacing: 0, fontSize: '11px' }}
              onClick={() => setAddingNew(true)}
            >
              + Add key
            </button>
          )}
        </div>

        <div class="card settings-group">
          {customKeys.map((name, i) => (
            <CustomKeyRow
              key={name}
              name={name}
              last={i === customKeys.length - 1 && !addingNew}
              onSaved={handleSaved}
            />
          ))}

          {addingNew && (
            <AddKeyRow
              last
              onSaved={(updated) => { handleSaved(updated); setAddingNew(false) }}
              onCancel={() => setAddingNew(false)}
            />
          )}

          {customKeys.length === 0 && !addingNew && (
            <div style={{ padding: '16px 20px', fontSize: '13px', color: 'var(--text-3)' }}>
              No custom keys yet.
            </div>
          )}
        </div>
      </div>
    </div>
  )
}

// ── Built-in provider row ─────────────────────────────────────────────────────

interface KeyRowProps {
  providerID: string
  label: string
  sublabel: string
  configured: boolean
  last: boolean
  onSaved: (updated: APIKeyStatus) => void
}

function KeyRow({ providerID, label, sublabel, configured, last, onSaved }: KeyRowProps) {
  const [editing, setEditing] = useState(false)
  const [value, setValue]     = useState('')
  const [saving, setSaving]   = useState(false)
  const [err, setErr]         = useState<string | null>(null)

  const save = async () => {
    if (!value.trim()) return
    setSaving(true); setErr(null)
    try {
      const updated = await api.setAPIKey(providerID, value.trim())
      onSaved(updated); setValue(''); setEditing(false)
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to save.')
    } finally { setSaving(false) }
  }

  const cancel = () => { setValue(''); setEditing(false); setErr(null) }

  return (
    <div style={{ borderBottom: last && !editing ? 'none' : '1px solid var(--border)' }}>
      <div class="settings-row" style={{ borderBottom: 'none' }}>
        <div class="settings-label-col">
          <div class="settings-label">{label}</div>
          <div class="settings-sublabel">{sublabel}</div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
          <StatusDot configured={configured} />
          <button class="btn btn-sm" onClick={() => setEditing(v => !v)}>
            {configured ? 'Change' : 'Add'}
          </button>
        </div>
      </div>
      {editing && <KeyInput value={value} onChange={setValue} onSave={save} onCancel={cancel} saving={saving} err={err} placeholder={`Paste ${label} key…`} />}
    </div>
  )
}

// ── Custom key row ────────────────────────────────────────────────────────────

function CustomKeyRow({ name, last, onSaved }: { name: string; last: boolean; onSaved: (u: APIKeyStatus) => void }) {
  const [editing, setEditing]   = useState(false)
  const [value, setValue]       = useState('')
  const [saving, setSaving]     = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [err, setErr]           = useState<string | null>(null)

  const save = async () => {
    if (!value.trim()) return
    setSaving(true); setErr(null)
    try {
      const updated = await api.setAPIKey('custom', value.trim(), name)
      onSaved(updated); setValue(''); setEditing(false)
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to save.')
    } finally { setSaving(false) }
  }

  const remove = async () => {
    setDeleting(true)
    try {
      const updated = await api.deleteAPIKey(name)
      onSaved(updated)
    } catch { /* best-effort */ } finally { setDeleting(false) }
  }

  return (
    <div style={{ borderBottom: last && !editing ? 'none' : '1px solid var(--border)' }}>
      <div class="settings-row" style={{ borderBottom: 'none' }}>
        <div class="settings-label-col">
          <div class="settings-label" style={{ fontFamily: 'var(--font-mono)', fontSize: '13px' }}>{name}</div>
          <div class="settings-sublabel">Custom API key</div>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
          <StatusDot configured />
          <button class="btn btn-sm" onClick={() => setEditing(v => !v)}>Change</button>
          <button class="btn btn-sm btn-danger" disabled={deleting} onClick={remove}>
            {deleting ? <span class="spinner" style={{ width: '10px', height: '10px' }} /> : 'Delete'}
          </button>
        </div>
      </div>
      {editing && <KeyInput value={value} onChange={setValue} onSave={save} onCancel={() => { setValue(''); setEditing(false); setErr(null) }} saving={saving} err={err} placeholder={`New value for ${name}…`} />}
    </div>
  )
}

// ── Add new key row ───────────────────────────────────────────────────────────

function AddKeyRow({ last, onSaved, onCancel }: { last: boolean; onSaved: (u: APIKeyStatus) => void; onCancel: () => void }) {
  const [name, setName]   = useState('')
  const [value, setValue] = useState('')
  const [saving, setSaving] = useState(false)
  const [err, setErr]     = useState<string | null>(null)

  const save = async () => {
    if (!name.trim() || !value.trim()) { setErr('Both a name and a key value are required.'); return }
    setSaving(true); setErr(null)
    try {
      const updated = await api.setAPIKey('custom', value.trim(), name.trim())
      onSaved(updated)
    } catch (e) {
      setErr(e instanceof Error ? e.message : 'Failed to save.')
    } finally { setSaving(false) }
  }

  return (
    <div style={{ borderBottom: last ? 'none' : '1px solid var(--border)', padding: '14px 20px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
      <div style={{ display: 'flex', gap: '8px' }}>
        <input
          class="input"
          type="text"
          placeholder="Key name (e.g. SERPER_API_KEY)"
          value={name}
          onInput={e => setName((e.target as HTMLInputElement).value)}
          style={{ fontFamily: 'var(--font-mono)', fontSize: '12.5px' }}
          autoFocus
        />
        <input
          class="input"
          type="password"
          placeholder="Key value"
          value={value}
          onInput={e => setValue((e.target as HTMLInputElement).value)}
          onKeyDown={e => { if (e.key === 'Enter') save(); if (e.key === 'Escape') onCancel() }}
        />
      </div>
      {err && <div style={{ fontSize: '12px', color: 'var(--red)' }}>{err}</div>}
      <div style={{ display: 'flex', gap: '6px' }}>
        <button class="btn btn-sm btn-primary" onClick={save} disabled={saving || !name.trim() || !value.trim()}>
          {saving ? <span class="spinner" style={{ width: '11px', height: '11px', borderTopColor: '#000', borderColor: 'rgba(0,0,0,0.2)' }} /> : 'Save'}
        </button>
        <button class="btn btn-sm" onClick={onCancel} disabled={saving}>Cancel</button>
      </div>
    </div>
  )
}

// ── Shared helpers ────────────────────────────────────────────────────────────

function StatusDot({ configured }: { configured: boolean }) {
  return (
    <span style={{ display: 'inline-flex', alignItems: 'center', gap: '5px', fontSize: '12.5px', fontWeight: 500, color: configured ? 'var(--green)' : 'var(--text-3)' }}>
      <span style={{ width: '7px', height: '7px', borderRadius: '50%', flexShrink: 0, backgroundColor: configured ? 'var(--green)' : 'var(--text-3)' }} />
      {configured ? 'Configured' : 'Not set'}
    </span>
  )
}

function KeyInput({ value, onChange, onSave, onCancel, saving, err, placeholder }: {
  value: string; onChange: (v: string) => void; onSave: () => void; onCancel: () => void
  saving: boolean; err: string | null; placeholder: string
}) {
  return (
    <div style={{ padding: '0 20px 14px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
      <input
        class="input"
        type="password"
        placeholder={placeholder}
        value={value}
        onInput={e => onChange((e.target as HTMLInputElement).value)}
        onKeyDown={e => { if (e.key === 'Enter') onSave(); if (e.key === 'Escape') onCancel() }}
        autoFocus
      />
      {err && <div style={{ fontSize: '12px', color: 'var(--red)' }}>{err}</div>}
      <div style={{ display: 'flex', gap: '6px' }}>
        <button class="btn btn-sm btn-primary" onClick={onSave} disabled={saving || !value.trim()}>
          {saving ? <span class="spinner" style={{ width: '11px', height: '11px', borderTopColor: '#000', borderColor: 'rgba(0,0,0,0.2)' }} /> : 'Save'}
        </button>
        <button class="btn btn-sm" onClick={onCancel} disabled={saving}>Cancel</button>
      </div>
    </div>
  )
}
