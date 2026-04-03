import { useEffect, useState } from 'preact/hooks'
import { api, CommunicationChannel, CommunicationPlatformStatus, CommunicationsSnapshot, RuntimeConfig } from '../api/client'
import { PageHeader } from '../components/PageHeader'
import { ErrorBanner } from '../components/ErrorBanner'

const RefreshIcon = () => (
  <svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <path d="M2.5 8a5.5 5.5 0 0 1 9.5-3.8" />
    <polyline points="13.5,2.5 13.5,6 10,6" />
    <path d="M13.5 8a5.5 5.5 0 0 1-9.5 3.8" />
    <polyline points="2.5,13.5 2.5,10 6,10" />
  </svg>
)

type PlatformID = CommunicationPlatformStatus['platform']
type SetupField = {
  id: string
  label: string
  placeholder: string
  inputType?: 'password' | 'text'
  storage: 'apiKey' | 'config'
}

const QUICK_SETUP_FIELDS: Record<PlatformID, SetupField[]> = {
  telegram: [
    { id: 'telegram', label: 'Telegram Bot Token', placeholder: '1234567890:ABC…', inputType: 'password', storage: 'apiKey' },
  ],
  discord: [
    { id: 'discord', label: 'Discord Bot Token', placeholder: 'Paste the Bot Token from the Discord Bot page…', inputType: 'password', storage: 'apiKey' },
    { id: 'discordClientID', label: 'Discord Client ID', placeholder: 'Paste the Application ID / Client ID…', inputType: 'text', storage: 'config' },
  ],
  slack: [
    { id: 'slackBot', label: 'Slack Bot Token', placeholder: 'xoxb-…', inputType: 'password', storage: 'apiKey' },
    { id: 'slackApp', label: 'Slack App Token', placeholder: 'xapp-…', inputType: 'password', storage: 'apiKey' },
  ],
  whatsapp: [],
  companion: [],
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

function platformSubtitle(platform: PlatformID) {
  switch (platform) {
    case 'telegram': return 'Token-based chat bot integration with polling.'
    case 'discord': return 'Bot gateway integration for DMs and @mentions.'
    case 'slack': return 'Socket Mode integration for DMs and @mentions.'
    case 'whatsapp': return 'Not available yet.'
    case 'companion': return 'Reserved for the Atlas companion app.'
    default: return ''
  }
}

function setupBadgeClass(status: CommunicationPlatformStatus) {
  switch (status.setupState) {
    case 'ready': return 'badge badge-green'
    case 'validation_failed': return 'badge badge-red'
    case 'partial_setup': return 'badge badge-yellow'
    case 'missing_credentials': return 'badge badge-gray'
    default: return 'badge badge-gray'
  }
}

function setupHint(status: CommunicationPlatformStatus) {
  if (status.blockingReason) return status.blockingReason
  switch (status.platform) {
    case 'telegram':
      return 'Add your Telegram bot token, enable the bridge, and validate polling.'
    case 'discord':
      return 'Add the Discord bot token and client ID, install the bot into your server, enable Message Content intent, and validate gateway access.'
    case 'slack':
      return 'Add the xoxb bot token and xapp app token, enable Socket Mode, and validate DMs plus @mentions.'
    default:
      return 'Finish setup to make this channel available.'
  }
}

const EMPTY_VALUES: Record<string, string> = {
  telegram: '',
  discord: '',
  discordClientID: '',
  slackBot: '',
  slackApp: '',
}

function platformBotLabel(platform: CommunicationPlatformStatus) {
  return `Bot name: ${platform.connectedAccountName ?? 'Not available'}`
}

function platformSetupNotes(platform: PlatformID) {
  switch (platform) {
    case 'telegram':
      return [
        'Create the bot with BotFather and paste the token exactly as given.',
        'Send the bot one message after setup so Atlas can discover the chat.',
        'Validate once the bot is reachable and polling is enabled.',
      ]
    case 'discord':
      return [
        'Paste the Bot Token and the Application ID / Client ID.',
        'Install the bot into the server where you want to use it.',
        'Enable Message Content intent before validating.',
        'Test one DM and one @mention in a normal channel.',
      ]
    case 'slack':
      return [
        'Use the Bot User OAuth Token and the App-Level token.',
        'Turn on the Messages tab, Socket Mode, and install the app.',
        'Subscribe to bot DMs and app mentions before validating.',
        'Send one DM or @mention after setup so Atlas can discover the channel.',
      ]
    default:
      return ['Finish setup to make this platform available in Atlas.']
  }
}

function platformDocsURL(platform: PlatformID) {
  switch (platform) {
    case 'telegram':
      return 'https://core.telegram.org/bots#6-botfather'
    case 'discord':
      return 'https://discord.com/developers/docs/quick-start/getting-started'
    case 'slack':
      return 'https://api.slack.com/start/quickstart'
    default:
      return null
  }
}

function PlatformLogo({ platform }: { platform: PlatformID }) {
  const assetPath = (() => {
    switch (platform) {
      case 'telegram':
        return '/web/chat-app-logos/telegram.png'
      case 'discord':
        return '/web/chat-app-logos/discord.png'
      case 'slack':
        return '/web/chat-app-logos/slack.png'
      default:
        return null
    }
  })()

  return (
    <div class={`communication-platform-logo communication-platform-logo-${platform}`} aria-hidden="true">
      {assetPath ? (
        <img src={assetPath} alt="" class="communication-platform-logo-image" />
      ) : (
        <span>{platformLabel(platform).charAt(0)}</span>
      )}
    </div>
  )
}

export function Communications() {
  const [snapshot, setSnapshot] = useState<CommunicationsSnapshot | null>(null)
  const [config, setConfig] = useState<RuntimeConfig | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [busyPlatform, setBusyPlatform] = useState<string | null>(null)
  const [selectedPlatformID, setSelectedPlatformID] = useState<PlatformID | null>(null)
  const [credentialValues, setCredentialValues] = useState<Record<string, string>>(EMPTY_VALUES)
  const [initialCredentialValues, setInitialCredentialValues] = useState<Record<string, string>>(EMPTY_VALUES)
  const [savingCredentials, setSavingCredentials] = useState(false)

  const mergePlatformStatus = (updatedPlatform: CommunicationPlatformStatus) => {
    setSnapshot(current => {
      if (!current) return current
      return {
        ...current,
        platforms: current.platforms.map(platform =>
          platform.platform === updatedPlatform.platform ? updatedPlatform : platform
        ),
      }
    })
  }

  const waitForPlatformReady = async (platformID: PlatformID) => {
    for (let attempt = 0; attempt < 8; attempt += 1) {
      const nextSnapshot = await api.communications()
      setSnapshot(nextSnapshot)
      const latest = nextSnapshot.platforms.find(platform => platform.platform === platformID)
      if (latest?.setupState === 'ready') {
        return latest
      }
      await new Promise(resolve => window.setTimeout(resolve, 750))
    }
    return null
  }

  const load = async () => {
    setLoading(true)
    try {
      const [communicationsSnapshot, runtimeConfig] = await Promise.all([
        api.communications(),
        api.config(),
      ])
      setSnapshot(communicationsSnapshot)
      setConfig(runtimeConfig)
      setError(null)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load communications.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  const platforms = snapshot?.platforms ?? []
  const readyPlatforms = platforms.filter(platform => platform.setupState === 'ready')
  const addablePlatforms = platforms.filter(platform => platform.available && platform.setupState !== 'ready')
  const selectedPlatform = platforms.find(platform => platform.platform === selectedPlatformID) ?? null
  const readyPlatformIDs = new Set(readyPlatforms.map(platform => platform.platform))
  const channels = (snapshot?.channels ?? []).filter(channel => readyPlatformIDs.has(channel.platform))

  const choosePlatform = async (platform: PlatformID) => {
    const initialValues = {
      ...EMPTY_VALUES,
      discordClientID: platform === 'discord' ? (config?.discordClientID ?? '') : '',
    }
    setSelectedPlatformID(platform)
    setCredentialValues(initialValues)
    setInitialCredentialValues(initialValues)
    setError(null)

    try {
      const setup = await api.communicationSetupValues(platform)
      const loadedValues = {
        ...EMPTY_VALUES,
        ...setup.values,
        discordClientID: platform === 'discord'
          ? (setup.values.discordClientID ?? config?.discordClientID ?? '')
          : '',
      }
      setCredentialValues(loadedValues)
      setInitialCredentialValues(loadedValues)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load saved channel credentials.')
    }
  }

  const saveAndValidate = async (platform: CommunicationPlatformStatus) => {
    const fields = QUICK_SETUP_FIELDS[platform.platform]
    setSavingCredentials(true)
    setBusyPlatform(`${platform.platform}:setup`)
    setError(null)

    try {
      const credentials = fields.reduce<Record<string, string>>((result, field) => {
        if (field.storage !== 'apiKey') return result
        const value = credentialValues[field.id]?.trim()
        if (value) {
          result[field.id] = value
        }
        return result
      }, {})

      const configPayload = platform.platform === 'discord'
        ? { discordClientID: credentialValues.discordClientID?.trim() || config?.discordClientID || '' }
        : undefined

      const validationResult = await api.validateCommunicationPlatform(platform.platform, {
        credentials,
        config: configPayload,
      })
      mergePlatformStatus(validationResult)

      if (validationResult.setupState !== 'ready') {
        await load()
        return
      }

      for (const field of fields) {
        const value = credentialValues[field.id]?.trim()
        const initialValue = initialCredentialValues[field.id]?.trim() ?? ''
        if (value && field.storage === 'apiKey' && value !== initialValue) {
          await api.setAPIKey(field.id, value)
        }
      }

      if (platform.platform === 'discord' && config) {
        const discordClientID = credentialValues.discordClientID?.trim() ?? ''
        if (discordClientID !== config.discordClientID) {
          const { config: updatedConfig } = await api.updateConfig({ ...config, discordClientID })
          setConfig(updatedConfig)
        }
      }

      const updatedPlatform = await api.updateCommunicationPlatform(platform.platform, true)
      mergePlatformStatus(updatedPlatform)
      setError(null)
      await waitForPlatformReady(platform.platform)
      setSelectedPlatformID(null)
      setCredentialValues(EMPTY_VALUES)
      setInitialCredentialValues(EMPTY_VALUES)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to complete setup.')
      await load()
    } finally {
      setSavingCredentials(false)
      setBusyPlatform(null)
    }
  }

  const disablePlatform = async (platform: PlatformID) => {
    setBusyPlatform(`${platform}:disable`)
    try {
      await api.updateCommunicationPlatform(platform, false)
      if (selectedPlatformID === platform) {
        setSelectedPlatformID(null)
      }
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to disable platform.')
    } finally {
      setBusyPlatform(null)
    }
  }

  const revalidatePlatform = async (platform: PlatformID) => {
    setBusyPlatform(`${platform}:validate`)
    try {
      const validatedPlatform = await api.validateCommunicationPlatform(platform)
      mergePlatformStatus(validatedPlatform)
      setError(null)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Validation failed.')
    } finally {
      setBusyPlatform(null)
    }
  }

  return (
    <div class="screen">
      <PageHeader
        title="Communications"
        subtitle="Manage connected channels and complete setup for supported chat platforms."
        actions={
          <button class="btn btn-primary btn-sm" onClick={load} disabled={loading}>
            {loading ? <><span class="spinner" style={{ width: '11px', height: '11px' }} /> Refresh</> : <><RefreshIcon /> Refresh</>}
          </button>
        }
      />

      <ErrorBanner error={error} onDismiss={() => setError(null)} />

      <div>
        <div class="section-label">Connected Channels</div>
        <div class="card settings-group">
          {readyPlatforms.length === 0 && (
            <div class="communication-empty-state">
              No channels are connected yet. Use Add Channel to set up Telegram, Discord, or Slack.
            </div>
          )}
          {readyPlatforms.map((platform, index) => (
            <ConnectedPlatformRow
              key={platform.id}
              platform={platform}
              last={index === readyPlatforms.length - 1}
              busy={busyPlatform === `${platform.platform}:disable` || busyPlatform === `${platform.platform}:validate`}
              onDisable={() => disablePlatform(platform.platform)}
              onValidate={() => revalidatePlatform(platform.platform)}
            />
          ))}
        </div>
      </div>

      <div>
        <div class="section-label">Add Channel</div>
        <div class="card settings-group">
          {addablePlatforms.length > 0 && (
            <div class="settings-row">
              <div class="settings-label-col">
                <div class="settings-label">Available Apps</div>
                <div class="settings-sublabel">Choose any supported app that is not fully configured yet to open guided setup.</div>
              </div>
            </div>
          )}
          <div>
            {addablePlatforms.map(platform => (
              <button
                key={platform.id}
                class="communication-picker-row"
                onClick={() => { void choosePlatform(platform.platform) }}
              >
                <div class="communication-platform-summary">
                  <PlatformLogo platform={platform.platform} />
                  <div class="settings-label-col">
                    <div class="settings-label">{platformLabel(platform.platform)}</div>
                    <div class="settings-sublabel">{platformSubtitle(platform.platform)}</div>
                  </div>
                </div>
                <div class="communication-platform-actions">
                  <button type="button" class="btn btn-sm">Set up</button>
                </div>
              </button>
            ))}
            {addablePlatforms.length === 0 && (
              <div class="communication-empty-state">All supported communication apps are already configured.</div>
            )}
          </div>
        </div>
      </div>

      <div>
        <div class="section-label">Routing</div>
        <div class="card communication-routing-card">
          <div class="settings-row">
            <div class="settings-label-col">
              <div class="settings-label">Inbound routing</div>
              <div class="settings-sublabel">All connected channels route into the same Atlas runtime.</div>
            </div>
            <div class="badge badge-green">Unified</div>
          </div>
          <div class="settings-row">
            <div class="settings-label-col">
              <div class="settings-label">Outbound automations</div>
              <div class="settings-sublabel">Automation results can target any notification-capable ready channel.</div>
            </div>
            <div class="badge badge-gray">{channels.filter(channel => channel.canReceiveNotifications).length} channels</div>
          </div>
        </div>
      </div>

      <div>
        <div class="section-label">Recent Sessions</div>
        <div class="card settings-group">
          {channels.length === 0 && (
            <div class="communication-empty-state">
              No sessions discovered yet. Once a ready integration receives a message, it will appear here.
            </div>
          )}
          {channels.map((channel, index) => (
            <CommunicationChannelRow key={channel.id} channel={channel} last={index === channels.length - 1} />
          ))}
        </div>
      </div>

      {selectedPlatform && (
        <QuickSetupModal
          platform={selectedPlatform}
          values={credentialValues}
          saving={savingCredentials}
          busy={busyPlatform === `${selectedPlatform.platform}:setup`}
          onChange={(id, value) => setCredentialValues(current => ({ ...current, [id]: value }))}
          onCancel={() => {
            setSelectedPlatformID(null)
            setCredentialValues(EMPTY_VALUES)
            setInitialCredentialValues(EMPTY_VALUES)
          }}
          onValidate={() => saveAndValidate(selectedPlatform)}
        />
      )}
    </div>
  )
}

function ConnectedPlatformRow({
  platform,
  last,
  busy,
  onDisable,
  onValidate,
}: {
  platform: CommunicationPlatformStatus
  last: boolean
  busy: boolean
  onDisable: () => void
  onValidate: () => void
}) {
  return (
    <div class="settings-row" style={{ borderBottom: last ? 'none' : undefined }}>
      <div class="communication-platform-summary">
        <PlatformLogo platform={platform.platform} />
        <div class="settings-label-col">
          <div class="settings-label">{platformLabel(platform.platform)}</div>
          <div class="settings-sublabel communication-bot-label">{platformBotLabel(platform)}</div>
          {platform.blockingReason && <div class="settings-sublabel" style={{ color: 'var(--text-2)', marginTop: '4px' }}>{platform.blockingReason}</div>}
        </div>
      </div>
      <div class="communication-platform-actions">
        <span class={setupBadgeClass(platform)}>{platform.statusLabel}</span>
        <button class="btn btn-sm" onClick={onValidate} disabled={busy}>
          {busy ? 'Working…' : 'Validate'}
        </button>
        <button class="btn btn-sm btn-danger" onClick={onDisable} disabled={busy}>
          Disable
        </button>
      </div>
    </div>
  )
}

function QuickSetupModal({
  platform,
  values,
  saving,
  busy,
  onChange,
  onCancel,
  onValidate,
}: {
  platform: CommunicationPlatformStatus
  values: Record<string, string>
  saving: boolean
  busy: boolean
  onChange: (id: string, value: string) => void
  onCancel: () => void
  onValidate: () => void
}) {
  const fields = QUICK_SETUP_FIELDS[platform.platform]
  const hasPendingInput = fields.some(field => values[field.id]?.trim())
  const isDiscord = platform.platform === 'discord'
  const installURL = platform.metadata.installURL
  const notes = platformSetupNotes(platform.platform)
  const docsURL = platformDocsURL(platform.platform)

  return (
      <div class="modal-overlay" onClick={(event) => { if ((event.target as HTMLElement).classList.contains('modal-overlay')) onCancel() }}>
      <div class="modal communication-setup-modal">
        <div class="modal-header communication-modal-header">
          <div class="communication-modal-title-wrap">
            <PlatformLogo platform={platform.platform} />
            <div class="communication-modal-title-block">
              <div class="surface-eyebrow">Quick Setup</div>
              <h3 class="communication-modal-title">{platformLabel(platform.platform)}</h3>
            </div>
          </div>
          <div class="communication-modal-header-actions">
            <button class="btn btn-sm btn-ghost" onClick={onCancel}>Cancel</button>
          </div>
        </div>

        <div class="modal-body communication-modal-body">
          <div class="communication-modal-panel">
            <div class="communication-panel-label">Required Credentials</div>
            <div class="communication-setup-fields">
              {fields.map(field => (
                <label key={field.id} class="communication-secret-field">
                  <span>{field.label}</span>
                  <input
                    class="input"
                    type={field.inputType ?? 'password'}
                    value={values[field.id] ?? ''}
                    placeholder={field.placeholder}
                    onInput={event => onChange(field.id, (event.target as HTMLInputElement).value)}
                  />
                </label>
              ))}
            </div>
            {isDiscord && (
              <div class="communication-setup-inline-action">
                {installURL ? (
                  <a class="btn btn-sm" href={installURL} target="_blank" rel="noreferrer">
                    Install Bot in Discord
                  </a>
                ) : (
                  <div class="communication-setup-note">Add the Discord Client ID to unlock the install link.</div>
                )}
              </div>
            )}
          </div>

          <div class="communication-setup-guide-column">
            <div class="communication-panel-label">Setup Tips</div>
            <div class="communication-setup-checklist communication-setup-checklist-tight">
              {notes.map(note => (
                <div key={note} class="communication-setup-check">{note}</div>
              ))}
            </div>
            {docsURL && (
              <div class="communication-setup-docs">
                <a class="btn btn-sm btn-ghost" href={docsURL} target="_blank" rel="noreferrer">
                  Open official setup guide
                </a>
              </div>
            )}
          </div>
        </div>

        <div class="modal-footer communication-setup-footer">
          <div class="communication-setup-footer-actions">
            <button class="btn btn-primary btn-sm" onClick={onValidate} disabled={saving || busy || (fields.length > 0 && !platform.credentialConfigured && !hasPendingInput)}>
              {saving || busy ? 'Validating…' : 'Save & Validate'}
            </button>
          </div>
        </div>
      </div>
    </div>
  )
}

function CommunicationChannelRow({ channel, last }: { channel: CommunicationChannel; last: boolean }) {
  return (
    <div class="settings-row" style={{ borderBottom: last ? 'none' : undefined }}>
      <div class="settings-label-col">
        <div class="settings-label">
          {platformLabel(channel.platform)} · {channel.channelName ?? channel.channelID}
        </div>
        <div class="settings-sublabel">
          Conversation {channel.activeConversationID.slice(0, 8)}
          {channel.threadID ? ` · thread ${channel.threadID}` : ''}
          {' · '}
          last active {new Date(channel.updatedAt).toLocaleString()}
        </div>
      </div>
      <div class="communication-channel-trailing">
        {channel.canReceiveNotifications && <span class="badge badge-green">Notifications</span>}
      </div>
    </div>
  )
}
