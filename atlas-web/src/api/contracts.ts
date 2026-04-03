export interface RuntimeStatus {
  isRunning: boolean
  activeConversationCount: number
  lastMessageAt?: string
  lastError?: string
  state: string
  runtimePort: number
  startedAt?: string
  activeRequests: number
  pendingApprovalCount: number
  details: string
  tokensIn?: number
  tokensOut?: number
  telegram?: {
    enabled: boolean
    connected: boolean
    pollingActive: boolean
    lastError?: string
  }
  communications?: CommunicationsSnapshot
}

export interface APIKeyStatus {
  openAIKeySet: boolean
  telegramTokenSet: boolean
  discordTokenSet: boolean
  slackBotTokenSet: boolean
  slackAppTokenSet: boolean
  braveSearchKeySet: boolean
  anthropicKeySet: boolean
  geminiKeySet: boolean
  lmStudioKeySet: boolean
  ollamaKeySet: boolean
  finnhubKeySet: boolean
  customKeys: string[]
}

export interface RuntimeConfig {
  runtimePort: number
  onboardingCompleted: boolean
  telegramEnabled: boolean
  discordEnabled: boolean
  discordClientID: string
  slackEnabled: boolean
  telegramPollingTimeoutSeconds: number
  telegramPollingRetryBaseSeconds: number
  telegramCommandPrefix: string
  telegramAllowedUserIDs: number[]
  telegramAllowedChatIDs: number[]
  defaultOpenAIModel: string
  baseSystemPrompt: string
  maxAgentIterations: number
  conversationWindowLimit: number
  memoryEnabled: boolean
  maxRetrievedMemoriesPerTurn: number
  memoryAutoSaveThreshold: number
  personaName: string
  actionSafetyMode: string
  activeImageProvider: string
  activeAIProvider: string
  lmStudioBaseURL: string
  selectedAnthropicModel: string
  selectedGeminiModel: string
  selectedOpenAIPrimaryModel: string
  selectedOpenAIFastModel: string
  selectedAnthropicFastModel: string
  selectedGeminiFastModel: string
  selectedLMStudioModel: string
  selectedLMStudioModelFast: string
  lmStudioContextWindowLimit: number
  lmStudioMaxAgentIterations: number
  ollamaBaseURL: string
  selectedOllamaModel: string
  selectedOllamaModelFast: string
  ollamaContextWindowLimit: number
  ollamaMaxAgentIterations: number
  atlasEnginePort: number
  selectedAtlasEngineModel: string
  selectedAtlasEngineModelFast: string
  atlasEngineContextWindowLimit: number
  atlasEngineMaxAgentIterations: number
  atlasEngineCtxSize: number
  atlasEngineKVCacheQuant: string
  atlasEngineRouterPort: number
  atlasEngineRouterModel: string
  atlasEngineRouterForAll: boolean
  enableSmartToolSelection: boolean
  toolSelectionMode: string
  enableMultiAgentOrchestration: boolean
  maxParallelAgents: number
  workerMaxIterations: number
  remoteAccessEnabled: boolean
  tailscaleEnabled: boolean
}

export interface RuntimeConfigUpdateResponse {
  config: RuntimeConfig
  restartRequired: boolean
}

export interface OnboardingStatus {
  completed: boolean
}

export interface MessageAttachment {
  filename: string
  mimeType: string
  data: string
}

export interface MessageResponse {
  conversation: {
    id: string
    messages: Array<{
      id: string
      role: 'user' | 'assistant'
      content: string
      timestamp: string
    }>
  }
  response: {
    assistantMessage?: string
    status: string
    errorMessage?: string
  }
}

export interface ApprovalToolCall {
  id: string
  toolName: string
  argumentsJSON: string
  permissionLevel: 'read' | 'draft' | 'execute' | string
  requiresApproval: boolean
  status?: string
  timestamp?: string
}

export interface Approval {
  id: string
  status: 'pending' | 'approved' | 'denied' | string
  conversationID?: string
  createdAt: string
  resolvedAt?: string
  deferredExecutionID?: string
  deferredExecutionStatus?: string
  lastError?: string
  previewDiff?: string
  toolCall: ApprovalToolCall
}

export interface FsRoot {
  id: string
  path: string
}

export interface SkillRecord {
  manifest: {
    id: string
    name: string
    version: string
    description: string
    lifecycleState: string
    riskLevel: string
    isUserVisible: boolean
    category?: string
    source?: string
    capabilities: string[]
    tags: string[]
  }
  actions: Array<{
    id: string
    name: string
    description: string
    permissionLevel: string
    approvalPolicy: string
    isEnabled: boolean
  }>
  validation?: {
    skillID: string
    status: string
    summary: string
    isValid: boolean
    issues: string[]
    validatedAt: string
  }
}

export interface MemoryItem {
  id: string
  category: string
  title: string
  content: string
  source?: string
  confidence: number
  importance: number
  isUserConfirmed: boolean
  isSensitive: boolean
  tags: string[]
  createdAt: string
  updatedAt: string
}

export interface MemoryParams {
  category?: string
  limit?: number
}

export interface LogEntry {
  id: string
  level: string
  message: string
  timestamp: string
  metadata?: Record<string, string>
}

export interface TelegramChat {
  chatID: number
  userID?: number
  activeConversationID: string
  createdAt: string
  updatedAt: string
  lastTelegramMessageID?: number
}

export interface CommunicationDestination {
  id: string
  platform: 'telegram' | 'discord' | 'slack' | 'whatsapp' | 'companion'
  channelID: string
  channelName?: string
  userID?: string
  threadID?: string
}

export interface CommunicationChannel {
  id: string
  platform: 'telegram' | 'discord' | 'slack' | 'whatsapp' | 'companion'
  channelID: string
  channelName?: string
  userID?: string
  threadID?: string
  activeConversationID: string
  createdAt: string
  updatedAt: string
  lastMessageID?: string
  canReceiveNotifications: boolean
}

export interface CommunicationPlatformStatus {
  id: string
  platform: 'telegram' | 'discord' | 'slack' | 'whatsapp' | 'companion'
  enabled: boolean
  connected: boolean
  available: boolean
  setupState: 'not_started' | 'missing_credentials' | 'partial_setup' | 'validation_failed' | 'ready'
  statusLabel: string
  connectedAccountName?: string
  credentialConfigured: boolean
  blockingReason?: string
  requiredCredentials: string[]
  lastError?: string
  lastUpdatedAt?: string
  metadata: Record<string, string>
}

export interface CommunicationsSnapshot {
  platforms: CommunicationPlatformStatus[]
  channels: CommunicationChannel[]
}

export interface CommunicationValidationPayload {
  credentials?: Record<string, string>
  config?: {
    discordClientID?: string
  }
}

export interface CommunicationSetupValues {
  values: Record<string, string>
}

export interface GremlinItem {
  id: string
  name: string
  emoji: string
  prompt: string
  scheduleRaw: string
  isEnabled: boolean
  sourceType: string
  createdAt: string
  workflowID?: string
  workflowInputValues?: Record<string, string>
  nextRunAt?: string
  lastRunAt?: string
  lastRunStatus?: string
  telegramChatID?: number
  communicationDestination?: CommunicationDestination
}

export type ForgeProposalStatus = 'pending' | 'installed' | 'enabled' | 'rejected' | 'uninstalled'

export interface ForgeProposalRecord {
  id: string
  skillID: string
  name: string
  description: string
  summary: string
  rationale?: string
  requiredSecrets: string[]
  domains: string[]
  actionNames: string[]
  riskLevel: string
  status: ForgeProposalStatus
  specJSON: string
  plansJSON: string
  contractJSON?: string
  createdAt: string
  updatedAt: string
}

export interface ForgeResearchingItem {
  id: string
  title: string
  message: string
  startedAt: string
}

// ── Engine LM ───────────────────────────────────────────────────────────

export interface EngineStatus {
  running: boolean
  loadedModel: string
  port: number
  binaryReady: boolean
  buildVersion?: string
  lastError?: string
  lastTPS?: number
  contextTokens?: number
}

export interface EngineModelInfo {
  name: string
  sizeBytes: number
  sizeHuman: string
}

export interface EngineDownloadProgress {
  downloaded: number
  total: number
  percent: number
}

// ── Model Selector ────────────────────────────────────────────────────────────

export interface AIModelRecord {
  id: string
  displayName: string
  isFast: boolean
}

export interface ModelSelectorInfo {
  primaryModel?: string
  fastModel?: string
  lastRefreshedAt?: string
  availableModels?: AIModelRecord[]
}

export interface GremlinRun {
  id: string
  gremlinID: string
  startedAt: string
  finishedAt?: string
  status: 'success' | 'failed' | 'running' | 'skipped' | string
  output?: string
  errorMessage?: string
  conversationID?: string
  workflowRunID?: string
}

export interface WorkflowTrustScope {
  approvedRootPaths: string[]
  allowedApps: string[]
  allowsSensitiveRead: boolean
  allowsLiveWrite: boolean
}

export interface WorkflowStep {
  id: string
  title: string
  kind: 'skill_action' | 'prompt' | string
  skillID?: string
  actionID?: string
  inputJSON?: string
  prompt?: string
  appName?: string
  targetPath?: string
  sideEffectLevel?: string
}

export interface WorkflowDefinition {
  id: string
  name: string
  description: string
  promptTemplate: string
  tags: string[]
  steps: WorkflowStep[]
  trustScope: WorkflowTrustScope
  approvalMode: 'workflow_boundary' | 'step_by_step' | string
  createdAt: string
  updatedAt: string
  sourceConversationID?: string
  isEnabled: boolean
}

export interface WorkflowApproval {
  id: string
  workflowID: string
  workflowRunID: string
  status: 'pending' | 'approved' | 'denied' | string
  reason: string
  requestedAt: string
  resolvedAt?: string
  trustScope: WorkflowTrustScope
}

export interface WorkflowStepRun {
  id: string
  stepID: string
  title: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'waiting_for_approval' | 'skipped' | string
  output?: string
  errorMessage?: string
  startedAt?: string
  finishedAt?: string
}

export interface WorkflowRun {
  id: string
  workflowID: string
  workflowName: string
  status: 'pending' | 'running' | 'waiting_for_approval' | 'completed' | 'failed' | 'denied' | string
  outcome?: 'success' | 'failed' | 'waiting_for_approval' | 'denied' | string
  inputValues: Record<string, string>
  stepRuns: WorkflowStepRun[]
  approval?: WorkflowApproval
  assistantSummary?: string
  errorMessage?: string
  startedAt: string
  finishedAt?: string
  conversationID?: string
}

export interface ConversationSummary {
  id: string
  messageCount: number
  firstUserMessage?: string
  lastAssistantMessage?: string
  createdAt: string
  updatedAt: string
  platformContext?: string
}

export interface ConversationMessage {
  id: string
  role: 'user' | 'assistant' | 'system' | 'tool'
  content: string
  timestamp: string
}

export interface ConversationDetail extends ConversationSummary {
  messages: ConversationMessage[]
}

export interface LinkPreview {
  url: string
  title?: string
  description?: string
  imageURL?: string
  domain?: string
}

export interface WidgetField {
  key: string
  label: string
  type: 'text' | 'number' | 'select' | 'date'
  required: boolean
  options?: string[]
}

export interface DashboardWidgetBinding {
  valuePath?: string
  itemsPath?: string
  rowsPath?: string
  primaryTextPath?: string
  secondaryTextPath?: string
  tertiaryTextPath?: string
  linkPath?: string
  imagePath?: string
  summaryPath?: string
}

export interface DashboardWidget {
  id: string
  type: 'stat_card' | 'summary' | 'list' | 'table' | 'form' | 'search'
  title: string
  skillID: string
  action?: string
  dataKey?: string
  defaultInputs?: Record<string, string>
  binding?: DashboardWidgetBinding
  fields?: WidgetField[]
  columns?: string[]
  emptyMessage?: string
}

export interface DashboardDisplayItem {
  primaryText: string
  secondaryText?: string
  tertiaryText?: string
  linkURL?: string
  imageURL?: string
}

export interface DashboardDisplayTableRow {
  values: string[]
}

export interface DashboardDisplayPayload {
  value?: string
  summary?: string
  items?: DashboardDisplayItem[]
  rows?: DashboardDisplayTableRow[]
}

export interface WidgetExecutionResult {
  widgetID: string
  rawOutput: string
  extractedValue?: string
  displayPayload?: DashboardDisplayPayload
  success: boolean
  error?: string
}

export interface DashboardSpec {
  id: string
  title: string
  icon: string
  description: string
  sourceSkillIDs: string[]
  widgets: DashboardWidget[]
  emptyState?: string
  isPinned: boolean
  lastAccessedAt?: string
}

export interface DashboardProposal {
  proposalID: string
  spec: DashboardSpec
  summary: string
  rationale: string
  linkedSkillID?: string
  linkedProposalID?: string
  status: 'pending' | 'installed' | 'rejected'
  createdAt: string
}
