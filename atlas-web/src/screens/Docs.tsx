import { useMemo, useState } from 'preact/hooks'
import { PageHeader } from '../components/PageHeader'

type DocsGroupID = 'intro' | 'getting-started' | 'web-ui' | 'integrations' | 'resources'

type DocsPageID =
  | 'home'
  | 'what-atlas-is'
  | 'how-atlas-works'
  | 'core-concepts'
  | 'getting-started'
  | 'chat'
  | 'communications'
  | 'approvals'
  | 'automations'
  | 'workflows'
  | 'api-keys'
  | 'ai-providers'
  | 'integrations-overview'
  | 'telegram-setup'
  | 'discord-setup'
  | 'slack-setup'
  | 'routing-sessions'
  | 'notifications-delivery'
  | 'integration-troubleshooting'
  | 'github'

interface DocsPageMeta {
  id: DocsPageID
  title: string
  group: DocsGroupID
  keywords?: string[]
}

interface DocsGroup {
  id: DocsGroupID
  title: string
}

const DOCS_GROUPS: DocsGroup[] = [
  { id: 'intro', title: 'Introduction' },
  { id: 'getting-started', title: 'Getting Started' },
  { id: 'web-ui', title: 'Web UI' },
  { id: 'integrations', title: 'Integrations' },
  { id: 'resources', title: 'Resources' },
]

const DOCS_PAGES: DocsPageMeta[] = [
  { id: 'home', title: 'Docs Home', group: 'intro', keywords: ['overview', 'portal', 'atlas'] },
  { id: 'what-atlas-is', title: 'What Atlas Is', group: 'intro', keywords: ['product', 'operator', 'assistant'] },
  { id: 'how-atlas-works', title: 'How Atlas Works', group: 'intro', keywords: ['runtime', 'daemon', 'approvals', 'skills'] },
  { id: 'core-concepts', title: 'Core Concepts', group: 'intro', keywords: ['skills', 'workflows', 'automations', 'memory'] },
  { id: 'getting-started', title: 'Getting Started', group: 'getting-started', keywords: ['setup', 'first run', 'onboarding'] },
  { id: 'ai-providers', title: 'AI Provider Billing', group: 'getting-started', keywords: ['openai', 'anthropic', 'gemini', 'lm studio', 'credits', 'free tier', 'billing', 'api key'] },
  { id: 'chat', title: 'Chat', group: 'web-ui', keywords: ['messages', 'attachments', 'conversation'] },
  { id: 'communications', title: 'Communications', group: 'web-ui', keywords: ['platforms', 'routing', 'sessions'] },
  { id: 'approvals', title: 'Approvals', group: 'web-ui', keywords: ['permissions', 'review', 'action'] },
  { id: 'automations', title: 'Automations', group: 'web-ui', keywords: ['schedule', 'notifications', 'gremlins'] },
  { id: 'workflows', title: 'Workflows', group: 'web-ui', keywords: ['template', 'trust scope', 'runs'] },
  { id: 'api-keys', title: 'Credentials', group: 'web-ui', keywords: ['openai', 'telegram', 'discord', 'token', 'api keys'] },
  { id: 'integrations-overview', title: 'How Atlas Chat Integrations Work', group: 'integrations', keywords: ['telegram', 'discord', 'slack', 'channels'] },
  { id: 'telegram-setup', title: 'Telegram Setup', group: 'integrations', keywords: ['botfather', 'chat id', 'token'] },
  { id: 'discord-setup', title: 'Discord Setup', group: 'integrations', keywords: ['discord app', 'bot token', 'gateway intents'] },
  { id: 'slack-setup', title: 'Slack Setup', group: 'integrations', keywords: ['coming soon', 'workspace', 'oauth'] },
  { id: 'routing-sessions', title: 'Routing and Sessions', group: 'integrations', keywords: ['channel mapping', 'session', 'conversation'] },
  { id: 'notifications-delivery', title: 'Notifications and Delivery', group: 'integrations', keywords: ['automation results', 'delivery', 'destinations'] },
  { id: 'integration-troubleshooting', title: 'Integration Troubleshooting', group: 'integrations', keywords: ['errors', 'debugging', 'reconnect'] },
  { id: 'github', title: 'GitHub', group: 'resources', keywords: ['source', 'repository', 'issues'] },
]

const DEFAULT_EXPANDED: Record<DocsGroupID, boolean> = {
  intro: true,
  'getting-started': true,
  'web-ui': false,
  integrations: true,
  resources: false,
}

export function Docs() {
  const [activePage, setActivePage] = useState<DocsPageID>('home')
  const [query, setQuery] = useState('')
  const [expandedGroups, setExpandedGroups] = useState<Record<DocsGroupID, boolean>>(DEFAULT_EXPANDED)

  const normalizedQuery = query.trim().toLowerCase()

  const filteredPages = useMemo(() => {
    if (!normalizedQuery) return DOCS_PAGES
    return DOCS_PAGES.filter((page) => {
      const haystack = [page.title, ...(page.keywords ?? [])].join(' ').toLowerCase()
      return haystack.includes(normalizedQuery)
    })
  }, [normalizedQuery])

  const visiblePageIDs = new Set(filteredPages.map((page) => page.id))

  const groupsWithPages = DOCS_GROUPS.map((group) => ({
    ...group,
    pages: filteredPages.filter((page) => page.group === group.id),
  })).filter((group) => group.pages.length > 0)

  const page = DOCS_PAGES.find((item) => item.id === activePage) ?? DOCS_PAGES[0]

  return (
    <div class="screen docs-screen">
      <PageHeader
        title="Docs"
        subtitle="Operator-grade guides for Atlas, built right into the control center."
      />

      <div class="docs-portal">
        <div class="docs-reader">
          <DocsPage pageID={page.id} onNavigate={setActivePage} />
        </div>

        <aside class="docs-sidebar">
          <div class="docs-sidebar-search-wrap">
            <input
              class="input docs-search-input"
              value={query}
              onInput={(e) => setQuery((e.target as HTMLInputElement).value)}
              placeholder="Search docs"
              aria-label="Search docs"
            />
          </div>

          <div class="docs-sidebar-label">Atlas Docs</div>

          <div class="docs-nav">
            {groupsWithPages.map((group) => {
              const shouldExpand = normalizedQuery ? true : expandedGroups[group.id]
              return (
                <div class="docs-nav-group" key={group.id}>
                  <button
                    class="docs-nav-group-btn"
                    onClick={() =>
                      setExpandedGroups((current) => ({ ...current, [group.id]: !current[group.id] }))
                    }
                    disabled={!!normalizedQuery}
                  >
                    <span>{group.title}</span>
                    <span class={`docs-nav-group-caret${shouldExpand ? ' expanded' : ''}`}>⌃</span>
                  </button>

                  {shouldExpand && (
                    <div class="docs-nav-items">
                      {group.pages.map((item) => (
                        <button
                          key={item.id}
                          class={`docs-nav-item${item.id === activePage ? ' active' : ''}`}
                          onClick={() => setActivePage(item.id)}
                        >
                          {item.title}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )
            })}

            {groupsWithPages.length === 0 && (
              <div class="docs-nav-empty">
                No docs matched <span class="docs-inline-code">{query}</span>.
              </div>
            )}
          </div>
        </aside>
      </div>
    </div>
  )
}

function DocsPage({ pageID, onNavigate }: { pageID: DocsPageID; onNavigate: (pageID: DocsPageID) => void }) {
  switch (pageID) {
    case 'home':
      return (
        <DocsPageLayout
          eyebrow="Docs Portal"
          title="Welcome to the Atlas handbook"
          summary="Everything here is designed to feel like an extension of the control center: practical, visual, and built around real Atlas workflows."
          graphic={<DocsFlowGraphic mode="atlas" />}
        >
          <DocsSection title="Start Here">
            <p>
              Atlas is growing into a full operator environment, so the docs focus on the questions users hit while they
              are actively working: what a feature does, where it lives, what needs setup, and how it connects to the
              rest of the system.
            </p>
            <DocsChecklistCard
              items={[
                'Learn how Atlas routes work between chat, approvals, skills, and output.',
                'Connect your first chat agent through Telegram or Discord.',
                'Use API Keys and Communications together so channels show up correctly.',
                'Return here for troubleshooting, trust-scope guidance, and workflow patterns.',
              ]}
            />
          </DocsSection>

          <DocsSection title="Recommended First Reads">
            <DocsLinkGrid
              links={[
                { title: 'Getting Started', pageID: 'getting-started', description: 'Onboard a new user from blank state to first useful conversation.' },
                { title: 'How Atlas Chat Integrations Work', pageID: 'integrations-overview', description: 'Understand the path from token to conversation routing.' },
                { title: 'Telegram Setup', pageID: 'telegram-setup', description: 'The fastest way to get a live remote chat channel into Atlas.' },
                { title: 'Discord Setup', pageID: 'discord-setup', description: 'Bring Atlas into a server and control delivery rules channel by channel.' },
              ]}
              onNavigate={onNavigate}
            />
          </DocsSection>

          <DocsSection title="Atlas System Map">
            <DocsConceptMap />
          </DocsSection>
        </DocsPageLayout>
      )
    case 'what-atlas-is':
      return (
        <DocsPageLayout
          eyebrow="Introduction"
          title="What Atlas Is"
          summary="Atlas is a personal AI operator that combines conversation, guarded action-taking, reusable skills, and multi-channel delivery inside one local control surface."
          graphic={<DocsSignalPanel title="Core Identity" lines={['Local runtime', 'Operator workflows', 'Human approvals', 'Chat-connected delivery']} />}
        >
          <DocsSection title="What makes Atlas different">
            <p>
              Atlas is not just a chat box. It is meant to coordinate conversations, skills, approvals, workflows, and
              automations from one place, with the user staying in control of sensitive actions.
            </p>
          </DocsSection>
          <DocsSection title="Who it is for">
            <p>
              People who want a capable, personal operator on macOS: someone who needs help with research, action
              routing, scheduling, and connected chat agents without losing visibility into what the system is doing.
            </p>
          </DocsSection>
        </DocsPageLayout>
      )
    case 'how-atlas-works':
      return (
        <DocsPageLayout
          eyebrow="Introduction"
          title="How Atlas Works"
          summary="Atlas runs as a local runtime with a web UI layered on top. Messages and workflows move through skills, approvals, and communication channels before results are delivered."
          graphic={<DocsFlowGraphic mode="runtime" />}
        >
          <DocsSection title="High-level flow">
            <DocsStepList
              steps={[
                'A user message arrives from the in-app chat or a connected platform like Telegram.',
                'Atlas evaluates which skills, tools, or workflows are needed to complete the request.',
                'If an action crosses a safety boundary, it is surfaced in Approvals before execution.',
                'Results are returned to the active conversation and can optionally be delivered to other channels.',
              ]}
            />
          </DocsSection>
          <DocsSection title="Why users should understand this">
            <p>
              When users know the difference between chat, approvals, automations, and communications, they can debug
              problems faster and trust the system more.
            </p>
          </DocsSection>
        </DocsPageLayout>
      )
    case 'core-concepts':
      return (
        <DocsPageLayout
          eyebrow="Introduction"
          title="Core Concepts"
          summary="A shared vocabulary helps users understand where a setup issue ends and where a product behavior begins."
          graphic={<DocsSignalPanel title="Core Terms" lines={['Skills unlock capabilities', 'Workflows package repeatable flows', 'Automations schedule work', 'Communications owns external channels']} />}
        >
          <DocsSection title="Key terms">
            <DocsDefinitionList
              items={[
                ['Skills', 'Capabilities Atlas can call to perform a specific kind of work.'],
                ['Workflows', 'Reusable operator flows with prompt templates, trust scope, and run history.'],
                ['Automations', 'Scheduled prompts or workflow bindings that run without manual triggering.'],
                ['Communications', 'The hub for connected platforms, routing, and discovered channels.'],
                ['Approvals', 'The review queue for actions Atlas cannot take without user sign-off.'],
              ]}
            />
          </DocsSection>
        </DocsPageLayout>
      )
    case 'getting-started':
      return (
        <DocsPageLayout
          eyebrow="Getting Started"
          title="Getting Started"
          summary="The quickest path to a useful Atlas setup is: configure keys, connect a chat agent, confirm routing, then test a live conversation."
          graphic={<DocsFlowGraphic mode="getting-started" />}
        >
          <DocsSection title="Recommended setup order">
            <DocsStepList
              steps={[
                'Open API Keys and add your OpenAI key first so Atlas can reason and respond.',
                'If you want remote chat access, add a Telegram or Discord token next.',
                'Open Communications to validate the platform and discover channels or sessions.',
                'Send a test message from the external platform, then confirm it appears inside Atlas.',
              ]}
            />
          </DocsSection>
          <DocsSection title="Best first milestone">
            <p>
              The best first milestone is not “configure every screen.” It is “get one real message from one real chat
              platform into Atlas, then confirm Atlas can reply back.”
            </p>
          </DocsSection>
        </DocsPageLayout>
      )
    case 'chat':
      return (
        <DocsSimplePage
          eyebrow="Web UI"
          title="Chat"
          summary="The main working surface for conversations, attachments, and direct collaboration with Atlas."
          accessPath="Sidebar > Chat"
          tasks={[
            'Start and continue conversations with Atlas.',
            'Attach images or PDFs for context.',
            'Use chat as the source conversation that can later influence workflows and memory.',
          ]}
          tips={[
            'Use Chat for exploratory work and one-off tasks.',
            'Move to Workflows or Automations when a pattern becomes repeatable.',
          ]}
        />
      )
    case 'communications':
      return (
        <DocsSimplePage
          eyebrow="Web UI"
          title="Communications"
          summary="The control room for connected platforms, routing rules, and discovered sessions or channels."
          accessPath="Sidebar > Communications"
          tasks={[
            'Validate whether Telegram, Discord, or future Slack connections are actually live.',
            'Review discovered channels and active sessions.',
            'Understand which routes Atlas can use for notifications or ongoing replies.',
          ]}
          tips={[
            'If a token is configured but a channel does not appear, check Communications before assuming the integration failed.',
            'This page works together with API Keys, not instead of it.',
          ]}
        />
      )
    case 'approvals':
      return (
        <DocsSimplePage
          eyebrow="Web UI"
          title="Approvals"
          summary="Approvals is where Atlas asks for permission before taking sensitive or execution-level actions."
          accessPath="Sidebar > Approvals"
          tasks={[
            'Review pending actions before Atlas executes them.',
            'Inspect tool calls, arguments, and permission levels.',
            'Approve or deny risky steps as part of Atlas’ trust model.',
          ]}
          tips={[
            'Users should learn to read the intent behind a tool call, not just the tool name.',
            'If Atlas appears stalled, a pending approval is often the reason.',
          ]}
        />
      )
    case 'automations':
      return (
        <DocsSimplePage
          eyebrow="Web UI"
          title="Automations"
          summary="Automations schedule prompts and workflow runs so Atlas can do useful work on a recurring basis."
          accessPath="Sidebar > Control > Automations"
          tasks={[
            'Create scheduled prompts or bind an automation to a saved workflow.',
            'Choose whether successful runs notify a communication destination.',
            'Review run history and iterate on timing or scope.',
          ]}
          tips={[
            'Start with high-value, low-risk automations such as summaries or check-ins.',
            'Use notifications carefully so chat channels stay useful instead of noisy.',
          ]}
        />
      )
    case 'workflows':
      return (
        <DocsSimplePage
          eyebrow="Web UI"
          title="Workflows"
          summary="Workflows package reusable operator behavior with trust scope, prompt templates, and run history."
          accessPath="Sidebar > Control > Workflows"
          tasks={[
            'Create reusable flows with prompt templates and tags.',
            'Define trust scope such as allowed paths or app access.',
            'Review workflow runs and approval status over time.',
          ]}
          tips={[
            'Use Workflows when a useful chat prompt is becoming repeatable.',
            'Trust scope should be explicit so users understand what a workflow is allowed to touch.',
          ]}
        />
      )
    case 'ai-providers':
      return (
        <DocsPageLayout
          eyebrow="Getting Started"
          title="AI Provider Billing"
          summary="Atlas supports four AI providers. They have very different billing models — understanding this saves you from unexpected errors or costs."
          graphic={<DocsSignalPanel title="Provider Summary" lines={['Gemini — free tier available', 'LM Studio — fully local, no cost', 'OpenAI — trial credits, then pay-as-you-go', 'Anthropic — trial credits, then pay-as-you-go']} />}
        >
          <DocsSection title="Google Gemini">
            <p>
              Gemini has a genuine free tier. Gemini 2.0 Flash — the default Atlas uses — is free within daily and per-minute rate limits. For normal personal use you are unlikely to hit those limits. No payment method is required to get started. If you need higher throughput, paid usage is very cheap (~$0.10 per million tokens).
            </p>
          </DocsSection>
          <DocsSection title="LM Studio (Local)">
            <p>
              LM Studio runs models entirely on your own machine. There are no API costs at all. The only requirement is enough RAM and GPU to run the model you choose. This is the right choice if privacy or offline access matters, or if you want to avoid API costs entirely.
            </p>
            <DocsChecklistCard
              items={[
                'Start LM Studio and load a model before connecting Atlas.',
                'Atlas fetches the available model list automatically — choose your active model from the dropdown in Settings.',
                'If LM Studio is configured to require authentication, add the API key in the Advanced section of the LM Studio settings card.',
                'Local models use lower defaults (2 max iterations, 10-turn context window) for speed. You can adjust these in the Advanced section.',
              ]}
            />
          </DocsSection>
          <DocsSection title="OpenAI">
            <DocsChecklistCard
              items={[
                'New accounts get a small amount of free trial credits (around $5) — enough to test.',
                'Once trial credits are exhausted, you must add a payment method to continue.',
                'ChatGPT Plus ($20/month) is a completely separate product and does not give API credits.',
                'GPT-4.1 costs roughly $2–$8 per million tokens. Personal use is pennies per day.',
              ]}
            />
          </DocsSection>
          <DocsSection title="Anthropic (Claude)">
            <DocsChecklistCard
              items={[
                'Same model as OpenAI — trial credits on signup, payment method required after.',
                'No free tier. If your account balance is $0 and there is no card on file, calls will fail.',
                'Claude Sonnet 4.6 costs around $3 per million input tokens.',
                'Check console.anthropic.com → Billing to see your balance and add a payment method.',
              ]}
            />
          </DocsSection>
          <DocsSection title="What to use">
            <DocsDefinitionList
              items={[
                ['Just getting started', 'Use Gemini — it works immediately with no billing setup.'],
                ['Privacy or offline use', 'Use LM Studio — no data leaves your machine.'],
                ['Best general-purpose quality', 'OpenAI or Anthropic with a small balance loaded.'],
                ['Errors after switching', 'Check the Credentials screen — the status dot shows whether the key is valid and connected.'],
              ]}
            />
          </DocsSection>
        </DocsPageLayout>
      )
    case 'api-keys':
      return (
        <DocsPageLayout
          eyebrow="Web UI"
          title="Credentials"
          summary="Credentials is where users unlock Atlas providers and platform integrations, including OpenAI, Telegram, and Discord."
          graphic={<DocsSignalPanel title="Credential Flow" lines={['Get token from provider', 'Paste into Atlas', 'Validate in Communications', 'Send a real test message']} />}
        >
          <DocsSection title="How to access it">
            <p class="docs-access-path">Sidebar &gt; Settings &gt; Credentials</p>
          </DocsSection>
          <DocsSection title="What belongs here">
            <DocsChecklistCard
              items={[
                'OpenAI key for Atlas responses and agent reasoning.',
                'Telegram bot token for Telegram chat integration.',
                'Discord bot token for Discord integration.',
                'Other provider or custom keys as Atlas grows.',
              ]}
            />
          </DocsSection>
          <DocsSection title="Important mindset">
            <p>
              Adding a token here does not always mean the integration is fully live. After saving credentials, users
              should always open Communications to validate the platform and discover real channels.
            </p>
          </DocsSection>
        </DocsPageLayout>
      )
    case 'integrations-overview':
      return (
        <DocsPageLayout
          eyebrow="Integrations"
          title="How Atlas Chat Integrations Work"
          summary="Chat integrations are a chain, not a single toggle. Users need to understand credentials, platform validation, channel discovery, and delivery routing as separate steps."
          graphic={<DocsFlowGraphic mode="integrations" />}
        >
          <DocsSection title="The integration chain">
            <DocsDefinitionList
              items={[
                ['API Keys', 'Stores the credentials Atlas needs to authenticate with a platform.'],
                ['Communications', 'Shows whether the platform is connected and which channels or sessions Atlas can see.'],
                ['Routing and Sessions', 'Maps external chats to Atlas conversations and delivery targets.'],
                ['Notifications and Delivery', 'Controls where automation outputs and agent replies should go.'],
              ]}
            />
          </DocsSection>
          <DocsSection title="The user mental model">
            <DocsStepList
              steps={[
                'Create or access the bot/app on the external platform.',
                'Copy the required token or credentials.',
                'Paste the credentials into Atlas under API Keys.',
                'Validate the platform and discover channels in Communications.',
                'Send a test message to confirm Atlas can receive and reply.',
              ]}
            />
          </DocsSection>
        </DocsPageLayout>
      )
    case 'telegram-setup':
      return (
        <DocsPageLayout
          eyebrow="Integrations"
          title="Telegram Setup"
          summary="Telegram is usually the fastest remote chat path into Atlas. The core flow is: create a bot with BotFather, add the token to Atlas, then send the bot a message so Communications can discover the chat."
          graphic={<DocsFlowGraphic mode="telegram" />}
        >
          <DocsSection title="Before you start">
            <DocsChecklistCard
              items={[
                'A Telegram account on mobile or desktop.',
                'Access to @BotFather inside Telegram.',
                'An OpenAI key already configured in Atlas, so replies can be generated.',
                'Atlas open to API Keys and Communications during setup.',
              ]}
            />
          </DocsSection>
          <DocsSection title="Step-by-step setup">
            <DocsStepList
              steps={[
                'Open Telegram and start a chat with @BotFather.',
                'Run /newbot and follow the prompts to name the bot and choose a unique username ending in bot.',
                'Copy the bot token BotFather returns. Treat it like a password.',
                'In Atlas, open API Keys and add the token under Telegram Bot.',
                'Open Communications and confirm the Telegram platform shows as available or connected.',
                'Start a chat with your new bot in Telegram and send any message, even something simple like “hello”.',
                'Reopen or refresh Communications so Atlas can discover the chat session.',
              ]}
            />
          </DocsSection>
          <DocsSection title="Verify it works">
            <DocsVerificationCard
              checks={[
                'The Telegram provider is marked as configured in API Keys.',
                'Communications shows Telegram as enabled and the session list is no longer empty.',
                'A real Telegram chat appears in Sessions / Channels.',
                'Atlas can reply to a test message from Telegram.',
              ]}
            />
          </DocsSection>
          <DocsSection title="Where users usually get stuck">
            <DocsDefinitionList
              items={[
                ['No channel appears', 'The bot has a token, but nobody has messaged it yet. Send the bot a direct message first.'],
                ['Token saved, still disconnected', 'Check that the token came from BotFather and was pasted without extra spaces.'],
                ['Atlas sees the chat but does not answer', 'Confirm your OpenAI key is present and the Atlas runtime is healthy.'],
              ]}
            />
          </DocsSection>
        </DocsPageLayout>
      )
    case 'discord-setup':
      return (
        <DocsPageLayout
          eyebrow="Integrations"
          title="Discord Setup"
          summary="Discord setup takes a few more platform steps than Telegram, but it gives Atlas a strong home in shared servers and structured channels."
          graphic={<DocsFlowGraphic mode="discord" />}
        >
          <DocsSection title="Before you start">
            <DocsChecklistCard
              items={[
                'A Discord account with permission to create or manage applications.',
                'Access to the server where Atlas should live.',
                'Atlas open to API Keys and Communications for testing.',
              ]}
            />
          </DocsSection>
          <DocsSection title="Step-by-step setup">
            <DocsStepList
              steps={[
                'Open the Discord Developer Portal and create a new application.',
                'Inside the application, add a Bot and generate or reveal the bot token.',
                'Copy the token and store it securely. Regenerate it immediately if it is exposed.',
                'If Atlas needs message content access, enable the necessary privileged intents in the bot settings.',
                'Use the OAuth or installation flow to invite the bot to your target server with the permissions Atlas needs.',
                'Open Atlas, go to API Keys, and paste the token into Discord Bot.',
                'Open Communications and validate that Discord is available and connected.',
                'Send a test message in the allowed server or channel to confirm discovery and reply behavior.',
              ]}
            />
          </DocsSection>
          <DocsSection title="Verify it works">
            <DocsVerificationCard
              checks={[
                'The bot is present in the expected server.',
                'Atlas shows Discord as connected in Communications.',
                'A channel or session appears after the first real message.',
                'Atlas can answer from the server or DM route you expect.',
              ]}
            />
          </DocsSection>
          <DocsSection title="Discord-specific gotchas">
            <DocsDefinitionList
              items={[
                ['Bot is online but silent', 'Check missing gateway intents, channel permissions, or server install scope.'],
                ['Wrong channel behavior', 'Review routing and session mapping once multiple channels are discovered.'],
                ['Connection breaks after token changes', 'Rotate the token in Atlas immediately after regenerating it in Discord.'],
              ]}
            />
          </DocsSection>
        </DocsPageLayout>
      )
    case 'slack-setup':
      return (
        <DocsPageLayout
          eyebrow="Integrations"
          title="Slack Setup"
          summary="Slack support is in active development. This page is reserved so the docs structure is ready the moment the integration lands."
          graphic={<DocsComingSoonCard title="In Active Development" copy="The Slack guide will expand once the integration flow, credential model, and channel behavior are finalized." />}
        >
          <DocsSection title="What will live here">
            <DocsChecklistCard
              items={[
                'How to create a Slack app and choose workspace install scope.',
                'Which OAuth scopes or bot permissions Atlas needs.',
                'Where to get the bot token or signing credentials.',
                'How Atlas will discover channels, DMs, and delivery targets.',
              ]}
            />
          </DocsSection>
          <DocsSection title="For now">
            <p>
              Users should treat Slack support as planned but not yet documented. Telegram and Discord remain the fully
              documented chat integrations for the current docs release.
            </p>
          </DocsSection>
        </DocsPageLayout>
      )
    case 'routing-sessions':
      return (
        <DocsPageLayout
          eyebrow="Integrations"
          title="Routing and Sessions"
          summary="Routing determines how external channels map to Atlas conversations and where Atlas sends replies or automation output once a platform is connected."
          graphic={<DocsSignalPanel title="Routing Logic" lines={['Channel discovered', 'Conversation linked', 'Destination selected', 'Reply delivered']} />}
        >
          <DocsSection title="What users should know">
            <p>
              A platform connection does not automatically answer the deeper question of “where will this reply go?”
              Routing and sessions answer that. Once channels are discovered, Atlas needs a stable conversation and a
              destination strategy.
            </p>
          </DocsSection>
          <DocsSection title="Common outcomes">
            <DocsDefinitionList
              items={[
                ['One channel, one conversation', 'A clean default for most personal setups.'],
                ['Multiple channels, separate sessions', 'Useful when the user wants isolated workstreams.'],
                ['Automation delivery target', 'Best when users want summaries or outputs returned to a specific chat.'],
              ]}
            />
          </DocsSection>
        </DocsPageLayout>
      )
    case 'notifications-delivery':
      return (
        <DocsPageLayout
          eyebrow="Integrations"
          title="Notifications and Delivery"
          summary="Atlas can route automation results and other outputs back to discovered communication channels, but the destination needs to be intentional."
          graphic={<DocsSignalPanel title="Delivery Loop" lines={['Automation runs', 'Output generated', 'Destination selected', 'User receives result']} />}
        >
          <DocsSection title="How it works">
            <p>
              Once a communication channel exists, Atlas can use it as a delivery target for successful automation runs
              or future notification patterns. That makes Communications and Automations tightly connected.
            </p>
          </DocsSection>
          <DocsSection title="Best practices">
            <DocsChecklistCard
              items={[
                'Send high-value updates, not every internal event.',
                'Use one destination per automation when clarity matters.',
                'Review noisy delivery patterns early so users do not mute Atlas.',
              ]}
            />
          </DocsSection>
        </DocsPageLayout>
      )
    case 'integration-troubleshooting':
      return (
        <DocsPageLayout
          eyebrow="Integrations"
          title="Integration Troubleshooting"
          summary="Most integration issues come from one of four layers: credentials, platform permissions, channel discovery, or Atlas runtime health."
          graphic={<DocsSignalPanel title="Troubleshooting Stack" lines={['Credentials', 'Platform config', 'Discovery and routing', 'Runtime health']} />}
        >
          <DocsSection title="Fast triage checklist">
            <DocsStepList
              steps={[
                'Confirm the token exists in API Keys and came from the correct provider flow.',
                'Open Communications and verify the platform is available or connected.',
                'Send a fresh real message from the external platform so Atlas has something to discover.',
                'Check Activity for runtime errors if the integration appears connected but silent.',
              ]}
            />
          </DocsSection>
          <DocsSection title="Common failure modes">
            <DocsDefinitionList
              items={[
                ['Saved token, no connection', 'Credential is wrong, expired, or missing a platform-side enablement step.'],
                ['Connected, no channels', 'Atlas has not yet seen a real incoming message or lacks visibility into the target channel.'],
                ['Channels exist, no reply', 'The runtime, model access, or routing path is broken further downstream.'],
              ]}
            />
          </DocsSection>
        </DocsPageLayout>
      )
    case 'github':
      return (
        <DocsPageLayout
          eyebrow="Resources"
          title="GitHub"
          summary="Use this space to point users toward the source repository, issue tracking, release notes, and contribution guidance once the public repo details are finalized."
          graphic={<DocsComingSoonCard title="Repository Link Pending" copy="Add the production GitHub URL here when the repo destination is finalized for users." />}
        >
          <DocsSection title="What this page should eventually include">
            <DocsChecklistCard
              items={[
                'Primary GitHub repository link.',
                'Issue reporting guidance.',
                'Release notes or changelog entry point.',
                'Contribution guidelines if the project becomes public.',
              ]}
            />
          </DocsSection>
        </DocsPageLayout>
      )
  }
}

function DocsSimplePage({
  eyebrow,
  title,
  summary,
  accessPath,
  tasks,
  tips,
}: {
  eyebrow: string
  title: string
  summary: string
  accessPath: string
  tasks: string[]
  tips: string[]
}) {
  return (
    <DocsPageLayout
      eyebrow={eyebrow}
      title={title}
      summary={summary}
      graphic={<DocsSignalPanel title={title} lines={tasks.slice(0, 4)} />}
    >
      <DocsSection title="How to access it">
        <p class="docs-access-path">{accessPath}</p>
      </DocsSection>
      <DocsSection title="What users do here">
        <DocsChecklistCard items={tasks} />
      </DocsSection>
      <DocsSection title="Tips and edge cases">
        <DocsChecklistCard items={tips} />
      </DocsSection>
    </DocsPageLayout>
  )
}

function DocsPageLayout({
  eyebrow,
  title,
  summary,
  graphic,
  children,
}: {
  eyebrow: string
  title: string
  summary: string
  graphic: preact.ComponentChild
  children: preact.ComponentChild
}) {
  return (
    <div class="docs-page">
      <section class="card docs-hero-card">
        <div class="docs-hero-copy">
          <div class="surface-eyebrow">{eyebrow}</div>
          <h2 class="docs-hero-title">{title}</h2>
          <p class="docs-hero-summary">{summary}</p>
        </div>
        <div class="docs-hero-graphic">{graphic}</div>
      </section>
      {children}
    </div>
  )
}

function DocsSection({ title, children }: { title: string; children: preact.ComponentChild }) {
  return (
    <section class="docs-section">
      <div class="section-label">{title}</div>
      <div class="docs-section-card surface-card-soft">{children}</div>
    </section>
  )
}

function DocsStepList({ steps }: { steps: string[] }) {
  return (
    <ol class="docs-step-list">
      {steps.map((step, index) => (
        <li key={step}>
          <span class="docs-step-index">{index + 1}</span>
          <span>{step}</span>
        </li>
      ))}
    </ol>
  )
}

function DocsChecklistCard({ items }: { items: string[] }) {
  return (
    <div class="docs-checklist">
      {items.map((item) => (
        <div class="docs-checklist-item" key={item}>
          <span class="docs-checklist-dot" />
          <span>{item}</span>
        </div>
      ))}
    </div>
  )
}

function DocsDefinitionList({ items }: { items: [string, string][] }) {
  return (
    <div class="docs-definition-list">
      {items.map(([term, description]) => (
        <div class="docs-definition-row" key={term}>
          <div class="docs-definition-term">{term}</div>
          <div class="docs-definition-copy">{description}</div>
        </div>
      ))}
    </div>
  )
}

function DocsVerificationCard({ checks }: { checks: string[] }) {
  return (
    <div class="docs-verification">
      {checks.map((item) => (
        <div class="docs-verification-item" key={item}>
          <span class="docs-verification-icon">✓</span>
          <span>{item}</span>
        </div>
      ))}
    </div>
  )
}

function DocsLinkGrid({
  links,
  onNavigate,
}: {
  links: Array<{ title: string; description: string; pageID: DocsPageID }>
  onNavigate: (pageID: DocsPageID) => void
}) {
  return (
    <div class="docs-link-grid">
      {links.map((link) => (
        <button key={link.pageID} class="docs-link-card surface-card-soft" onClick={() => onNavigate(link.pageID)}>
          <div class="surface-title">{link.title}</div>
          <div class="surface-copy">{link.description}</div>
        </button>
      ))}
    </div>
  )
}

function DocsSignalPanel({ title, lines }: { title: string; lines: string[] }) {
  return (
    <div class="docs-signal-panel">
      <div class="docs-signal-title">{title}</div>
      <div class="docs-signal-lines">
        {lines.map((line) => (
          <div class="docs-signal-line" key={line}>
            <span class="docs-signal-pip" />
            <span>{line}</span>
          </div>
        ))}
      </div>
    </div>
  )
}

function DocsComingSoonCard({ title, copy }: { title: string; copy: string }) {
  return (
    <div class="docs-coming-soon">
      <div class="surface-eyebrow">Placeholder</div>
      <div class="docs-coming-soon-title">{title}</div>
      <div class="surface-copy">{copy}</div>
    </div>
  )
}

function DocsConceptMap() {
  const nodes = [
    { title: 'Chat', copy: 'Live conversations and attachments' },
    { title: 'Approvals', copy: 'Human sign-off for risky actions' },
    { title: 'Skills', copy: 'Capability layer Atlas can invoke' },
    { title: 'Automations', copy: 'Scheduled prompts and workflow runs' },
    { title: 'Communications', copy: 'Platform links, sessions, and delivery' },
  ]

  return (
    <div class="docs-concept-map">
      {nodes.map((node) => (
        <div key={node.title} class="docs-concept-node">
          <div class="surface-title">{node.title}</div>
          <div class="surface-copy">{node.copy}</div>
        </div>
      ))}
    </div>
  )
}

function DocsFlowGraphic({ mode }: { mode: 'atlas' | 'runtime' | 'getting-started' | 'integrations' | 'telegram' | 'discord' }) {
  const labels = (() => {
    switch (mode) {
      case 'runtime':
        return ['Message', 'Skills', 'Approvals', 'Result']
      case 'getting-started':
        return ['OpenAI Key', 'Chat Agent', 'Communications', 'Test Message']
      case 'integrations':
        return ['Token', 'API Keys', 'Communications', 'Reply']
      case 'telegram':
        return ['BotFather', 'Bot Token', 'Atlas', 'Telegram Chat']
      case 'discord':
        return ['Discord App', 'Bot Token', 'Atlas', 'Server Channel']
      case 'atlas':
      default:
        return ['Chat', 'Skills', 'Workflows', 'Delivery']
    }
  })()

  return (
    <div class="docs-flow-graphic">
      {labels.map((label, index) => (
        <div class="docs-flow-node" key={label}>
          <span>{label}</span>
          {index < labels.length - 1 && <div class="docs-flow-link" />}
        </div>
      ))}
    </div>
  )
}
