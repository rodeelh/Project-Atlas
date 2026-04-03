import { useEffect, useState, useRef } from 'preact/hooks'
import { api, type EngineStatus, type EngineModelInfo, type RuntimeConfig } from '../api/client'
import { PageHeader } from '../components/PageHeader'
import { ErrorBanner } from '../components/ErrorBanner'
import { parseModelInfo } from '../modelName'

// Pinned default — must match Atlas/Makefile LLAMA_VERSION
const PINNED_VERSION = 'b8641'

// ── Curated starter models ────────────────────────────────────────────────────
const PRIMARY_MODELS = [
  {
    label: 'Gemma 4 E4B  (Q4_K_M · ~2.5 GB · 8 GB+ RAM)',
    filename: 'google_gemma-4-E4B-it-Q4_K_M.gguf',
    url: 'https://huggingface.co/bartowski/google_gemma-4-E4B-it-GGUF/resolve/main/google_gemma-4-E4B-it-Q4_K_M.gguf',
  },
  {
    label: 'Qwen 3 8B  (Q4_K_M · ~5.2 GB · 16 GB+ RAM)',
    filename: 'Qwen_Qwen3-8B-Q4_K_M.gguf',
    url: 'https://huggingface.co/bartowski/Qwen_Qwen3-8B-GGUF/resolve/main/Qwen_Qwen3-8B-Q4_K_M.gguf',
  },
  {
    label: 'Gemma 3 12B  (Q4_K_M · ~7.5 GB · 16 GB+ RAM)',
    filename: 'google_gemma-3-12b-it-Q4_K_M.gguf',
    url: 'https://huggingface.co/bartowski/google_gemma-3-12b-it-GGUF/resolve/main/google_gemma-3-12b-it-Q4_K_M.gguf',
  },
  {
    label: 'Gemma 4 26B MoE  (Q4_K_M · ~14 GB · 24 GB+ RAM)',
    filename: 'google_gemma-4-26B-A4B-it-Q4_K_M.gguf',
    url: 'https://huggingface.co/bartowski/google_gemma-4-26B-A4B-it-GGUF/resolve/main/google_gemma-4-26B-A4B-it-Q4_K_M.gguf',
  },
  {
    label: 'Qwen 3.5 27B  (Q4_K_M · ~17 GB · 32 GB+ RAM)',
    filename: 'Qwen_Qwen3.5-27B-Q4_K_M.gguf',
    url: 'https://huggingface.co/bartowski/Qwen_Qwen3.5-27B-GGUF/resolve/main/Qwen_Qwen3.5-27B-Q4_K_M.gguf',
  },
]

const ROUTER_MODELS = [
  {
    label: 'Qwen 3 1.7B  (Q4_K_M · ~1.3 GB · recommended)',
    filename: 'Qwen_Qwen3-1.7B-Q4_K_M.gguf',
    url: 'https://huggingface.co/bartowski/Qwen_Qwen3-1.7B-GGUF/resolve/main/Qwen_Qwen3-1.7B-Q4_K_M.gguf',
  },
  {
    label: 'Ministral 3B  (Q4_K_M · ~2.2 GB · agentic)',
    filename: 'mistralai_Ministral-3-3B-Instruct-2512-Q4_K_M.gguf',
    url: 'https://huggingface.co/bartowski/mistralai_Ministral-3-3B-Instruct-2512-GGUF/resolve/main/mistralai_Ministral-3-3B-Instruct-2512-Q4_K_M.gguf',
  },
  {
    label: 'Gemma 3 1B  (Q4_K_M · ~0.8 GB · lightest)',
    filename: 'google_gemma-3-1b-it-Q4_K_M.gguf',
    url: 'https://huggingface.co/bartowski/google_gemma-3-1b-it-GGUF/resolve/main/google_gemma-3-1b-it-Q4_K_M.gguf',
  },
]

const ALL_STARTER_MODELS = [...PRIMARY_MODELS, ...ROUTER_MODELS]

type DownloadState = {
  filename: string
  downloaded: number
  total: number
  percent: number
  done: boolean
  error: string | null
}

type UpdateState = {
  version: string
  downloaded: number
  total: number
  percent: number
  done: boolean
  error: string | null
}

const CTX_SIZE_OPTIONS = [4096, 8192, 16384, 32768, 65536]

export function AtlasEngine() {
  const [status, setStatus]   = useState<EngineStatus | null>(null)
  const [models, setModels]   = useState<EngineModelInfo[]>([])
  const [error, setError]     = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [acting, setActing]   = useState(false)

  // Context size + KV cache quant — read from config, editable here
  const [ctxSize, setCtxSize]             = useState(8192)
  const [ctxSizeSaving, setCtxSizeSaving] = useState(false)
  const [kvQuant, setKvQuant]             = useState('q4_0')
  const [kvQuantSaving, setKvQuantSaving] = useState(false)

  // Tool Router (Phase 3)
  const [routerStatus, setRouterStatus]     = useState<EngineStatus | null>(null)
  const [routerModel, setRouterModel]       = useState('')
  const [routerModelSaving, setRouterModelSaving] = useState(false)
  const [routerActing, setRouterActing]     = useState(false)

  const [dlURL, setDlURL]           = useState('')
  const [dlFilename, setDlFilename] = useState('')
  const [dlPreset, setDlPreset]     = useState('')
  const [download, setDownload]     = useState<DownloadState | null>(null)
  const dlAbortRef = useRef<(() => void) | null>(null)

  const [update, setUpdate]         = useState<UpdateState | null>(null)
  const updateAbortRef = useRef<(() => void) | null>(null)

  const load = async () => {
    try {
      const [s, m, rs] = await Promise.all([
        api.engineStatus(),
        api.engineModels(),
        api.engineRouterStatus().catch(() => null),
      ])
      setStatus(s); setModels(m); setError(null)
      if (rs) setRouterStatus(rs)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'Failed to load engine status.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
    const interval = setInterval(load, 4000)
    return () => clearInterval(interval)
  }, [])

  // Load ctx size, KV quant + router model from config once on mount
  useEffect(() => {
    api.config().then(cfg => {
      if (cfg.atlasEngineCtxSize && cfg.atlasEngineCtxSize > 0) setCtxSize(cfg.atlasEngineCtxSize)
      if (cfg.atlasEngineKVCacheQuant) setKvQuant(cfg.atlasEngineKVCacheQuant)
      if (cfg.atlasEngineRouterModel) setRouterModel(cfg.atlasEngineRouterModel)
    }).catch(() => {})
  }, [])

  const handleCtxSizeChange = async (newSize: number) => {
    setCtxSize(newSize)
    setCtxSizeSaving(true)
    try {
      await api.updateConfig({ atlasEngineCtxSize: newSize } as Partial<RuntimeConfig>)
    } catch {
      // best-effort
    } finally {
      setCtxSizeSaving(false)
    }
  }

  const handleKvQuantChange = async (quant: string) => {
    setKvQuant(quant)
    setKvQuantSaving(true)
    try {
      await api.updateConfig({ atlasEngineKVCacheQuant: quant } as Partial<RuntimeConfig>)
    } catch {
      // best-effort
    } finally {
      setKvQuantSaving(false)
    }
  }

  const handleRouterModelChange = async (model: string) => {
    setRouterModel(model)
    setRouterModelSaving(true)
    try { await api.updateConfig({ atlasEngineRouterModel: model } as any) }
    catch { /* best-effort */ }
    finally { setRouterModelSaving(false) }
  }

  const handleRouterStart = async () => {
    setRouterActing(true); setError(null)
    try { setRouterStatus(await api.engineRouterStart(routerModel || undefined)) }
    catch (e) { setError(e instanceof Error ? e.message : 'Failed to start router.') }
    finally { setRouterActing(false) }
  }

  const handleRouterStop = async () => {
    setRouterActing(true); setError(null)
    try { setRouterStatus(await api.engineRouterStop()) }
    catch (e) { setError(e instanceof Error ? e.message : 'Failed to stop router.') }
    finally { setRouterActing(false) }
  }

  const handleStart = async (modelName: string) => {
    setActing(true); setError(null)
    try { setStatus(await api.engineStart(modelName, undefined, ctxSize)) }
    catch (e) { setError(e instanceof Error ? e.message : 'Failed to load model.') }
    finally { setActing(false) }
  }

  const handleStop = async () => {
    setActing(true); setError(null)
    try { setStatus(await api.engineStop()) }
    catch (e) { setError(e instanceof Error ? e.message : 'Failed to eject model.') }
    finally { setActing(false) }
  }

  const handleDelete = async (name: string) => {
    if (!confirm(`Delete ${name}?`)) return
    setError(null)
    try { setModels(await api.engineDeleteModel(name)) }
    catch (e) { setError(e instanceof Error ? e.message : 'Failed to delete model.') }
  }

  const handlePresetChange = (preset: string) => {
    setDlPreset(preset)
    const found = ALL_STARTER_MODELS.find(m => m.filename === preset)
    if (found) { setDlURL(found.url); setDlFilename(found.filename) }
  }

  const handleDownload = async () => {
    if (!dlURL || !dlFilename) return
    setDownload({ filename: dlFilename, downloaded: 0, total: 0, percent: 0, done: false, error: null })
    setError(null)

    const controller = new AbortController()
    dlAbortRef.current = () => controller.abort()

    try {
      const resp = await fetch(`${api.engineDownloadBaseURL()}/engine/models/download`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ url: dlURL, filename: dlFilename }),
        signal: controller.signal,
      })
      if (!resp.ok || !resp.body) throw new Error(`Server returned ${resp.status}`)

      const reader = resp.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split('\n\n')
        buffer = parts.pop() ?? ''
        for (const part of parts) {
          const eventLine = part.split('\n').find(l => l.startsWith('event:'))
          const dataLine  = part.split('\n').find(l => l.startsWith('data:'))
          if (!eventLine || !dataLine) continue
          const event = eventLine.slice(7).trim()
          const data  = JSON.parse(dataLine.slice(5).trim())
          if (event === 'progress') {
            setDownload(prev => prev ? { ...prev, ...data } : null)
          } else if (event === 'done') {
            setDownload(prev => prev ? { ...prev, done: true, percent: 100 } : null)
            if (data.models) setModels(data.models)
            setDlURL(''); setDlFilename(''); setDlPreset('')
            dlAbortRef.current = null
          } else if (event === 'error') {
            setDownload(prev => prev ? { ...prev, error: data.message } : null)
            dlAbortRef.current = null
          }
        }
      }
    } catch (e) {
      if ((e as Error).name !== 'AbortError') {
        setDownload(prev => prev ? { ...prev, error: e instanceof Error ? e.message : 'Download failed' } : null)
      }
      dlAbortRef.current = null
    }
  }

  const handleCancelDownload = () => {
    dlAbortRef.current?.()
    setDownload(null)
  }

  const handleUpdate = async (version: string) => {
    setUpdate({ version, downloaded: 0, total: 0, percent: 0, done: false, error: null })
    setError(null)

    const controller = new AbortController()
    updateAbortRef.current = () => controller.abort()

    try {
      const resp = await fetch(`${api.engineUpdateBaseURL()}/engine/update`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ version }),
        signal: controller.signal,
      })
      if (!resp.ok || !resp.body) throw new Error(`Server returned ${resp.status}`)

      const reader = resp.body.getReader()
      const decoder = new TextDecoder()
      let buffer = ''
      while (true) {
        const { done, value } = await reader.read()
        if (done) break
        buffer += decoder.decode(value, { stream: true })
        const parts = buffer.split('\n\n')
        buffer = parts.pop() ?? ''
        for (const part of parts) {
          const eventLine = part.split('\n').find(l => l.startsWith('event:'))
          const dataLine  = part.split('\n').find(l => l.startsWith('data:'))
          if (!eventLine || !dataLine) continue
          const event = eventLine.slice(7).trim()
          const data  = JSON.parse(dataLine.slice(5).trim())
          if (event === 'progress') {
            setUpdate(prev => prev ? { ...prev, ...data } : null)
          } else if (event === 'done') {
            setUpdate(prev => prev ? { ...prev, done: true, percent: 100 } : null)
            if (data.status) setStatus(data.status)
            updateAbortRef.current = null
          } else if (event === 'error') {
            setUpdate(prev => prev ? { ...prev, error: data.message } : null)
            updateAbortRef.current = null
          }
        }
      }
    } catch (e) {
      if ((e as Error).name !== 'AbortError') {
        setUpdate(prev => prev ? { ...prev, error: e instanceof Error ? e.message : 'Update failed' } : null)
      }
      updateAbortRef.current = null
    }
  }

  const handleCancelUpdate = () => {
    updateAbortRef.current?.()
    setUpdate(null)
  }

  const isRunning       = status?.running ?? false
  const binaryReady     = status?.binaryReady ?? false
  const buildVersion    = status?.buildVersion ?? ''
  const isDownloading   = !!download && !download.done && !download.error
  const isUpdating      = !!update && !update.done && !update.error
  const isUpToDate      = buildVersion === PINNED_VERSION

  if (loading) {
    return (
      <div class="screen">
        <PageHeader title="Engine LM" subtitle="Built-in local inference — no external tools required." />
        <div style={{ display: 'flex', justifyContent: 'center', padding: '48px' }}>
          <span class="spinner" />
        </div>
      </div>
    )
  }

  return (
    <div class="screen">
      <PageHeader title="Engine LM" subtitle="Built-in local inference — no external tools required." />

      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      {/* ── Models ─────────────────────────────────────────────────────────── */}
      <div>
        {/* Section header: label + count */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 10 }}>
          <div class="section-label" style={{ margin: 0, flex: 1 }}>Models</div>
          {models.length > 0 && (
            <span class="surface-chip">{models.length} {models.length === 1 ? 'model' : 'models'}</span>
          )}
        </div>

        {!binaryReady && (
          <div class="banner banner-warn" style={{ marginBottom: 12, borderRadius: '6px' }}>
            <span class="banner-message">
              llama-server binary not found. Run <code>make install</code> or <code>make download-engine</code>.
            </span>
          </div>
        )}

        {/* Unified models card */}
        <div class="card">
          {models.length === 0 ? (
            <div style={{ padding: '40px 20px', textAlign: 'center', color: 'var(--theme-text-muted)', fontSize: 13 }}>
              No models downloaded yet — use the section below to get started.
            </div>
          ) : (
            models.map(m => {
              const isActive = isRunning && status?.loadedModel === m.name
              const { display, quant } = parseModelInfo(m.name)
              return (
                <div key={m.name} class="settings-row">
                  {/* Model info */}
                  <div class="settings-label-col">
                    <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                      <span class="settings-label" style={{ fontFamily: 'var(--font-mono)', fontSize: 13 }}>
                        {display}
                      </span>
                      {quant && (
                        <span class="badge badge-gray" style={{ fontSize: 11, padding: '1px 6px' }}>{quant}</span>
                      )}
                      {isActive && (
                        <span class="badge badge-green" style={{ fontSize: 11, padding: '1px 6px' }}>Active</span>
                      )}
                      {isActive && status?.port && (
                        <span class="badge badge-gray" style={{ fontSize: 11, padding: '1px 6px' }}>port {status.port}</span>
                      )}
                      {isActive && status?.lastTPS != null && status.lastTPS > 0 && (
                        <span class="badge badge-gray" style={{ fontSize: 11, padding: '1px 6px' }}>{status.lastTPS.toFixed(1)} tok/s</span>
                      )}
                      {isActive && status?.contextTokens != null && status.contextTokens > 0 && (
                        <span class="badge badge-gray" style={{ fontSize: 11, padding: '1px 6px' }}>{status.contextTokens.toLocaleString()} ctx</span>
                      )}
                    </div>
                    <div class="settings-sublabel" style={{ marginTop: 3 }}>{m.sizeHuman}</div>
                    {isActive && status?.lastError && (
                      <div style={{ fontSize: 11.5, color: 'var(--theme-text-danger, #e05252)', marginTop: 3 }}>
                        {status.lastError}
                      </div>
                    )}
                  </div>

                  {/* Actions */}
                  <div class="settings-field" style={{ gap: 6, flexShrink: 0 }}>
                    {isActive ? (
                      <button class="btn btn-sm" onClick={handleStop} disabled={acting}>
                        {acting ? '…' : 'Eject'}
                      </button>
                    ) : (
                      <button
                        class="btn btn-sm btn-primary"
                        onClick={() => handleStart(m.name)}
                        disabled={acting || !binaryReady}
                      >
                        {acting ? '…' : 'Load'}
                      </button>
                    )}
                    <button
                      class="btn btn-sm btn-danger"
                      onClick={() => handleDelete(m.name)}
                      disabled={acting || isActive}
                      title={isActive ? 'Eject the model before deleting' : `Delete ${m.name}`}
                    >
                      Delete
                    </button>
                  </div>
                </div>
              )
            })
          )}
        </div>
      </div>

      {/* ── Tool Router ────────────────────────────────────────────────────── */}
      <div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 10 }}>
          <div class="section-label" style={{ margin: 0, flex: 1 }}>Tool Router</div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <span class={`status-dot ${routerStatus?.running ? 'ready' : 'stopped'}`} />
            <span style={{ fontSize: 12.5, color: 'var(--theme-text-secondary)' }}>
              {routerStatus?.running ? `Running · port ${routerStatus.port}` : 'Stopped'}
            </span>
            {routerStatus?.running && (
              <button class="btn btn-sm" onClick={handleRouterStop} disabled={routerActing} style={{ marginLeft: 2 }}>
                {routerActing ? '…' : 'Stop'}
              </button>
            )}
          </div>
        </div>
        <div class="card">
          <div class="settings-row" style={{ borderBottom: 'none' }}>
            <div class="settings-label-col">
              <div class="settings-label">Router model</div>
              <div class="settings-sublabel">
                Small model used when Tool Selection is set to <strong>LLM</strong>.
                Select a downloaded model — Gemma 4 2B recommended.
                Auto-starts when a chat turn needs it.
              </div>
            </div>
            <div class="settings-field" style={{ gap: 8, flexShrink: 0 }}>
              <select
                class="input"
                style={{ flex: '0 0 220px' }}
                value={routerModel}
                onChange={e => handleRouterModelChange((e.target as HTMLSelectElement).value)}
                disabled={routerModelSaving}
              >
                <option value="">— Select router model —</option>
                {models.map(m => {
                  const { display, quant } = parseModelInfo(m.name)
                  return (
                    <option key={m.name} value={m.name}>
                      {display}{quant ? ` · ${quant}` : ''} · {m.sizeHuman}
                    </option>
                  )
                })}
              </select>
              {!routerStatus?.running ? (
                <button
                  class="btn btn-sm btn-primary"
                  onClick={handleRouterStart}
                  disabled={routerActing || !routerModel || !binaryReady}
                >
                  {routerActing ? '…' : 'Start'}
                </button>
              ) : null}
            </div>
          </div>
        </div>
      </div>

      {/* ── Download a model ───────────────────────────────────────────────── */}
      <div>
        <div class="section-label">Download Model</div>
        <div class="card">

          <div class="settings-row">
            <div class="settings-label-col">
              <div class="settings-label">Starter Models</div>
              <div class="settings-sublabel">Curated GGUF models that run well on Apple Silicon</div>
            </div>
            <div class="settings-field" style={{ flex: '0 0 300px' }}>
              <select
                class="input"
                value={dlPreset}
                onChange={e => handlePresetChange((e.target as HTMLSelectElement).value)}
                disabled={isDownloading}
              >
                <option value="">— Choose a model or enter URL below —</option>
                <optgroup label="Primary Models">
                  {PRIMARY_MODELS.map(m => (
                    <option key={m.filename} value={m.filename}>{m.label}</option>
                  ))}
                </optgroup>
                <optgroup label="Router Models">
                  {ROUTER_MODELS.map(m => (
                    <option key={m.filename} value={m.filename}>{m.label}</option>
                  ))}
                </optgroup>
              </select>
            </div>
          </div>

          <div class="settings-row" style={{ borderBottom: 'none' }}>
            <div class="settings-label-col">
              <div class="settings-label">Download URL</div>
              <div class="settings-sublabel">Direct link to any <code>.gguf</code> file</div>
            </div>
            <div class="settings-field" style={{ flex: '0 0 300px' }}>
              <input
                class="input"
                type="url"
                placeholder="https://huggingface.co/…/model.gguf"
                value={dlURL}
                onInput={e => {
                  const url = (e.target as HTMLInputElement).value
                  setDlURL(url)
                  const name = url.split('/').pop()?.split('?')[0] ?? ''
                  if (name) setDlFilename(name)
                }}
                disabled={isDownloading}
              />
            </div>
          </div>

          {/* Footer: progress + actions */}
          <div style={{ borderTop: '1px solid var(--theme-border-subtle)', padding: '14px 20px', display: 'flex', flexDirection: 'column', gap: 12 }}>

            {isDownloading && (
              <div>
                <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12.5, color: 'var(--theme-text-secondary)', marginBottom: 7 }}>
                  <span>Downloading <code style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}>{download!.filename}</code>…</span>
                  <span style={{ color: 'var(--theme-text-muted)' }}>
                    {download!.total > 0
                      ? `${(download!.downloaded / 1e9).toFixed(2)} / ${(download!.total / 1e9).toFixed(2)} GB · ${download!.percent.toFixed(1)}%`
                      : `${(download!.downloaded / 1e6).toFixed(1)} MB`}
                  </span>
                </div>
                <div style={{ height: 4, background: 'var(--theme-border-strong)', borderRadius: 2, overflow: 'hidden' }}>
                  <div style={{ height: '100%', width: `${download!.percent}%`, background: 'var(--theme-accent-fill)', borderRadius: 2, transition: 'width 0.25s' }} />
                </div>
              </div>
            )}

            {download?.done && (
              <div class="banner banner-success" style={{ borderRadius: 6 }}>
                <span class="banner-message">✓ {download.filename} downloaded successfully.</span>
              </div>
            )}
            {download?.error && (
              <div class="banner banner-error" style={{ borderRadius: 6 }}>
                <span class="banner-message">Download failed: {download.error}</span>
              </div>
            )}

            <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
              {(download?.done || download?.error) && (
                <button class="btn btn-sm btn-ghost" onClick={() => setDownload(null)}>Dismiss</button>
              )}
              {isDownloading ? (
                <button class="btn btn-sm" onClick={handleCancelDownload}>Cancel</button>
              ) : (
                <button
                  class="btn btn-sm btn-primary"
                  onClick={handleDownload}
                  disabled={!dlURL}
                >
                  Download
                </button>
              )}
            </div>

          </div>
        </div>
      </div>

      {/* ── Engine info card ───────────────────────────────────────────────── */}
      <div>
        <div class="section-label">Engine</div>
        <div class="card">

          {/* Context window size */}
          <div class="settings-row">
            <div class="settings-label-col">
              <div class="settings-label">Context size</div>
              <div class="settings-sublabel">
                KV-cache token limit passed to llama-server via --ctx-size.
                Larger values use more VRAM/RAM. Restart the model after changing.
              </div>
            </div>
            <div class="settings-field" style={{ flex: '0 0 160px' }}>
              <select
                class="input"
                value={ctxSize}
                onChange={e => handleCtxSizeChange(parseInt((e.target as HTMLSelectElement).value))}
                disabled={ctxSizeSaving}
              >
                {CTX_SIZE_OPTIONS.map(n => (
                  <option key={n} value={n}>
                    {n >= 1024 ? `${n / 1024}K tokens` : `${n} tokens`}
                  </option>
                ))}
              </select>
            </div>
          </div>

          {/* KV cache quantisation */}
          <div class="settings-row">
            <div class="settings-label-col">
              <div class="settings-label">KV cache quantisation</div>
              <div class="settings-sublabel">
                Precision for the attention key/value cache during inference (-ctk/-ctv).
                Use q4_0 with Q4 models — activations from 4-bit weights carry no extra
                precision worth storing at higher quality. Restart the model after changing.
              </div>
            </div>
            <div class="settings-field" style={{ flex: '0 0 220px' }}>
              <select
                class="input"
                value={kvQuant}
                onChange={e => handleKvQuantChange((e.target as HTMLSelectElement).value)}
                disabled={kvQuantSaving}
              >
                <option value="q4_0">q4_0 — 4-bit (Q4 models)</option>
                <option value="q8_0">q8_0 — 8-bit (Q8 / f16 models)</option>
                <option value="f16">f16 — full precision</option>
              </select>
            </div>
          </div>

          {/* llama-server version + update — merged row */}
          <div class="settings-row" style={{ borderBottom: 'none' }}>
            <div class="settings-label-col">
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <div class="settings-label">llama-server</div>
                {binaryReady && buildVersion && (
                  <span
                    class={`badge ${isUpToDate ? 'badge-green' : 'badge-yellow'}`}
                    style={{ fontSize: 11, padding: '2px 8px' }}
                  >
                    v. {buildVersion}
                  </span>
                )}
                {!binaryReady && (
                  <span class="badge badge-red" style={{ fontSize: 11, padding: '2px 8px' }}>Missing</span>
                )}
              </div>
              <div class="settings-sublabel">
                {binaryReady
                  ? isUpToDate
                    ? `Up to date — pinned release ${PINNED_VERSION}.`
                    : `Update available — pinned release: ${PINNED_VERSION}`
                  : 'Not installed — run make install or make download-engine'}
              </div>
            </div>
            <div class="settings-field" style={{ flexShrink: 0 }}>
              {isUpdating ? (
                <button class="btn btn-sm" onClick={handleCancelUpdate}>Cancel</button>
              ) : (
                <button
                  class="btn btn-sm btn-primary"
                  onClick={() => handleUpdate(PINNED_VERSION)}
                  disabled={isUpdating}
                >
                  {isUpToDate ? 'Reinstall' : 'Update'}
                </button>
              )}
            </div>
          </div>

          {/* Update progress */}
          {(isUpdating || update?.done || update?.error) && (
            <div style={{ borderTop: '1px solid var(--theme-border-subtle)', padding: '14px 20px', display: 'flex', flexDirection: 'column', gap: 10 }}>
              {isUpdating && (
                <div>
                  <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 12.5, color: 'var(--theme-text-secondary)', marginBottom: 7 }}>
                    <span>Downloading llama-server <code style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }}>{update!.version}</code>…</span>
                    <span style={{ color: 'var(--theme-text-muted)' }}>
                      {update!.total > 0
                        ? `${(update!.downloaded / 1e6).toFixed(1)} / ${(update!.total / 1e6).toFixed(1)} MB · ${update!.percent.toFixed(1)}%`
                        : `${(update!.downloaded / 1e6).toFixed(1)} MB`}
                    </span>
                  </div>
                  <div style={{ height: 4, background: 'var(--theme-border-strong)', borderRadius: 2, overflow: 'hidden' }}>
                    <div style={{ height: '100%', width: `${update!.percent}%`, background: 'var(--theme-accent-fill)', borderRadius: 2, transition: 'width 0.25s' }} />
                  </div>
                </div>
              )}
              {update?.done && (
                <div class="banner banner-success" style={{ borderRadius: 6 }}>
                  <span class="banner-message">✓ Engine updated to {update.version}.</span>
                </div>
              )}
              {update?.error && (
                <div class="banner banner-error" style={{ borderRadius: 6 }}>
                  <span class="banner-message">Update failed: {update.error}</span>
                </div>
              )}
              {(update?.done || update?.error) && (
                <button class="btn btn-sm btn-ghost" style={{ alignSelf: 'flex-start' }} onClick={() => setUpdate(null)}>Dismiss</button>
              )}
            </div>
          )}

        </div>
      </div>

    </div>
  )
}
