import { useEffect, useState } from 'preact/hooks'
import { api, type APIKeyStatus, type CommunicationPlatformStatus, type CommunicationsSnapshot, type RuntimeConfig } from '../api/client'
import { ErrorBanner } from '../components/ErrorBanner'

type StepID = 'mind' | 'provider' | 'channel' | 'finish'
type ProviderID = 'openai' | 'anthropic' | 'gemini' | 'lm_studio' | 'ollama' | 'atlas_engine'
type PlatformID = CommunicationPlatformStatus['platform']

const STEPS: Array<{ id: StepID; label: string; eyebrow: string; title: string; subtitle: string }> = [
  {
    id: 'mind',
    label: 'Mind',
    eyebrow: 'Step 1',
    title: 'Give Atlas a starting point',
    subtitle: 'Seed MIND.md so the first conversation already has context.',
  },
  {
    id: 'provider',
    label: 'Model',
    eyebrow: 'Step 2',
    title: 'Connect the model Atlas should use',
    subtitle: 'Pick the provider you want to start with and save the key Atlas needs.',
  },
  {
    id: 'channel',
    label: 'Channels',
    eyebrow: 'Step 3',
    title: 'Wire up a communication path',
    subtitle: 'Connect a bot channel now, or skip and finish it later.',
  },
  {
    id: 'finish',
    label: 'Ready',
    eyebrow: 'Step 4',
    title: 'Atlas is ready to talk',
    subtitle: 'Local file and system permissions can wait until a real feature needs them.',
  },
]

const PROVIDERS: Array<{ id: ProviderID; label: string; hint: string; statusKey?: keyof APIKeyStatus }> = [
  { id: 'openai', label: 'OpenAI', hint: 'Best default for a fast web-first setup.', statusKey: 'openAIKeySet' },
  { id: 'anthropic', label: 'Anthropic', hint: 'Claude models for longer reasoning and writing.', statusKey: 'anthropicKeySet' },
  { id: 'gemini', label: 'Gemini', hint: 'Google Gemini models with broad multimodal support.', statusKey: 'geminiKeySet' },
  { id: 'lm_studio', label: 'LM Studio', hint: 'Use a local model running on this machine.' },
  { id: 'ollama',        label: 'Ollama',         hint: 'Use any model pulled via Ollama on this machine.' },
  { id: 'atlas_engine', label: 'Engine LM', hint: 'Atlas\'s built-in local inference engine — no external tools needed.' },
]

const CHANNEL_FIELDS: Record<PlatformID, Array<{ id: string; label: string; placeholder: string; inputType?: 'password' | 'text'; storage: 'apiKey' | 'config' }>> = {
  telegram: [
    { id: 'telegram', label: 'Telegram Bot Token', placeholder: '1234567890:ABC…', inputType: 'password', storage: 'apiKey' },
  ],
  discord: [
    { id: 'discord', label: 'Discord Bot Token', placeholder: 'Paste the Discord bot token…', inputType: 'password', storage: 'apiKey' },
    { id: 'discordClientID', label: 'Discord Client ID', placeholder: 'Paste the application ID / client ID…', inputType: 'text', storage: 'config' },
  ],
  slack: [
    { id: 'slackBot', label: 'Slack Bot Token', placeholder: 'xoxb-…', inputType: 'password', storage: 'apiKey' },
    { id: 'slackApp', label: 'Slack App Token', placeholder: 'xapp-…', inputType: 'password', storage: 'apiKey' },
  ],
  whatsapp: [],
  companion: [],
}

const EMPTY_CHANNEL_VALUES: Record<string, string> = {
  telegram: '',
  discord: '',
  discordClientID: '',
  slackBot: '',
  slackApp: '',
}

function defaultMindTemplate(name: string, focus: string) {
  const assistantName = name.trim() || 'Atlas'
  const trimmedFocus = focus.trim()
  const focusBlock = trimmedFocus
    ? trimmedFocus
    : 'Start by learning what the user wants Atlas to help with this week and refine this file through conversation.'

  return [
    `# ${assistantName} Mind`,
    '',
    '## Who I Am',
    `${assistantName} is a practical operator that helps the user turn plans into finished work.`,
    '',
    '## What Matters Right Now',
    focusBlock,
    '',
    '## Working Style',
    '- Prefer clear next steps over abstract advice.',
    '- Ask for local permissions only when a feature truly needs them.',
    '- Keep improving this document after real conversations.',
  ].join('\n')
}

function platformLabel(platform: PlatformID) {
  switch (platform) {
    case 'telegram': return 'Telegram'
    case 'discord': return 'Discord'
    case 'slack': return 'Slack'
    case 'whatsapp': return 'WhatsApp'
    case 'companion': return 'Companion'
    default: return platform
  }
}

function channelHint(platform: PlatformID) {
  switch (platform) {
    case 'telegram':
      return 'Fastest path if you want Atlas reachable from your phone.'
    case 'discord':
      return 'Best if Atlas will live inside a team or project server.'
    case 'slack':
      return 'Good fit when Atlas should join an existing work chat.'
    default:
      return 'Optional. You can come back to this later.'
  }
}

function configuredProvider(providerID: ProviderID, keyStatus: APIKeyStatus | null) {
  const provider = PROVIDERS.find((candidate) => candidate.id === providerID)
  // Local providers don't need an API key — treat them as always configured.
  if (!provider?.statusKey || !keyStatus) return providerID === 'lm_studio' || providerID === 'ollama' || providerID === 'atlas_engine'
  return Boolean(keyStatus[provider.statusKey])
}

export function Onboarding({ onCompleted }: { onCompleted: () => void }) {
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [config, setConfig] = useState<RuntimeConfig | null>(null)
  const [keyStatus, setKeyStatus] = useState<APIKeyStatus | null>(null)
  const [communications, setCommunications] = useState<CommunicationsSnapshot | null>(null)
  const [step, setStep] = useState<StepID>('mind')
  const [saving, setSaving] = useState(false)

  const [assistantName, setAssistantName] = useState('Atlas')
  const [focusPrompt, setFocusPrompt] = useState('')
  const [mindContent, setMindContent] = useState('')

  const [selectedProvider, setSelectedProvider] = useState<ProviderID>('openai')
  const [providerKey, setProviderKey] = useState('')
  const [lmStudioBaseURL, setLMStudioBaseURL] = useState('http://localhost:1234')
  const [ollamaBaseURL, setOllamaBaseURL] = useState('http://localhost:11434')

  const [selectedPlatformID, setSelectedPlatformID] = useState<PlatformID | 'skip'>('skip')
  const [channelValues, setChannelValues] = useState<Record<string, string>>(EMPTY_CHANNEL_VALUES)

  const currentStep = STEPS.find((candidate) => candidate.id === step) ?? STEPS[0]
  const selectedPlatform = communications?.platforms.find((platform) => platform.platform === selectedPlatformID) ?? null
  const readyPlatforms = communications?.platforms.filter((platform) => platform.setupState === 'ready') ?? []

  const load = async () => {
    setLoading(true)
    try {
      const [runtimeConfig, currentKeys, currentMind, currentCommunications] = await Promise.all([
        api.config(),
        api.apiKeys(),
        api.mind(),
        api.communications(),
      ])

      setConfig(runtimeConfig)
      setKeyStatus(currentKeys)
      setCommunications(currentCommunications)
      setAssistantName(runtimeConfig.personaName || 'Atlas')
      setMindContent(currentMind.content || defaultMindTemplate(runtimeConfig.personaName || 'Atlas', ''))
      setSelectedProvider((runtimeConfig.activeAIProvider as ProviderID) || 'openai')
      setLMStudioBaseURL(runtimeConfig.lmStudioBaseURL || 'http://localhost:1234')
      setOllamaBaseURL(runtimeConfig.ollamaBaseURL || 'http://localhost:11434')

      const firstReadyPlatform = currentCommunications.platforms.find((platform) => platform.setupState === 'ready')
      setSelectedPlatformID(firstReadyPlatform?.platform ?? 'skip')
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load onboarding.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    void load()
  }, [])

  const updateChannelValue = (id: string, value: string) => {
    setChannelValues((current) => ({ ...current, [id]: value }))
  }

  const continueFromMind = async () => {
    if (!config) return
    setSaving(true)
    setError(null)
    try {
      const finalName = assistantName.trim() || 'Atlas'
      const finalMind = mindContent.trim() || defaultMindTemplate(finalName, focusPrompt)

      if (finalName !== config.personaName) {
        const { config: updatedConfig } = await api.updateConfig({ ...config, personaName: finalName })
        setConfig(updatedConfig)
      }
      await api.updateMind(finalMind)
      setMindContent(finalMind)
      setStep('provider')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save MIND.md.')
    } finally {
      setSaving(false)
    }
  }

  const continueFromProvider = async () => {
    if (!config) return
    const trimmedKey = providerKey.trim()
    const needsKey = selectedProvider !== 'lm_studio' && selectedProvider !== 'ollama' && selectedProvider !== 'atlas_engine' && !configuredProvider(selectedProvider, keyStatus)
    if (needsKey && !trimmedKey) {
      setError(`Add a ${PROVIDERS.find((provider) => provider.id === selectedProvider)?.label ?? 'provider'} key to continue.`)
      return
    }

    setSaving(true)
    setError(null)
    try {
      if (trimmedKey) {
        await api.setAPIKey(selectedProvider, trimmedKey)
      }

      const nextConfig = {
        ...config,
        activeAIProvider: selectedProvider,
        lmStudioBaseURL: selectedProvider === 'lm_studio' ? lmStudioBaseURL.trim() || 'http://localhost:1234' : config.lmStudioBaseURL,
        ollamaBaseURL: selectedProvider === 'ollama' ? ollamaBaseURL.trim() || 'http://localhost:11434' : config.ollamaBaseURL,
        // Engine LM port is configured in Settings after onboarding; no URL input needed here.
      }
      const { config: updatedConfig } = await api.updateConfig(nextConfig)
      setConfig(updatedConfig)
      setKeyStatus(await api.apiKeys())
      setProviderKey('')
      setStep('channel')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save provider setup.')
    } finally {
      setSaving(false)
    }
  }

  const continueFromChannel = async () => {
    if (!config || selectedPlatformID === 'skip') {
      setStep('finish')
      return
    }

    const fields = CHANNEL_FIELDS[selectedPlatformID]
    setSaving(true)
    setError(null)
    try {
      const credentials = fields.reduce<Record<string, string>>((result, field) => {
        if (field.storage !== 'apiKey') return result
        const value = channelValues[field.id]?.trim()
        if (value) {
          result[field.id] = value
        }
        return result
      }, {})

      const configPayload = selectedPlatformID === 'discord'
        ? { discordClientID: channelValues.discordClientID?.trim() || config.discordClientID || '' }
        : undefined

      const validatedPlatform = await api.validateCommunicationPlatform(selectedPlatformID, {
        credentials,
        config: configPayload,
      })

      for (const [fieldID, value] of Object.entries(credentials)) {
        await api.setAPIKey(fieldID, value)
      }

      let latestConfig = config
      if (selectedPlatformID === 'discord') {
        const discordClientID = channelValues.discordClientID?.trim() || ''
        if (discordClientID && discordClientID !== config.discordClientID) {
          const result = await api.updateConfig({ ...config, discordClientID })
          latestConfig = result.config
          setConfig(result.config)
        }
      }

      if (validatedPlatform.setupState === 'ready' || validatedPlatform.credentialConfigured) {
        await api.updateCommunicationPlatform(selectedPlatformID, true)
      }

      setCommunications(await api.communications())
      setConfig(latestConfig)
      setChannelValues(EMPTY_CHANNEL_VALUES)
      setStep('finish')
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save channel setup.')
    } finally {
      setSaving(false)
    }
  }

  const finishOnboarding = async () => {
    setSaving(true)
    setError(null)
    try {
      await api.updateOnboardingStatus(true)
      onCompleted()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to finish onboarding.')
    } finally {
      setSaving(false)
    }
  }

  if (loading) {
    return (
      <div class="onboarding-shell">
        <div class="onboarding-card">
          <div class="onboarding-loading">
            <span class="spinner" />
            <span>Preparing first-run setup…</span>
          </div>
        </div>
      </div>
    )
  }

  return (
    <div class="onboarding-shell">
      <div class="onboarding-card">
        <aside class="onboarding-rail">
          <div class="onboarding-brand">
            <div class="onboarding-brand-mark">A</div>
            <div>
              <div class="onboarding-brand-title">Project Atlas</div>
              <div class="onboarding-brand-subtitle">Web-first runtime setup</div>
            </div>
          </div>

          <div class="onboarding-value">
            <div class="surface-eyebrow">Goal</div>
            <h2>Get Atlas useful before asking for extra permissions.</h2>
            <p>We’ll seed the mind, connect a model, add a channel if you want one, then drop straight into conversation.</p>
          </div>

          <div class="onboarding-step-list">
            {STEPS.map((candidate, index) => {
              const active = candidate.id === step
              const complete = STEPS.findIndex((item) => item.id === step) > index
              return (
                <div key={candidate.id} class={`onboarding-step-item${active ? ' active' : ''}${complete ? ' complete' : ''}`}>
                  <span>{index + 1}</span>
                  <div>
                    <div class="onboarding-step-label">{candidate.label}</div>
                    <div class="onboarding-step-hint">{candidate.subtitle}</div>
                  </div>
                </div>
              )
            })}
          </div>
        </aside>

        <section class="onboarding-panel">
          <div class="onboarding-panel-header">
            <div class="surface-eyebrow">{currentStep.eyebrow}</div>
            <h1>{currentStep.title}</h1>
            <p>{currentStep.subtitle}</p>
          </div>

          <ErrorBanner error={error} onDismiss={() => setError(null)} />

          {step === 'mind' && (
            <div class="onboarding-section">
              <label class="onboarding-field">
                <span>What should Atlas call itself?</span>
                <input
                  class="input"
                  type="text"
                  value={assistantName}
                  onInput={(event) => setAssistantName((event.target as HTMLInputElement).value)}
                  placeholder="Atlas"
                />
              </label>

              <label class="onboarding-field">
                <span>What should Atlas focus on first?</span>
                <textarea
                  class="mind-raw-editor onboarding-textarea"
                  value={focusPrompt}
                  onInput={(event) => setFocusPrompt((event.target as HTMLTextAreaElement).value)}
                  placeholder="Ship the migration to a web-first, Go-backed Atlas without losing momentum."
                  rows={4}
                />
              </label>

              <label class="onboarding-field">
                <span>MIND.md seed</span>
                <textarea
                  class="mind-raw-editor onboarding-textarea onboarding-textarea-large"
                  value={mindContent}
                  onInput={(event) => setMindContent((event.target as HTMLTextAreaElement).value)}
                  rows={14}
                />
              </label>
            </div>
          )}

          {step === 'provider' && (
            <div class="onboarding-section">
              <div class="onboarding-choice-grid">
                {PROVIDERS.map((provider) => {
                  const selected = provider.id === selectedProvider
                  const configured = configuredProvider(provider.id, keyStatus)
                  return (
                    <button
                      key={provider.id}
                      class={`onboarding-choice-card${selected ? ' selected' : ''}`}
                      onClick={() => setSelectedProvider(provider.id)}
                    >
                      <div class="onboarding-choice-header">
                        <strong>{provider.label}</strong>
                        {configured && <span class="badge badge-green">Configured</span>}
                      </div>
                      <p>{provider.hint}</p>
                    </button>
                  )
                })}
              </div>

              {selectedProvider !== 'lm_studio' && selectedProvider !== 'ollama' && selectedProvider !== 'atlas_engine' && (
                <label class="onboarding-field">
                  <span>{PROVIDERS.find((provider) => provider.id === selectedProvider)?.label} API key</span>
                  <input
                    class="input"
                    type="password"
                    value={providerKey}
                    onInput={(event) => setProviderKey((event.target as HTMLInputElement).value)}
                    placeholder="Paste the key Atlas should use…"
                  />
                </label>
              )}

              {selectedProvider === 'lm_studio' && (
                <label class="onboarding-field">
                  <span>LM Studio server URL</span>
                  <input
                    class="input"
                    type="text"
                    value={lmStudioBaseURL}
                    onInput={(event) => setLMStudioBaseURL((event.target as HTMLInputElement).value)}
                    placeholder="http://localhost:1234"
                  />
                </label>
              )}

              {selectedProvider === 'ollama' && (
                <label class="onboarding-field">
                  <span>Ollama server URL</span>
                  <input
                    class="input"
                    type="text"
                    value={ollamaBaseURL}
                    onInput={(event) => setOllamaBaseURL((event.target as HTMLInputElement).value)}
                    placeholder="http://localhost:11434"
                  />
                </label>
              )}

              {selectedProvider === 'atlas_engine' && (
                <p class="onboarding-hint">
                  Engine LM is built in — no server URL or API key needed. You can download and load models from Settings after setup.
                </p>
              )}
            </div>
          )}

          {step === 'channel' && (
            <div class="onboarding-section">
              <div class="onboarding-choice-grid">
                <button
                  class={`onboarding-choice-card${selectedPlatformID === 'skip' ? ' selected' : ''}`}
                  onClick={() => setSelectedPlatformID('skip')}
                >
                  <div class="onboarding-choice-header">
                    <strong>Skip for now</strong>
                  </div>
                  <p>Atlas can start in the web UI first. Add chat channels later from Communications.</p>
                </button>

                {(communications?.platforms ?? [])
                  .filter((platform) => platform.available && platform.platform !== 'companion' && platform.platform !== 'whatsapp')
                  .map((platform) => (
                    <button
                      key={platform.id}
                      class={`onboarding-choice-card${selectedPlatformID === platform.platform ? ' selected' : ''}`}
                      onClick={() => setSelectedPlatformID(platform.platform)}
                    >
                      <div class="onboarding-choice-header">
                        <strong>{platformLabel(platform.platform)}</strong>
                        {platform.setupState === 'ready' && <span class="badge badge-green">Ready</span>}
                      </div>
                      <p>{channelHint(platform.platform)}</p>
                    </button>
                  ))}
              </div>

              {selectedPlatform && (
                <div class="onboarding-inline-card">
                  {CHANNEL_FIELDS[selectedPlatform.platform].map((field) => (
                    <label key={field.id} class="onboarding-field">
                      <span>{field.label}</span>
                      <input
                        class="input"
                        type={field.inputType ?? 'password'}
                        value={channelValues[field.id] ?? ''}
                        onInput={(event) => updateChannelValue(field.id, (event.target as HTMLInputElement).value)}
                        placeholder={field.placeholder}
                      />
                    </label>
                  ))}
                </div>
              )}
            </div>
          )}

          {step === 'finish' && (
            <div class="onboarding-section">
              <div class="onboarding-summary-grid">
                <div class="onboarding-summary-card">
                  <div class="surface-eyebrow">Mind</div>
                  <strong>{assistantName.trim() || 'Atlas'}</strong>
                  <p>{mindContent.trim() ? 'MIND.md is seeded and ready to evolve through conversation.' : 'MIND.md will be seeded on the next save.'}</p>
                </div>
                <div class="onboarding-summary-card">
                  <div class="surface-eyebrow">Model</div>
                  <strong>{PROVIDERS.find((provider) => provider.id === selectedProvider)?.label ?? 'OpenAI'}</strong>
                  <p>{configuredProvider(selectedProvider, keyStatus) || selectedProvider === 'lm_studio' || selectedProvider === 'ollama' || selectedProvider === 'atlas_engine' ? 'Atlas has a model path configured.' : 'You can still add the key later from Credentials.'}</p>
                </div>
                <div class="onboarding-summary-card">
                  <div class="surface-eyebrow">Channels</div>
                  <strong>{readyPlatforms.length > 0 ? `${readyPlatforms.length} ready` : 'Optional'}</strong>
                  <p>{readyPlatforms.length > 0 ? 'At least one communication route is already connected.' : 'No problem. Atlas can start in the web UI and connect channels later.'}</p>
                </div>
              </div>

              <div class="onboarding-note">
                File access, notifications, and system-level permissions now happen later, when a real feature needs them.
              </div>
            </div>
          )}

          <div class="onboarding-actions">
            {step !== 'mind' && (
              <button
                class="btn btn-ghost btn-sm"
                onClick={() => setStep(STEPS[Math.max(0, STEPS.findIndex((candidate) => candidate.id === step) - 1)].id)}
                disabled={saving}
              >
                Back
              </button>
            )}

            {step === 'mind' && (
              <button class="btn btn-primary btn-sm" onClick={() => void continueFromMind()} disabled={saving}>
                {saving ? 'Saving…' : 'Save MIND.md'}
              </button>
            )}

            {step === 'provider' && (
              <button class="btn btn-primary btn-sm" onClick={() => void continueFromProvider()} disabled={saving}>
                {saving ? 'Saving…' : 'Connect model'}
              </button>
            )}

            {step === 'channel' && (
              <button class="btn btn-primary btn-sm" onClick={() => void continueFromChannel()} disabled={saving}>
                {saving ? 'Saving…' : selectedPlatformID === 'skip' ? 'Continue without channels' : 'Save channel'}
              </button>
            )}

            {step === 'finish' && (
              <button class="btn btn-primary btn-sm" onClick={() => void finishOnboarding()} disabled={saving}>
                {saving ? 'Finishing…' : 'Start chatting'}
              </button>
            )}
          </div>
        </section>
      </div>
    </div>
  )
}
