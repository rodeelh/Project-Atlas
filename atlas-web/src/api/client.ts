// Atlas HTTP API client
import type {
  APIKeyStatus,
  Approval,
  EngineModelInfo,
  EngineStatus,
  FsRoot,
  CommunicationChannel,
  CommunicationsSnapshot,
  CommunicationPlatformStatus,
  CommunicationSetupValues,
  CommunicationValidationPayload,
  ConversationDetail,
  ConversationSummary,
  DashboardProposal,
  DashboardSpec,
  ForgeProposalRecord,
  ForgeResearchingItem,
  GremlinItem,
  GremlinRun,
  LinkPreview,
  LogEntry,
  MemoryItem,
  MemoryParams,
  MessageAttachment,
  MessageResponse,
  ModelSelectorInfo,
  OnboardingStatus,
  RuntimeConfig,
  RuntimeConfigUpdateResponse,
  RuntimeStatus,
  SkillRecord,
  TelegramChat,
  WidgetExecutionResult,
  WorkflowDefinition,
  WorkflowRun,
} from './contracts'

export type {
  AIModelRecord,
  APIKeyStatus,
  EngineModelInfo,
  EngineStatus,
  Approval,
  FsRoot,
  ApprovalToolCall,
  CommunicationChannel,
  CommunicationDestination,
  CommunicationsSnapshot,
  CommunicationPlatformStatus,
  CommunicationSetupValues,
  CommunicationValidationPayload,
  ConversationDetail,
  ConversationMessage,
  ConversationSummary,
  DashboardDisplayItem,
  DashboardDisplayPayload,
  DashboardDisplayTableRow,
  DashboardProposal,
  DashboardSpec,
  DashboardWidget,
  DashboardWidgetBinding,
  ForgeProposalRecord,
  ForgeProposalStatus,
  ForgeResearchingItem,
  GremlinItem,
  GremlinRun,
  LinkPreview,
  LogEntry,
  MemoryItem,
  MemoryParams,
  MessageAttachment,
  MessageResponse,
  ModelSelectorInfo,
  OnboardingStatus,
  RuntimeConfig,
  RuntimeConfigUpdateResponse,
  RuntimeStatus,
  SkillRecord,
  TelegramChat,
  WidgetExecutionResult,
  WidgetField,
  WorkflowApproval,
  WorkflowDefinition,
  WorkflowRun,
  WorkflowStep,
  WorkflowStepRun,
  WorkflowTrustScope,
} from './contracts'

export function getPort(): string {
  try { return localStorage.getItem('atlasPort') ?? '1984' } catch { return '1984' }
}

// Derive the base API URL.
// When the page is served from a non-localhost host (LAN IP, Tailscale, etc.) we
// always target that same host — no localStorage needed, no timing race.
// Only fall back to localhost when running in the local menu-bar context.
const BASE = () => {
  if (typeof window !== 'undefined' &&
      window.location.hostname !== 'localhost' &&
      window.location.hostname !== '127.0.0.1') {
    return `http://${window.location.host}`
  }
  try {
    const stored = localStorage.getItem('atlasHost')
    if (stored) return `http://${stored}`
  } catch { /* localStorage blocked */ }
  return `http://localhost:${getPort()}`
}

// ---- HTTP helpers ----

async function request<T>(
  path: string,
  options: RequestInit = {}
): Promise<T> {
  const url = `${BASE()}${path}`
  const res = await fetch(url, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...(options.headers ?? {}),
    },
  })
  if (!res.ok) {
    // On remote device, a 401 means the session expired — redirect to the auth gate
    if (res.status === 401 && typeof window !== 'undefined' &&
        window.location.hostname !== 'localhost' && window.location.hostname !== '127.0.0.1') {
      window.location.href = `http://${window.location.host}/auth/remote-gate`
      throw new Error('Session expired — redirecting to login')
    }
    const text = await res.text().catch(() => res.statusText)
    let message = text
    try { const j = JSON.parse(text); if (j?.error) message = j.error } catch { /* use raw text */ }
    throw new Error(message)
  }
  // Some endpoints return empty bodies (204)
  const text = await res.text()
  return text ? (JSON.parse(text) as T) : ({} as T)
}

function get<T>(path: string, params?: Record<string, string | number | undefined>): Promise<T> {
  let p = path
  if (params) {
    const q = Object.entries(params)
      .filter(([, v]) => v !== undefined)
      .map(([k, v]) => `${encodeURIComponent(k)}=${encodeURIComponent(String(v))}`)
      .join('&')
    if (q) p = `${path}?${q}`
  }
  return request<T>(p, { method: 'GET' })
}

function post<T>(path: string, body: unknown): Promise<T> {
  return request<T>(path, { method: 'POST', body: JSON.stringify(body) })
}

function put<T>(path: string, body: unknown): Promise<T> {
  return request<T>(path, { method: 'PUT', body: JSON.stringify(body) })
}

function del<T>(path: string, body: unknown): Promise<T> {
  return request<T>(path, { method: 'DELETE', body: JSON.stringify(body) })
}

// ---- API surface ----

export const api = {
  status: () => get<RuntimeStatus>('/status'),
  apiKeys: () => get<APIKeyStatus>('/api-keys'),
  setAPIKey: (provider: string, key: string, name?: string) => post<APIKeyStatus>('/api-keys', { provider, key, name }),
  deleteAPIKey: (name: string) => del<APIKeyStatus>('/api-keys', { name }),
  config: () => get<RuntimeConfig>('/config'),
  updateConfig: (c: Partial<RuntimeConfig>) => put<RuntimeConfigUpdateResponse>('/config', c),
  onboardingStatus: () => get<OnboardingStatus>('/onboarding'),
  updateOnboardingStatus: (completed: boolean) => put<OnboardingStatus>('/onboarding', { completed }),
  sendMessage: (conversationID: string, message: string, attachments?: MessageAttachment[]) =>
    post<MessageResponse>('/message', {
      conversationId: conversationID,
      message,
      ...(attachments && attachments.length > 0 ? { attachments } : {}),
    }),
  streamMessage: (conversationID: string) =>
    new EventSource(`${BASE()}/message/stream?conversationID=${encodeURIComponent(conversationID)}`),
  approvals: () => get<Approval[]>('/approvals'),
  // approve/deny take the toolCall.id (toolCallID), not the approval.id
  approve: (toolCallID: string) => post<Approval>(`/approvals/${toolCallID}/approve`, {}),
  deny: (toolCallID: string) => post<Approval>(`/approvals/${toolCallID}/deny`, {}),
  skills: () => get<SkillRecord[]>('/skills'),
  enableSkill: (id: string) => post<SkillRecord>(`/skills/${encodeURIComponent(id)}/enable`, {}),
  disableSkill: (id: string) => post<SkillRecord>(`/skills/${encodeURIComponent(id)}/disable`, {}),
  validateSkill: (id: string) => post<SkillRecord>(`/skills/${encodeURIComponent(id)}/validate`, {}),
  customSkills: () => get<unknown[]>('/skills/custom'),
  installCustomSkill: (path: string) => post<{ id: string; path: string; message: string }>('/skills/install', { path }),
  removeCustomSkill: (id: string) => request<{ id: string; removed: boolean }>(`/skills/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  fsRoots: () => get<FsRoot[]>('/skills/file-system/roots'),
  addFsRoot: (path: string) => post<FsRoot>('/skills/file-system/roots', { path }),
  removeFsRoot: (id: string) => post<FsRoot[]>(`/skills/file-system/roots/${encodeURIComponent(id)}/remove`, {}),
  pickFsFolder: () => post<{ path: string }>('/skills/file-system/pick-folder', {}),
  actionPolicies: () => get<Record<string, string>>('/action-policies'),
  setActionPolicy: (actionID: string, policy: string) =>
    put<Record<string, string>>(`/action-policies/${encodeURIComponent(actionID)}`, { policy }),
  memories: (params?: MemoryParams) =>
    get<MemoryItem[]>('/memories', params as Record<string, string | number | undefined>),
  searchMemories: (query: string) => get<MemoryItem[]>('/memories/search', { query }),
  deleteMemory: (id: string) => post<MemoryItem>(`/memories/${id}/delete`, {}),
  logs: (limit = 100) => get<LogEntry[]>(`/logs?limit=${limit}`),

  // MIND.md
  mind: () => get<{ content: string }>('/mind'),
  updateMind: (content: string) => put<object>('/mind', { content }),
  regenerateMind: () => post<{ content: string }>('/mind/regenerate', {}),

  // Skills Memory
  skillsMemory: () => get<{ content: string }>('/skills-memory'),
  updateSkillsMemory: (content: string) => put<object>('/skills-memory', { content }),

  // Model selector
  models: () => get<ModelSelectorInfo>('/models'),
  modelsForProvider: (provider: string) => get<ModelSelectorInfo>(`/models/available?provider=${provider}`),
  refreshModels: () => post<ModelSelectorInfo>('/models/refresh', {}),

  // Telegram
  telegramChats: () => get<TelegramChat[]>('/telegram/chats'),
  communications: () => get<CommunicationsSnapshot>('/communications'),
  communicationSetupValues: (platform: string) => get<CommunicationSetupValues>(`/communications/platforms/${encodeURIComponent(platform)}/setup`),
  communicationChannels: () => get<CommunicationChannel[]>('/communications/channels'),
  updateCommunicationPlatform: (platform: string, enabled: boolean) =>
    put<CommunicationPlatformStatus>(`/communications/platforms/${encodeURIComponent(platform)}`, { enabled }),
  validateCommunicationPlatform: (platform: string, payload?: CommunicationValidationPayload) =>
    post<CommunicationPlatformStatus>(`/communications/platforms/${encodeURIComponent(platform)}/validate`, payload ?? {}),

  // Automations (Gremlins)
  automations: () => get<GremlinItem[]>('/automations'),
  automationsFile: () => get<{ content: string }>('/automations/file'),
  writeAutomationsFile: (content: string) => put<object>('/automations/file', { content }),
  createAutomation: (item: Omit<GremlinItem, 'id'> & { id?: string }) =>
    post<GremlinItem>('/automations', item),
  updateAutomation: (item: GremlinItem) =>
    put<GremlinItem>(`/automations/${item.id}`, item),
  deleteAutomation: (id: string) => request<object>(`/automations/${id}`, { method: 'DELETE' }),
  enableAutomation: (id: string) => post<GremlinItem>(`/automations/${id}/enable`, {}),
  disableAutomation: (id: string) => post<GremlinItem>(`/automations/${id}/disable`, {}),
  runAutomationNow: (id: string) => post<GremlinRun>(`/automations/${id}/run`, {}),
  automationRuns: (id: string) => get<GremlinRun[]>(`/automations/${id}/runs`),
  workflows: () => get<WorkflowDefinition[]>('/workflows'),
  workflow: (id: string) => get<WorkflowDefinition>(`/workflows/${encodeURIComponent(id)}`),
  createWorkflow: (definition: WorkflowDefinition) => post<WorkflowDefinition>('/workflows', definition),
  updateWorkflow: (definition: WorkflowDefinition) => put<WorkflowDefinition>(`/workflows/${encodeURIComponent(definition.id)}`, definition),
  deleteWorkflow: (id: string) => request<WorkflowDefinition>(`/workflows/${encodeURIComponent(id)}`, { method: 'DELETE' }),
  runWorkflow: (id: string, inputValues?: Record<string, string>) =>
    post<WorkflowRun>(`/workflows/${encodeURIComponent(id)}/run`, { inputValues }),
  workflowRuns: (id?: string) => get<WorkflowRun[]>(id ? `/workflows/${encodeURIComponent(id)}/runs` : '/workflows/runs'),
  approveWorkflowRun: (runID: string) => post<WorkflowRun>(`/workflows/runs/${encodeURIComponent(runID)}/approve`, {}),
  denyWorkflowRun: (runID: string) => post<WorkflowRun>(`/workflows/runs/${encodeURIComponent(runID)}/deny`, {}),

  // Dashboards
  executeWidget: (dashboardID: string, widgetID: string, inputs?: Record<string, string>) =>
    post<WidgetExecutionResult>('/dashboards/widgets/execute', { dashboardID, widgetID, inputs }),
  dashboardProposals: () => get<DashboardProposal[]>('/dashboards/proposals'),
  createDashboardProposal: (intent: string, skillIDs: string[]) => post<DashboardProposal>('/dashboards/proposals', { intent, skillIDs }),
  installDashboard: (proposalID: string) => post<DashboardProposal>('/dashboards/install', { proposalID }),
  rejectDashboard: (proposalID: string) => post<DashboardProposal>('/dashboards/reject', { proposalID }),
  installedDashboards: () => get<DashboardSpec[]>('/dashboards/installed'),
  removeDashboard: (dashboardID: string) => del<{ ok: boolean }>('/dashboards/installed', { dashboardID }),
  recordDashboardAccess: (dashboardID: string) => post<{ ok: boolean }>('/dashboards/access', { dashboardID }),
  toggleDashboardPin: (dashboardID: string) => post<DashboardSpec>('/dashboards/pin', { dashboardID }),

  // Conversation History
  conversations: (limit = 50, offset = 0) =>
    get<ConversationSummary[]>('/conversations', { limit, offset }),
  searchConversations: (query: string, limit = 50) =>
    get<ConversationSummary[]>('/conversations/search', { query, limit }),
  conversationDetail: (id: string) =>
    get<ConversationDetail>(`/conversations/${encodeURIComponent(id)}`),
  clearAllConversations: () => request<void>('/conversations', { method: 'DELETE' }),

  // Link Preview
  fetchLinkPreview: (url: string) => get<LinkPreview>('/link-preview', { url }),

  // Forge
  forgeResearching: () => get<ForgeResearchingItem[]>('/forge/researching'),
  forgeProposals: () => get<ForgeProposalRecord[]>('/forge/proposals'),
  forgeInstalled: () => get<SkillRecord[]>('/forge/installed'),
  forgeInstall: (id: string) => post<ForgeProposalRecord>(`/forge/proposals/${id}/install`, {}),
  forgeInstallEnable: (id: string) => post<ForgeProposalRecord>(`/forge/proposals/${id}/install-enable`, {}),
  forgeReject: (id: string) => post<ForgeProposalRecord>(`/forge/proposals/${id}/reject`, {}),
  forgeUninstall: (skillID: string) => post<{ skillID: string; uninstalled: boolean }>(`/forge/installed/${encodeURIComponent(skillID)}/uninstall`, {}),

  // Remote access
  remoteAccessStatus: () => get<{ remoteAccessEnabled: boolean; port: number; lanIP: string | null; accessURL: string | null; tailscaleEnabled: boolean; tailscaleIP: string | null; tailscaleURL: string | null; tailscaleConnected: boolean }>('/auth/remote-status'),
  remoteAccessKey: () => get<{ key: string }>('/auth/remote-key'),
  revokeRemoteSessions: () => del<{ revoked: boolean }>('/auth/remote-sessions', {}),

  // Engine LM
  engineStatus: () => get<EngineStatus>('/engine/status'),
  engineModels: () => get<EngineModelInfo[]>('/engine/models'),
  engineStart: (model: string, port?: number, ctxSize?: number) => post<EngineStatus>('/engine/start', { model, port, ctxSize }),
  engineStop: () => post<EngineStatus>('/engine/stop', {}),
  engineDeleteModel: (name: string) => request<EngineModelInfo[]>(`/engine/models/${encodeURIComponent(name)}`, { method: 'DELETE' }),
  engineDownloadModel: (url: string, filename: string): EventSource =>
    // POST body can't be sent via EventSource — use a query-string shim via a GET
    // that the server won't support. Instead we open an SSE POST via fetch in the
    // component using a raw fetch+ReadableStream. This entry point is a factory
    // helper for the base URL so components don't need to import BASE directly.
    new EventSource(`${BASE()}/engine/models/download`),
  engineDownloadBaseURL: () => BASE(),
  engineUpdateBaseURL: () => BASE(),

  // Tool Router (Phase 3)
  engineRouterStatus: () => get<EngineStatus>('/engine/router/status'),
  engineRouterStart: (model?: string) => post<EngineStatus>('/engine/router/start', { model }),
  engineRouterStop: () => post<EngineStatus>('/engine/router/stop', {}),
}
