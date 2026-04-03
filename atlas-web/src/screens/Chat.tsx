import { useState, useEffect, useRef, useCallback } from 'preact/hooks'
import type { JSX } from 'preact/jsx-runtime'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import { api, MessageAttachment, LinkPreview, ConversationSummary, ConversationDetail } from '../api/client'
import { PageHeader } from '../components/PageHeader'
import { ErrorBanner } from '../components/ErrorBanner'
import { formatAtlasModelName } from '../modelName'

// Configure marked once — GFM tables, auto line-breaks, external links
marked.use({
  gfm: true,
  breaks: true,
  renderer: {
    link({ href, title, text }: { href: string; title?: string | null; text: string }) {
      const safeHref = encodeURI(href ?? '')
      const titleAttr = title ? ` title="${title.replace(/"/g, '&quot;')}"` : ''
      return `<a href="${safeHref}"${titleAttr} target="_blank" rel="noopener noreferrer" class="chat-link">${text}</a>`
    }
  }
})

// ── Types ─────────────────────────────────────────────────────────────────────

interface Message {
  id: string
  role: 'user' | 'assistant'
  content: string
  isTyping?: boolean
  /** URL → preview map so each card can be anchored to its source URL. */
  linkPreviews?: Record<string, LinkPreview>
}

type ChatProvider = 'openai' | 'anthropic' | 'gemini' | 'lm_studio' | 'ollama' | 'atlas_engine'

const STORAGE_ID_KEY  = 'atlasConversationID'
const STORAGE_MSG_KEY = 'atlasChatMessages'

function selectedModelForProvider(config: {
  selectedOpenAIPrimaryModel?: string
  selectedAnthropicModel?: string
  selectedGeminiModel?: string
  selectedLMStudioModel?: string
  selectedOllamaModel?: string
  selectedAtlasEngineModel?: string
}, provider: string): string | null {
  switch (provider) {
    case 'openai':
      return config.selectedOpenAIPrimaryModel?.trim() || null
    case 'anthropic':
      return config.selectedAnthropicModel?.trim() || null
    case 'gemini':
      return config.selectedGeminiModel?.trim() || null
    case 'lm_studio':
      return config.selectedLMStudioModel?.trim() || null
    case 'ollama':
      return config.selectedOllamaModel?.trim() || null
    case 'atlas_engine':
      return config.selectedAtlasEngineModel?.trim() || null
    default:
      return null
  }
}

// ── Utilities ─────────────────────────────────────────────────────────────────

/** UUID v4 generator that works in both secure (HTTPS) and non-secure (HTTP) contexts.
 *  `uuid()` is only available in secure contexts (HTTPS / localhost).
 *  On plain HTTP (LAN access), we fall back to `crypto.getRandomValues()` which
 *  is available everywhere, including HTTP on Safari and Android browsers. */
function uuid(): string {
  if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') {
    return crypto.randomUUID()
  }
  // RFC 4122 v4 UUID via getRandomValues — works on HTTP
  const bytes = new Uint8Array(16)
  crypto.getRandomValues(bytes)
  bytes[6] = (bytes[6] & 0x0f) | 0x40 // version 4
  bytes[8] = (bytes[8] & 0x3f) | 0x80 // variant 10
  const hex = Array.from(bytes).map(b => b.toString(16).padStart(2, '0'))
  return `${hex.slice(0,4).join('')}-${hex.slice(4,6).join('')}-${hex.slice(6,8).join('')}-${hex.slice(8,10).join('')}-${hex.slice(10,16).join('')}`
}

function getConversationID(): string {
  let id = localStorage.getItem(STORAGE_ID_KEY)
  if (!id) { id = uuid(); localStorage.setItem(STORAGE_ID_KEY, id) }
  return id
}

function loadMessages(): Message[] {
  try {
    const raw = localStorage.getItem(STORAGE_MSG_KEY)
    if (!raw) return []
    return (JSON.parse(raw) as Message[]).map(m => ({ ...m, isTyping: false }))
  } catch { return [] }
}

function saveMessages(msgs: Message[]) {
  try {
    const toSave = msgs
      .filter(m => m.content.length > 0 && !m.isTyping)
      .map(({ id, role, content }) => ({ id, role, content }))
    localStorage.setItem(STORAGE_MSG_KEY, JSON.stringify(toSave))
  } catch {
    // QuotaExceededError — storage full; skip silently
  }
}

/**
 * Maps a tool name to a calm, human-readable status phrase.
 * The backend already humanizes most names via AgentOrchestrator.humanReadableName;
 * this is a frontend safety net for any raw IDs that slip through.
 */
function humanizeToolName(raw: string): string {
  if (!raw) return 'Working on it…'
  // Already humanized (contains spaces or ends with ellipsis) — pass through
  if (raw.includes(' ') || raw.endsWith('…')) return raw
  if (raw.startsWith('browser.'))                     return 'Browsing…'
  if (raw.startsWith('weather.'))                     return 'Checking the weather…'
  if (raw.startsWith('websearch.'))                   return 'Searching the web…'
  if (raw.startsWith('web.search'))                   return 'Searching the web…'
  if (raw.startsWith('web.'))                         return 'Looking this up…'
  if (raw.startsWith('fs.'))                          return 'Reading files…'
  if (raw.startsWith('file.'))                        return 'Reading files…'
  if (raw.startsWith('terminal.'))                    return 'Running a command…'
  if (raw.startsWith('finance.'))                     return 'Checking the markets…'
  if (raw.startsWith('vault.'))                       return 'Checking credentials…'
  if (raw.startsWith('diary.'))                       return 'Writing to memory…'
  if (raw.startsWith('forge.orchestration.propose'))  return 'Drafting a new skill…'
  if (raw.startsWith('forge.orchestration.plan'))     return 'Planning this out…'
  if (raw.startsWith('forge.orchestration.review'))   return 'Reviewing the plan…'
  if (raw.startsWith('forge.orchestration.validate')) return 'Verifying the details…'
  if (raw.startsWith('forge.'))                       return 'Building that for you…'
  if (raw.startsWith('dashboard.'))                   return 'Updating your dashboard…'
  if (raw.startsWith('system.'))                      return 'Running that now…'
  if (raw.startsWith('applescript.'))                 return 'Working in your apps…'
  if (raw.startsWith('gremlin.'))                     return 'Managing automations…'
  if (raw.startsWith('gremlins.'))                    return 'Managing automations…'
  if (raw.startsWith('image.'))                       return 'Generating an image…'
  if (raw.startsWith('vision.'))                      return 'Analyzing the image…'
  if (raw.startsWith('atlas.'))                       return 'Checking Atlas…'
  if (raw.startsWith('info.'))                        return 'Checking that…'
  return 'Working on it…'
}

// ── URL detection & link previews ──────────────────────────────────────────────

const URL_RE = /https?:\/\/[^\s<>"'()[\]{}]+[^\s<>"'()[\]{}.,!?;:]/g

/**
 * Extracts unique http/https URLs from text (max 3).
 */
function extractURLs(text: string): string[] {
  return Array.from(new Set(text.match(URL_RE) ?? [])).slice(0, 3)
}

/**
 * Renders assistant message content as markdown.
 * - Normalizes mixed HTML (e.g. <br> tags from local models) before parsing
 * - Parses with marked (GFM: tables, autolinks, fenced code)
 * - Sanitizes with DOMPurify before injection
 * - Appends LinkPreviewCards for any URLs that have resolved previews
 */
function renderMessageContent(
  content: string,
  linkPreviews: Record<string, LinkPreview> | undefined
): JSX.Element {
  const normalized = content.replace(/<br\s*\/?>/gi, '\n')
  const rawHtml = marked.parse(normalized) as string
  const safeHtml = DOMPurify.sanitize(rawHtml, {
    ADD_ATTR: ['target', 'rel', 'class'],
    ALLOWED_TAGS: [
      'p', 'br', 'strong', 'b', 'em', 'i', 'code', 'pre', 'a',
      'ul', 'ol', 'li', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6',
      'table', 'thead', 'tbody', 'tr', 'th', 'td',
      'blockquote', 'hr', 's', 'del', 'span'
    ]
  })

  const previews = linkPreviews ?? {}
  const previewCards = Object.entries(previews).map(([url, preview]) => (
    <div key={`pv${url}`} class="link-preview-anchor">
      <LinkPreviewCard preview={preview} />
    </div>
  ))

  return (
    <>
      <div class="message-markdown" dangerouslySetInnerHTML={{ __html: safeHtml }} />
      {previewCards}
    </>
  )
}

/**
 * Compact, clickable link preview card anchored below its source URL.
 */
const LinkPreviewCard = ({ preview }: { preview: LinkPreview }) => {
  const domain = preview.domain
    ?? (() => { try { return new URL(preview.url).hostname.replace(/^www\./, '') } catch { return preview.url } })()

  return (
    <a
      href={preview.url}
      target="_blank"
      rel="noopener noreferrer"
      class="link-preview-card"
      onClick={(e) => e.stopPropagation()}
    >
      {preview.imageURL && (
        <img
          src={preview.imageURL}
          class="link-preview-img"
          alt=""
          loading="lazy"
          onError={(e) => { (e.target as HTMLImageElement).style.display = 'none' }}
        />
      )}
      <div class="link-preview-body">
        <span class="link-preview-domain">{domain}</span>
        {preview.title && <span class="link-preview-title">{preview.title}</span>}
        {preview.description && <span class="link-preview-desc">{preview.description}</span>}
      </div>
    </a>
  )
}

// ── Icon components ────────────────────────────────────────────────────────────

const SendIcon = () => (
  <svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <line x1="14" y1="2" x2="7" y2="9" />
    <polygon points="14,2 9,14 7,9 2,7" fill="currentColor" stroke="none" />
  </svg>
)

const MicIcon = () => (
  <svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
    <rect x="5" y="1" width="6" height="9" rx="3" />
    <path d="M2 8a6 6 0 0012 0" />
    <line x1="8" y1="14" x2="8" y2="16" />
    <line x1="5.5" y1="16" x2="10.5" y2="16" />
  </svg>
)

const AvatarGlyph = () => (
  <svg width="14" height="14" viewBox="0 0 16 16" fill="currentColor">
    <circle cx="8" cy="5.5" r="3" />
    <path d="M2.5 15c0-3 2.5-5.5 5.5-5.5S13.5 12 13.5 15" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" fill="none" />
  </svg>
)

/**
 * Animated typing dots — the only loading indicator in chat.
 * `working` softens the animation while tools run in the background,
 * maintaining presence without demanding attention.
 */
const TypingDots = ({ working }: { working?: boolean }) => (
  <span class={`typing-dots${working ? ' working' : ''}`}>
    <span /><span /><span />
  </span>
)

// ── Chat component ─────────────────────────────────────────────────────────────

export function Chat({ onNavigateHistory }: { onNavigateHistory?: () => void } = {}) {
  const [messages, setMessages]               = useState<Message[]>(loadMessages)
  const [input, setInput]                     = useState('')
  const [sending, setSending]                 = useState(false)
  const [approvalBanner, setApprovalBanner]   = useState(false)
  const [error, setError]                     = useState<string | null>(null)
  const [attachments, setAttachments]         = useState<MessageAttachment[]>([])
  const [agentName, setAgentName]             = useState('Atlas')
  const [activeProvider, setActiveProvider]   = useState<ChatProvider>('openai')
  const [modelByProvider, setModelByProvider] = useState<Record<ChatProvider, string>>({
    openai:    '',
    anthropic: '',
    gemini:    '',
    lm_studio:    '',
    ollama:       '',
    atlas_engine: '',
  })

  // History search state
  const [historyOpen, setHistoryOpen]           = useState(false)
  const [historyQuery, setHistoryQuery]         = useState('')
  const [historySummaries, setHistorySummaries] = useState<ConversationSummary[]>([])
  const [historyLoading, setHistoryLoading]     = useState(false)
  const historySearchRef                        = useRef<HTMLInputElement>(null)
  const historyDebounceRef                      = useRef<ReturnType<typeof setTimeout> | null>(null)
  const historyContainerRef                     = useRef<HTMLDivElement>(null)

  // Presence state — replaces spinner + tool banner entirely
  const [presenceText, setPresenceText]       = useState('Thinking…')
  const [presenceVisible, setPresenceVisible] = useState(false)
  const [presenceWorking, setPresenceWorking] = useState(false)
  const presenceTimer = useRef<ReturnType<typeof setTimeout> | null>(null)

  // activeMsgId: tracks which assistant bubble is the active one this turn.
  // Used to keep typing dots visible even after assistant_done fires (tool-only turns
  // produce no text, so assistant_done fires before tools run, yet the turn continues).
  const activeMsgId = useRef<string | null>(null)

  const bottomRef      = useRef<HTMLDivElement>(null)
  const esRef          = useRef<EventSource | null>(null)
  const textareaRef    = useRef<HTMLTextAreaElement>(null)
  const fileInputRef   = useRef<HTMLInputElement>(null)
  const conversationID = useRef<string>(getConversationID())
  const isInitialMount = useRef(true)

  useEffect(() => {
    saveMessages(messages)
    bottomRef.current?.scrollIntoView({ behavior: isInitialMount.current ? 'instant' : 'smooth' })
    isInitialMount.current = false
  }, [messages])

  useEffect(() => {
    return () => {
      esRef.current?.close()
      if (presenceTimer.current) clearTimeout(presenceTimer.current)
    }
  }, [])

  const resolveModelLabel = useCallback(async (provider: ChatProvider, selectedModel?: string | null) => {
    const explicitModel = selectedModel?.trim()
    if (explicitModel) {
      setModelByProvider((current) => ({ ...current, [provider]: explicitModel }))
      return
    }

    try {
      const info = await api.modelsForProvider(provider)
      const resolvedPrimary = info.primaryModel?.trim()
      if (resolvedPrimary) {
        setModelByProvider((current) => ({ ...current, [provider]: resolvedPrimary }))
      }
    } catch {
      // Leave the current value alone if the provider cannot be queried right now.
    }
  }, [])

  useEffect(() => {
    api.config().then(async (s) => {
      if (s.personaName) setAgentName(s.personaName)
      if (s.activeAIProvider) setActiveProvider(s.activeAIProvider as ChatProvider)
      setModelByProvider({
        openai:    s.selectedOpenAIPrimaryModel?.trim() || '',
        anthropic: s.selectedAnthropicModel?.trim() || '',
        gemini:    s.selectedGeminiModel?.trim() || '',
        lm_studio:    s.selectedLMStudioModel?.trim() || '',
        ollama:       s.selectedOllamaModel?.trim() || '',
        atlas_engine: s.selectedAtlasEngineModel?.trim() || '',
      })
      const provider = (s.activeAIProvider || 'openai') as ChatProvider
      await resolveModelLabel(provider, selectedModelForProvider(s, provider))
    }).catch(() => {})
  }, [resolveModelLabel])

  // Click-outside handler for search dropdown
  useEffect(() => {
    if (!historyOpen) return
    const handler = (e: MouseEvent) => {
      if (historyContainerRef.current && !historyContainerRef.current.contains(e.target as Node)) {
        setHistoryOpen(false)
        setHistoryQuery('')
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [historyOpen])

  // Load conversation list whenever search opens
  useEffect(() => {
    if (!historyOpen) return
    setHistoryQuery('')
    setHistoryLoading(true)
    api.conversations(50, 0)
      .then(setHistorySummaries)
      .catch(() => setHistorySummaries([]))
      .finally(() => setHistoryLoading(false))
    setTimeout(() => historySearchRef.current?.focus(), 50)
  }, [historyOpen])

  // Debounced search
  useEffect(() => {
    if (!historyOpen) return
    if (historyDebounceRef.current) clearTimeout(historyDebounceRef.current)
    if (!historyQuery.trim()) {
      setHistoryLoading(true)
      api.conversations(50, 0)
        .then(setHistorySummaries)
        .catch(() => setHistorySummaries([]))
        .finally(() => setHistoryLoading(false))
      return
    }
    historyDebounceRef.current = setTimeout(() => {
      setHistoryLoading(true)
      api.searchConversations(historyQuery.trim())
        .then(setHistorySummaries)
        .catch(() => setHistorySummaries([]))
        .finally(() => setHistoryLoading(false))
    }, 280)
    return () => { if (historyDebounceRef.current) clearTimeout(historyDebounceRef.current) }
  }, [historyQuery, historyOpen])

  const resumeConversation = async (id: string) => {
    localStorage.setItem(STORAGE_ID_KEY, id)
    localStorage.removeItem(STORAGE_MSG_KEY)
    conversationID.current = id
    setError(null)
    setApprovalBanner(false)
    setAttachments([])
    hideStatus(0)
    setPresenceWorking(false)
    activeMsgId.current = null
    setHistoryOpen(false)
    setHistoryQuery('')
    try {
      const detail: ConversationDetail = await api.conversationDetail(id)
      const loaded: Message[] = detail.messages
        .filter(m => m.role === 'user' || m.role === 'assistant')
        .map(m => ({ id: m.id, role: m.role as 'user' | 'assistant', content: m.content }))
      setMessages(loaded)
    } catch (err) {
      setMessages([])
      setError(err instanceof Error ? err.message : 'Failed to load conversation.')
    }
  }

  const handleProviderChange = async (provider: ChatProvider) => {
    const previousProvider = activeProvider
    setActiveProvider(provider)
    await resolveModelLabel(provider, modelByProvider[provider])
    try {
      await api.updateConfig({ activeAIProvider: provider })
    } catch {
      setActiveProvider(previousProvider)
    }
  }

  // ── Presence helpers ─────────────────────────────────────────────────────────

  /**
   * Cross-fade between status phrases.
   * Fades out current text (180ms), swaps it, fades new text in.
   */
  const fadeStatus = (text: string) => {
    if (presenceTimer.current) clearTimeout(presenceTimer.current)
    setPresenceVisible(false)
    presenceTimer.current = setTimeout(() => {
      setPresenceText(text)
      setPresenceVisible(true)
      presenceTimer.current = null
    }, 180)
  }

  /** Schedule a fade-out of the status line after `delay` ms (0 = immediate). */
  const hideStatus = (delay = 0) => {
    if (presenceTimer.current) clearTimeout(presenceTimer.current)
    if (delay > 0) {
      presenceTimer.current = setTimeout(() => {
        setPresenceVisible(false)
        presenceTimer.current = null
      }, delay)
    } else {
      setPresenceVisible(false)
    }
  }

  // ── Link preview fetching ──────────────────────────────────────────────────────

  /**
   * Scans a finalized message for URLs, fetches previews in parallel,
   * and attaches any successful results back onto the message record.
   * Runs in the background — never blocks or throws.
   */
  const fetchAndAttachPreviews = async (msgId: string, content: string) => {
    const urls = extractURLs(content)
    if (!urls.length) return
    const results = await Promise.all(
      urls.map(url => api.fetchLinkPreview(url).catch(() => null))
    )
    // Build a URL → preview map so cards can be anchored to their source URL.
    // Only include results that have at least a title (domain-only isn't useful).
    const previewMap: Record<string, LinkPreview> = {}
    results.forEach((p, i) => {
      if (p && p.title) previewMap[urls[i]] = p
    })
    if (!Object.keys(previewMap).length) return
    setMessages(prev => prev.map(m => m.id === msgId ? { ...m, linkPreviews: previewMap } : m))
  }

  // ── File handling ─────────────────────────────────────────────────────────────

  const resizeTextarea = () => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    el.style.height = Math.min(el.scrollHeight, 140) + 'px'
  }

  const handleFileChange = (e: Event) => {
    const files = (e.target as HTMLInputElement).files
    if (!files || files.length === 0) return
    Array.from(files).forEach(file => {
      const reader = new FileReader()
      reader.onload = () => {
        const dataURL = reader.result as string
        const comma = dataURL.indexOf(',')
        const base64 = comma >= 0 ? dataURL.slice(comma + 1) : dataURL
        setAttachments(prev => [...prev, { filename: file.name, mimeType: file.type || 'application/octet-stream', data: base64 }])
      }
      reader.readAsDataURL(file)
    })
    if (fileInputRef.current) fileInputRef.current.value = ''
  }

  const removeAttachment = (index: number) => {
    setAttachments(prev => prev.filter((_, i) => i !== index))
  }

  // ── Send ───────────────────────────────────────────────────────────────────────

  const send = async () => {
    const text = input.trim()
    if ((!text && attachments.length === 0) || sending) return

    const pendingAttachments = [...attachments]
    setInput('')
    setAttachments([])
    if (textareaRef.current) textareaRef.current.style.height = 'auto'
    setError(null)
    setApprovalBanner(false)
    setSending(true)

    // Presence: surface immediately so there is never a blank state
    if (presenceTimer.current) clearTimeout(presenceTimer.current)
    setPresenceText('Thinking…')
    setPresenceVisible(true)
    setPresenceWorking(false)

    const userContent = pendingAttachments.length > 0
      ? `${text}${text ? '\n' : ''}📎 ${pendingAttachments.map(a => a.filename).join(', ')}`
      : text
    const userMsg: Message      = { id: uuid(), role: 'user',      content: userContent }
    const assistantMsg: Message = { id: uuid(), role: 'assistant', content: '', isTyping: true }
    activeMsgId.current = assistantMsg.id   // track the active bubble for presence dots
    setMessages(prev => [...prev, userMsg, assistantMsg])

    esRef.current?.close()
    const es = api.streamMessage(conversationID.current)
    esRef.current = es

    let accumulatedContent = ''
    let resumedMsgID: string | null = null
    let resumedContent = ''
    let awaitingResume = false
    let hasReceivedText = false   // tracks first text delta this turn

    es.onmessage = (evt) => {
      try {
        const data = JSON.parse(evt.data) as {
          type: string; content?: string; toolName?: string; message?: string; status?: string
        }
        switch (data.type) {

          // ── Streaming text events ──────────────────────────────────────────────
          case 'assistant_started':
            // A new model turn is beginning. For the resume path, create the
            // new message bubble now; for the first turn it already exists.
            if (awaitingResume && !resumedMsgID) {
              const newMsg: Message = { id: uuid(), role: 'assistant', content: '', isTyping: true }
              resumedMsgID = newMsg.id
              activeMsgId.current = newMsg.id   // update active bubble for presence dots
              setMessages(prev => [...prev, newMsg])
            }
            break

          case 'assistant_delta': {
            const delta = data.content ?? ''

            // Resume behavior: when text starts flowing, keep last status visible
            // briefly (~380ms) then fade it out — creates a natural "I finished
            // that, now I'm answering" continuity.
            if (!hasReceivedText) {
              hasReceivedText = true
              setPresenceWorking(false)
              hideStatus(380)
            }

            if (awaitingResume) {
              resumedContent += delta
              if (!resumedMsgID) {
                const newMsg: Message = { id: uuid(), role: 'assistant', content: resumedContent, isTyping: true }
                resumedMsgID = newMsg.id
                setMessages(prev => [...prev, newMsg])
              } else {
                setMessages(prev => prev.map(m => m.id === resumedMsgID ? { ...m, content: resumedContent, isTyping: true } : m))
              }
            } else {
              accumulatedContent += delta
              setMessages(prev => prev.map(m => m.id === assistantMsg.id ? { ...m, content: accumulatedContent, isTyping: true } : m))
            }
            break
          }

          case 'assistant_done':
            if (awaitingResume && resumedMsgID) {
              setMessages(prev => prev.map(m => m.id === resumedMsgID ? { ...m, isTyping: false } : m))
            } else {
              setMessages(prev => prev.map(m => m.id === assistantMsg.id ? { ...m, isTyping: false } : m))
            }
            break

          // ── Tool activity ──────────────────────────────────────────────────────
          case 'tool_started':
          case 'tool_call': {
            const label = humanizeToolName(data.toolName ?? '')
            setPresenceWorking(true)
            fadeStatus(label)
            break
          }

          case 'tool_finished':
            setPresenceWorking(false)
            // Linger the current status phrase for ~400ms so the user can read it,
            // then cross-fade back to "Thinking…" if text hasn't started yet.
            if (presenceTimer.current) clearTimeout(presenceTimer.current)
            presenceTimer.current = setTimeout(() => {
              if (!hasReceivedText) {
                setPresenceText('Thinking…')
                setPresenceVisible(true)
              }
              presenceTimer.current = null
            }, 400)
            break

          case 'tool_failed':
            setPresenceWorking(false)
            hideStatus(200)
            break

          // ── Approval ──────────────────────────────────────────────────────────
          case 'approval_required':
            setApprovalBanner(true)
            hideStatus(0)
            setPresenceWorking(false)
            break

          // ── Legacy token (single-shot full-text delivery) ──────────────────────
          case 'token':
            if (!hasReceivedText) {
              hasReceivedText = true
              setPresenceWorking(false)
              hideStatus(380)
            }
            if (awaitingResume) {
              resumedContent += data.content ?? ''
              if (!resumedMsgID) {
                const newMsg: Message = { id: uuid(), role: 'assistant', content: resumedContent, isTyping: true }
                resumedMsgID = newMsg.id
                setMessages(prev => [...prev, newMsg])
              } else {
                setMessages(prev => prev.map(m => m.id === resumedMsgID ? { ...m, content: resumedContent, isTyping: true } : m))
              }
            } else {
              accumulatedContent += data.content ?? ''
              setMessages(prev => prev.map(m => m.id === assistantMsg.id ? { ...m, content: accumulatedContent, isTyping: true } : m))
            }
            break

          // ── Conversation complete ──────────────────────────────────────────────
          case 'done':
            hideStatus(0)
            setPresenceWorking(false)
            activeMsgId.current = null
            if (data.status === 'waitingForApproval') {
              setMessages(prev => prev.map(m => m.id === assistantMsg.id ? { ...m, content: accumulatedContent || m.content, isTyping: false } : m))
              awaitingResume = true
              hasReceivedText = false   // reset for the resumed turn
            } else if (data.status === 'denied') {
              const targetID = resumedMsgID ?? assistantMsg.id
              setMessages(prev => prev.map(m => m.id === targetID ? { ...m, content: resumedContent || 'The action was denied.', isTyping: false } : m))
              setApprovalBanner(false); setSending(false); es.close()
            } else {
              // Last-resort frontend safety net: if the backend somehow produced no text
              // (backend fixes should have covered this), show a minimal fallback so the
              // bubble is never empty on a failed turn.
              const emptyFallback = (data.status === 'failed')
                ? "I ran into an issue with that. Let me know if you'd like to try again."
                : ''
              const finalID      = resumedMsgID ?? assistantMsg.id
              const finalContent = resumedMsgID
                ? (resumedContent || '')
                : (accumulatedContent || '')
              if (resumedMsgID) {
                setMessages(prev => prev.map(m => m.id === resumedMsgID ? { ...m, content: resumedContent || m.content || emptyFallback, isTyping: false } : m))
              } else {
                setMessages(prev => prev.map(m => m.id === assistantMsg.id ? { ...m, content: accumulatedContent || m.content || emptyFallback, isTyping: false } : m))
              }
              // Fetch link previews for assistant replies in the background
              if (data.status === 'completed' && finalContent) {
                fetchAndAttachPreviews(finalID, finalContent)
              }
              setApprovalBanner(false); setSending(false); es.close()
            }
            break

          case 'error':
            hideStatus(0)
            setPresenceWorking(false)
            activeMsgId.current = null
            setError(data.message ?? 'An error occurred.')
            const targetID = resumedMsgID ?? assistantMsg.id
            setMessages(prev => prev.map(m => m.id === targetID ? { ...m, content: resumedContent || accumulatedContent || 'Failed to get response.', isTyping: false } : m))
            setSending(false); es.close()
            break
        }
      } catch { /* ignore parse errors */ }
    }

    es.onerror = () => {
      hideStatus(0)
      setPresenceWorking(false)
      activeMsgId.current = null
      setSending(false)
      es.close()
    }

    try {
      await api.sendMessage(conversationID.current, text, pendingAttachments.length > 0 ? pendingAttachments : undefined)
    } catch (err) {
      hideStatus(0)
      setPresenceWorking(false)
      activeMsgId.current = null
      setError(err instanceof Error ? err.message : 'Failed to send message.')
      setMessages(prev => prev.map(m => m.id === assistantMsg.id ? { ...m, content: 'Failed to send message.', isTyping: false } : m))
      setSending(false); es.close()
    }
  }

  const handleKeyDown = (e: KeyboardEvent) => {
    if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() }
  }

  const newConversation = () => {
    const id = uuid()
    localStorage.setItem(STORAGE_ID_KEY, id)
    localStorage.removeItem(STORAGE_MSG_KEY)
    conversationID.current = id
    setMessages([])
    setError(null)
    setApprovalBanner(false)
    setAttachments([])
    hideStatus(0)
    setPresenceWorking(false)
    activeMsgId.current = null
  }

  // Derived — model name shown as header subtitle.
  // Atlas Engine model names are GGUF filenames; format them into readable labels.
  const activeModelRaw = modelByProvider[activeProvider]?.trim() || 'Loading…'
  const activeModel = activeProvider === 'atlas_engine'
    ? formatAtlasModelName(activeModelRaw)
    : activeModelRaw

  // ── Render ─────────────────────────────────────────────────────────────────────

  return (
    <div class="chat-screen">
      <PageHeader
        title="Chat"
        subtitle={activeModel ? `Model: ${activeModel}` : ''}
        actions={
          <>
            {/* Search — icon collapses to expanding search bar + dropdown */}
            <div ref={historyContainerRef} style={{ position: 'relative' }}>
              {historyOpen ? (
                <>
                  <div style={{ position: 'relative', display: 'flex', alignItems: 'center' }}>
                    <svg style={{ position: 'absolute', left: '9px', top: '50%', transform: 'translateY(-50%)', pointerEvents: 'none', color: 'var(--theme-text-muted)' }} width="13" height="13" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
                      <circle cx="6.5" cy="6.5" r="4.5" /><line x1="10" y1="10" x2="14" y2="14" />
                    </svg>
                    <input
                      ref={historySearchRef}
                      class="input"
                      type="text"
                      placeholder="Search conversations…"
                      value={historyQuery}
                      onInput={(e) => setHistoryQuery((e.target as HTMLInputElement).value)}
                      onKeyDown={(e) => { if (e.key === 'Escape') { setHistoryOpen(false); setHistoryQuery('') } }}
                      style={{ paddingLeft: '30px', paddingRight: '30px', fontSize: '13px', height: '30px', borderRadius: '10px', width: '220px' }}
                    />
                    <button
                      class="btn btn-ghost btn-icon"
                      style={{ position: 'absolute', right: '4px', minWidth: '20px', width: '20px', height: '20px', padding: 0, borderRadius: '6px' }}
                      onClick={() => { setHistoryOpen(false); setHistoryQuery('') }}
                      title="Close"
                    >
                      <svg width="9" height="9" viewBox="0 0 10 10" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round">
                        <line x1="1" y1="1" x2="9" y2="9" /><line x1="9" y1="1" x2="1" y2="9" />
                      </svg>
                    </button>
                  </div>

                  {/* Results dropdown */}
                  <div style={{
                    position: 'absolute', top: 'calc(100% + 6px)', right: 0, width: '100%',
                    background: 'var(--bg, var(--theme-surface-overlay))',
                    border: '1px solid var(--border, var(--theme-border-strong))',
                    borderRadius: '12px',
                    boxShadow: '0 8px 32px rgba(0,0,0,0.18)',
                    overflow: 'hidden',
                    zIndex: 300,
                    maxHeight: '340px',
                    display: 'flex',
                    flexDirection: 'column',
                  }}>
                    {historyLoading && (
                      <div style={{ padding: '20px', textAlign: 'center', color: 'var(--text-2)', fontSize: '13px' }}>Loading…</div>
                    )}
                    {!historyLoading && historySummaries.length === 0 && (
                      <div style={{ padding: '20px', textAlign: 'center', color: 'var(--text-2)', fontSize: '13px' }}>
                        {historyQuery ? `No results for "${historyQuery}"` : 'No conversations yet'}
                      </div>
                    )}
                    {!historyLoading && historySummaries.length > 0 && (
                      <div style={{ overflowY: 'auto' }}>
                        {historySummaries.map((s, i) => {
                          const diff = Date.now() - new Date(s.updatedAt).getTime()
                          const rel = diff < 60000 ? 'Just now' : diff < 3600000 ? `${Math.floor(diff / 60000)}m ago` : diff < 86400000 ? `${Math.floor(diff / 3600000)}h ago` : diff < 604800000 ? `${Math.floor(diff / 86400000)}d ago` : new Date(s.updatedAt).toLocaleDateString()
                          return (
                            <div
                              key={s.id}
                              onClick={() => resumeConversation(s.id)}
                              style={{
                                padding: '10px 14px', cursor: 'pointer',
                                borderBottom: i < historySummaries.length - 1 ? '0.5px solid var(--border)' : 'none',
                                transition: 'background 0.1s',
                              }}
                              onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = 'var(--control-bg-hover)' }}
                              onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = '' }}
                            >
                              <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '3px' }}>
                                <span style={{ fontSize: '11px', color: 'var(--text-2)' }}>{rel}</span>
                                <span style={{ fontSize: '11px', color: 'var(--text-2)' }}>{s.messageCount} msgs</span>
                              </div>
                              <div style={{ fontSize: '13px', color: 'var(--text)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                {s.firstUserMessage || <em style={{ color: 'var(--text-2)' }}>No messages</em>}
                              </div>
                            </div>
                          )
                        })}
                      </div>
                    )}
                    {/* Clear history footer */}
                    {!historyLoading && historySummaries.length > 0 && (
                      <div style={{ borderTop: '0.5px solid var(--border)', padding: '8px 10px' }}>
                        <button
                          style={{ width: '100%', padding: '6px 10px', fontSize: '12px', color: 'var(--theme-text-danger, #e05252)', background: 'none', border: 'none', borderRadius: '6px', cursor: 'pointer', textAlign: 'center', transition: 'background 0.1s' }}
                          onMouseEnter={(e) => { (e.currentTarget as HTMLElement).style.background = 'var(--control-bg-hover)' }}
                          onMouseLeave={(e) => { (e.currentTarget as HTMLElement).style.background = '' }}
                          onClick={async () => {
                            if (!confirm('Clear all conversation history? This cannot be undone.')) return
                            await api.clearAllConversations()
                            setHistorySummaries([])
                            setHistoryOpen(false)
                            newConversation()
                          }}
                        >
                          Clear all history
                        </button>
                      </div>
                    )}
                  </div>
                </>
              ) : (
                <button
                  class="btn btn-sm btn-icon"
                  onClick={() => setHistoryOpen(true)}
                  title="Search conversations"
                >
                  <svg width="14" height="14" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
                    <circle cx="6.5" cy="6.5" r="4.5" /><line x1="10" y1="10" x2="14" y2="14" />
                  </svg>
                </button>
              )}
            </div>

            <button class="btn btn-primary btn-sm" onClick={newConversation}>New Chat</button>
          </>
        }
      />

      {/* Messages */}
      <div class="chat-messages">
        <div class="chat-thread">
          {messages.length === 0 && (
            <div class="empty-state">
              <svg class="empty-icon" viewBox="0 0 36 36" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
                <path d="M30 5.5A2.5 2.5 0 0027.5 3h-19A2.5 2.5 0 006 5.5v16A2.5 2.5 0 008.5 24H15l5 6 5-6h2.5A2.5 2.5 0 0030 21.5v-16z" />
              </svg>
              <h3>Start a conversation</h3>
              <p>Type a message below to chat with Atlas</p>
            </div>
          )}

          {messages.map(msg => (
            <div key={msg.id} class={`chat-message-row ${msg.role}${msg.isTyping ? ' typing' : ''}`}>
              <div class={`chat-avatar chat-avatar-${msg.role}`}>
                <span class="chat-avatar-content chat-avatar-content-glyph"><AvatarGlyph /></span>
                <span class="chat-avatar-content chat-avatar-content-initial">{msg.role === 'assistant' ? 'A' : 'Y'}</span>
                <span class="chat-avatar-content chat-avatar-content-minimal"><span class="chat-avatar-minimal-dot" /></span>
              </div>
              <div class="chat-bubble">
                {msg.content
                  // Assistant messages: linkify URLs and anchor preview cards inline.
                  // User messages: plain text (user already knows the links they typed).
                  ? (msg.role === 'assistant'
                      ? renderMessageContent(msg.content, msg.linkPreviews)
                      : msg.content)
                  // No content yet — show typing dots while the turn is active.
                  : (msg.isTyping || msg.id === activeMsgId.current)
                      ? <TypingDots working={presenceWorking} />
                      : null
                }
              </div>
            </div>
          ))}

          {/* Presence line — replaces tool banner + spinner.
              Fades status text in/out smoothly during all active states.
              Never blank: typing dots in bubble + this line below. */}
          {sending && (
            <div class={`chat-presence-line${presenceVisible ? ' visible' : ''}`}>
              <span class="chat-presence-text">{presenceText}</span>
            </div>
          )}

          {approvalBanner && (
            <div class="chat-approval-banner">
              <span class="chat-approval-text">⚠ Waiting for your approval before continuing.</span>
              <a href="#approvals" onClick={(e) => { e.preventDefault(); window.location.hash = 'approvals' }}
                class="btn btn-sm" style={{ color: 'var(--yellow)', borderColor: 'rgba(245,158,11,0.35)' }}>
                Review
              </a>
            </div>
          )}

          <ErrorBanner error={error} onDismiss={() => setError(null)} />
          <div ref={bottomRef} />
        </div>
      </div>

      {/* Composer v2 */}
      <div class="chat-composer">
        <input
          ref={fileInputRef}
          type="file"
          accept="image/*,.pdf"
          multiple
          style={{ display: 'none' }}
          onChange={handleFileChange}
        />

        <div class="chat-composer-inner">
          {/* Attachment chips */}
          {attachments.length > 0 && (
            <div class="chat-attachment-chips">
              {attachments.map((att, i) => (
                <div key={i} class="chat-attachment-chip">
                  <span class="chat-attachment-name">{att.filename}</span>
                  <button class="chat-attachment-remove" onClick={() => removeAttachment(i)} title="Remove">×</button>
                </div>
              ))}
            </div>
          )}

          <div class="chat-composer-row">
            {/* + attachment button — outside left */}
            <button
              class={`chat-round-btn chat-round-btn-attach${attachments.length > 0 ? ' active' : ''}`}
              onClick={() => fileInputRef.current?.click()}
              disabled={sending}
              title="Attach image or PDF"
            >
              <svg width="16" height="16" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                <line x1="8" y1="2" x2="8" y2="14" />
                <line x1="2" y1="8" x2="14" y2="8" />
              </svg>
            </button>

            {/* Textarea box */}
            <div class="chat-textarea-wrap">
              <textarea
                ref={textareaRef}
                class="chat-input"
                placeholder={`Message ${agentName}…`}
                value={input}
                onInput={(e) => { setInput((e.target as HTMLTextAreaElement).value); resizeTextarea() }}
                onKeyDown={handleKeyDown}
                disabled={sending}
                rows={1}
              />
              {/* Mic — inside box, bottom-right, greyed out */}
              <button class="chat-mic-btn" disabled title="Voice message (coming soon)">
                <MicIcon />
              </button>

              {/* Provider select — inside box, bottom-right */}
              <select
                class="chat-provider-select"
                value={activeProvider}
                onChange={(e) => handleProviderChange((e.target as HTMLSelectElement).value as ChatProvider)}
                title="Choose model provider"
              >
                <option value="openai">OpenAI</option>
                <option value="anthropic">Anthropic</option>
                <option value="gemini">Gemini</option>
                <option value="lm_studio">LM Studio</option>
                <option value="ollama">Ollama</option>
                <option value="atlas_engine">Engine LM</option>
              </select>
              <span class="chat-provider-select-icon" aria-hidden="true">
                <svg width="15" height="15" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round">
                  <line x1="2" y1="5" x2="14" y2="5" />
                  <line x1="2" y1="11" x2="14" y2="11" />
                  <circle cx="6" cy="5" r="2" />
                  <circle cx="10" cy="11" r="2" />
                </svg>
              </span>
            </div>

            {/* Send button — no spinner; disabled opacity communicates sending state */}
            <button
              class="chat-round-btn chat-round-btn-send"
              onClick={send}
              disabled={sending || (!input.trim() && attachments.length === 0)}
            >
              <SendIcon />
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}
