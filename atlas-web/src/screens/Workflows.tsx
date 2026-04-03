import { useEffect, useState } from 'preact/hooks'
import { api, WorkflowDefinition, WorkflowRun } from '../api/client'
import { PageHeader } from '../components/PageHeader'

const RefreshIcon = () => (
  <svg width="11" height="11" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">
    <path d="M2.5 8a5.5 5.5 0 0 1 9.5-3.8" />
    <polyline points="13.5,2.5 13.5,6 10,6" />
    <path d="M13.5 8a5.5 5.5 0 0 1-9.5 3.8" />
    <polyline points="2.5,13.5 2.5,10 6,10" />
  </svg>
)

function formatDate(value?: string) {
  if (!value) return '—'
  try { return new Date(value).toLocaleString() } catch { return value }
}

function statusBadge(status: string) {
  switch (status) {
    case 'completed': return <span class="badge badge-green">{status}</span>
    case 'failed':
    case 'denied': return <span class="badge badge-red">{status}</span>
    case 'waiting_for_approval': return <span class="badge badge-yellow">needs approval</span>
    default: return <span class="badge badge-gray">{status}</span>
  }
}

interface WorkflowModalProps {
  workflow?: WorkflowDefinition
  onSave: (workflow: WorkflowDefinition) => Promise<void>
  onClose: () => void
}

function WorkflowModal({ workflow, onSave, onClose }: WorkflowModalProps) {
  const [name, setName] = useState(workflow?.name ?? '')
  const [description, setDescription] = useState(workflow?.description ?? '')
  const [promptTemplate, setPromptTemplate] = useState(workflow?.promptTemplate ?? '')
  const [tags, setTags] = useState((workflow?.tags ?? []).join(', '))
  const [approvalMode, setApprovalMode] = useState(workflow?.approvalMode ?? 'workflow_boundary')
  const [approvedRootPaths, setApprovedRootPaths] = useState((workflow?.trustScope.approvedRootPaths ?? []).join('\n'))
  const [allowedApps, setAllowedApps] = useState((workflow?.trustScope.allowedApps ?? []).join(', '))
  const [allowsSensitiveRead, setAllowsSensitiveRead] = useState(workflow?.trustScope.allowsSensitiveRead ?? false)
  const [allowsLiveWrite, setAllowsLiveWrite] = useState(workflow?.trustScope.allowsLiveWrite ?? false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  async function handleSave() {
    if (!name.trim() || !promptTemplate.trim()) {
      setError('Name and prompt template are required.')
      return
    }

    setSaving(true)
    setError(null)
    try {
      const now = new Date().toISOString()
      const id = workflow?.id ?? name.toLowerCase().replace(/\s+/g, '-').replace(/[^a-z0-9-]/g, '')
      await onSave({
        id,
        name: name.trim(),
        description: description.trim(),
        promptTemplate: promptTemplate.trim(),
        tags: tags.split(',').map(value => value.trim()).filter(Boolean),
        steps: workflow?.steps ?? [],
        trustScope: {
          approvedRootPaths: approvedRootPaths.split('\n').map(value => value.trim()).filter(Boolean),
          allowedApps: allowedApps.split(',').map(value => value.trim()).filter(Boolean),
          allowsSensitiveRead,
          allowsLiveWrite,
        },
        approvalMode,
        createdAt: workflow?.createdAt ?? now,
        updatedAt: now,
        sourceConversationID: workflow?.sourceConversationID,
        isEnabled: workflow?.isEnabled ?? true,
      })
      onClose()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to save workflow.')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div class="modal-overlay" onClick={(e) => { if ((e.target as HTMLElement).classList.contains('modal-overlay')) onClose() }}>
      <div class="modal automation-modal" style={{ maxWidth: 820, width: '94vw' }}>
        <div class="modal-header">
          <div class="automation-modal-title-wrap">
            <div class="surface-eyebrow">Workflow</div>
            <h3 class="automation-modal-title">{workflow ? workflow.name : 'Create workflow'}</h3>
          </div>
          <button class="btn btn-ghost btn-sm" onClick={onClose}>✕</button>
        </div>
        <div class="modal-body automation-modal-body" style={{ maxHeight: 'calc(85vh - 130px)', overflowY: 'auto' }}>
          {error && <p class="error-banner">{error}</p>}

          <div class="workflow-form-grid">
            <div class="automation-field-group">
              <label class="field-label">Name</label>
              <input class="field-input" value={name} onInput={(e) => setName((e.target as HTMLInputElement).value)} placeholder="Project handoff" />
            </div>
            <div class="automation-field-group">
              <label class="field-label">Description</label>
              <input class="field-input" value={description} onInput={(e) => setDescription((e.target as HTMLInputElement).value)} placeholder="What this workflow does" />
            </div>
          </div>

          <div class="automation-field-group">
            <label class="field-label">Prompt Template</label>
            <span class="workflow-field-hint">Use {'{{variable}}'} for dynamic values</span>
            <textarea
              class="field-input workflow-textarea"
              value={promptTemplate}
              onInput={(e) => setPromptTemplate((e.target as HTMLTextAreaElement).value)}
              placeholder="Describe the work Atlas should do…"
            />
          </div>

          {/* Tags + Approval Mode — flat row-sharing grid */}
          <div class="workflow-aligned-grid">
            <label class="field-label">Tags</label>
            <label class="field-label">Approval Mode</label>
            <span class="workflow-field-hint">Comma-separated</span>
            <span class="workflow-field-hint">When Atlas pauses for confirmation</span>
            <input class="field-input" value={tags} onInput={(e) => setTags((e.target as HTMLInputElement).value)} placeholder="files, apps, handoff" />
            <select class="field-input" value={approvalMode} onChange={(e) => setApprovalMode((e.target as HTMLSelectElement).value)}>
              <option value="workflow_boundary">Workflow boundary</option>
              <option value="step_by_step">Step by step</option>
            </select>
          </div>

          {/* Trust Scope — flat 4-row grid */}
          <div class="workflow-section">
            <div class="workflow-section-label">Trust Scope</div>
            <div class="workflow-trust-grid">
              <label class="field-label">Approved Root Paths</label>
              <label class="field-label">Allowed Apps</label>
              <span class="workflow-field-hint">One path per line</span>
              <span class="workflow-field-hint">Comma-separated app names</span>
              <textarea
                class="field-input workflow-textarea"
                value={approvedRootPaths}
                onInput={(e) => setApprovedRootPaths((e.target as HTMLTextAreaElement).value)}
                placeholder="/Users/you/Projects&#10;/Users/you/Documents"
              />
              <div style={{ display: 'flex', flexDirection: 'column', gap: '8px', alignSelf: 'start' }}>
                <input
                  class="field-input"
                  value={allowedApps}
                  onInput={(e) => setAllowedApps((e.target as HTMLInputElement).value)}
                  placeholder="Finder, Safari, Calendar"
                />
                <div class="workflow-checkboxes">
                  <label class="workflow-checkbox-label">
                    <input type="checkbox" checked={allowsSensitiveRead} onChange={(e) => setAllowsSensitiveRead((e.target as HTMLInputElement).checked)} />
                    Allow sensitive reads
                  </label>
                  <label class="workflow-checkbox-label">
                    <input type="checkbox" checked={allowsLiveWrite} onChange={(e) => setAllowsLiveWrite((e.target as HTMLInputElement).checked)} />
                    Allow live writes
                  </label>
                </div>
              </div>
            </div>
          </div>
        </div>
        <div class="modal-footer">
          <button class="btn btn-ghost btn-sm" onClick={onClose} disabled={saving}>Cancel</button>
          <button class="btn btn-primary btn-sm" onClick={handleSave} disabled={saving}>
            {saving ? 'Saving…' : workflow ? 'Save Changes' : 'Create Workflow'}
          </button>
        </div>
      </div>
    </div>
  )
}

function WorkflowRunsPanel({ workflow, onClose }: { workflow: WorkflowDefinition, onClose: () => void }) {
  const [runs, setRuns] = useState<WorkflowRun[]>([])

  useEffect(() => {
    api.workflowRuns(workflow.id).then(setRuns).catch(() => setRuns([]))
  }, [workflow.id])

  async function handleApprove(runID: string) {
    const updated = await api.approveWorkflowRun(runID)
    setRuns(current => current.map(run => run.id === updated.id ? updated : run))
  }

  async function handleDeny(runID: string) {
    const updated = await api.denyWorkflowRun(runID)
    setRuns(current => current.map(run => run.id === updated.id ? updated : run))
  }

  return (
    <div class="modal-overlay" onClick={(e) => { if ((e.target as HTMLElement).classList.contains('modal-overlay')) onClose() }}>
      <div class="modal automation-modal" style={{ maxWidth: 760, width: '92vw' }}>
        <div class="modal-header">
          <div class="automation-modal-title-wrap">
            <div class="surface-eyebrow">Workflow Runs</div>
            <h3 class="automation-modal-title">{workflow.name}</h3>
          </div>
          <button class="btn btn-ghost btn-sm" onClick={onClose}>✕</button>
        </div>
        <div class="modal-body" style={{ maxHeight: 500, overflowY: 'auto' }}>
          {runs.length === 0 && <p class="empty-state">No workflow runs yet.</p>}
          {runs.map(run => (
            <div key={run.id} class="surface-card-soft" style={{ padding: '14px', marginBottom: '12px' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: '10px', alignItems: 'center' }}>
                <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                  {statusBadge(run.status)}
                  <span class="surface-meta">{formatDate(run.startedAt)}</span>
                </div>
                {run.status === 'waiting_for_approval' && (
                  <div style={{ display: 'flex', gap: '8px' }}>
                    <button class="btn btn-primary btn-xs" onClick={() => handleApprove(run.id)}>Approve</button>
                    <button class="btn btn-ghost btn-xs" onClick={() => handleDeny(run.id)}>Deny</button>
                  </div>
                )}
              </div>
              {run.approval?.reason && <p class="automation-prompt" style={{ marginTop: '8px' }}>{run.approval.reason}</p>}
              {run.assistantSummary && <pre class="run-output">{run.assistantSummary}</pre>}
              {run.errorMessage && <p class="error-banner" style={{ marginTop: '10px' }}>{run.errorMessage}</p>}
              {run.stepRuns.length > 0 && (
                <div style={{ marginTop: '10px', display: 'flex', flexDirection: 'column', gap: '8px' }}>
                  {run.stepRuns.map(step => (
                    <div key={step.id} class="automation-run-card">
                      <div class="run-row-header">
                        <strong>{step.title}</strong>
                        {statusBadge(step.status)}
                      </div>
                      {(step.output || step.errorMessage) && <pre class="run-output">{step.output ?? step.errorMessage}</pre>}
                    </div>
                  ))}
                </div>
              )}
            </div>
          ))}
        </div>
      </div>
    </div>
  )
}

export function Workflows() {
  const [workflows, setWorkflows] = useState<WorkflowDefinition[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [editing, setEditing] = useState<WorkflowDefinition | 'new' | null>(null)
  const [runsTarget, setRunsTarget] = useState<WorkflowDefinition | null>(null)
  const [runningID, setRunningID] = useState<string | null>(null)

  async function load() {
    setLoading(true)
    setError(null)
    try {
      setWorkflows(await api.workflows())
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load workflows.')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load() }, [])

  async function handleSave(workflow: WorkflowDefinition) {
    if (editing === 'new') {
      const created = await api.createWorkflow(workflow)
      setWorkflows(current => [created, ...current])
    } else {
      const updated = await api.updateWorkflow(workflow)
      setWorkflows(current => current.map(item => item.id === updated.id ? updated : item))
    }
  }

  async function handleDelete(workflow: WorkflowDefinition) {
    if (!confirm(`Delete workflow "${workflow.name}"?`)) return
    await api.deleteWorkflow(workflow.id)
    setWorkflows(current => current.filter(item => item.id !== workflow.id))
  }

  async function handleRun(workflow: WorkflowDefinition) {
    setRunningID(workflow.id)
    try {
      await api.runWorkflow(workflow.id)
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to run workflow.')
    } finally {
      setRunningID(null)
    }
  }

  return (
    <div class="screen">
      <PageHeader
        title="Workflows"
        subtitle="Reusable, trust-bounded operator flows Atlas can run directly or schedule."
        actions={
          <>
            <button class="btn btn-primary btn-sm" onClick={() => setEditing('new')}>+ New</button>
            <button class="btn btn-primary btn-sm" onClick={load}><RefreshIcon /> Refresh</button>
          </>
        }
      />

      {error && <p class="error-banner">{error}</p>}
      {loading && <p class="empty-state">Loading workflows…</p>}
      {!loading && workflows.length === 0 && (
        <div class="empty-state">
          <svg class="empty-icon" viewBox="0 0 36 36" fill="none" stroke="currentColor" stroke-width="1.2" stroke-linecap="round" stroke-linejoin="round">
            <circle cx="18" cy="18" r="13" />
            <path d="M13 18h10M18 13l5 5-5 5" />
          </svg>
          <h3>No workflows saved yet</h3>
          <p>Click <strong>+ New</strong> to build a reusable operator flow, then attach it to an automation.</p>
        </div>
      )}

      {!loading && workflows.length > 0 && (
        <div class="automation-list">
          {workflows.map(workflow => (
            <div key={workflow.id} class="automation-card">
              <div class="automation-card-header">
                <div class="automation-meta">
                  <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                    <span class="automation-name">{workflow.name}</span>
                    {workflow.steps.length > 0 && <span class="badge badge-gray">{workflow.steps.length} steps</span>}
                  </div>
                  <span class="automation-schedule">{workflow.description || workflow.promptTemplate.slice(0, 90)}</span>
                </div>
                <div class="automation-actions">
                  <button class="btn btn-ghost btn-xs" onClick={() => handleRun(workflow)} disabled={runningID === workflow.id}>
                    {runningID === workflow.id ? '…' : 'Run'}
                  </button>
                  <button class="btn btn-ghost btn-xs" onClick={() => setRunsTarget(workflow)}>Runs</button>
                  <button class="btn btn-ghost btn-xs" onClick={() => setEditing(workflow)}>Edit</button>
                  <button class="btn btn-ghost btn-xs btn-danger" onClick={() => handleDelete(workflow)}>Delete</button>
                </div>
              </div>

              <p class="automation-prompt">{workflow.promptTemplate}</p>

              <div style={{ display: 'flex', flexWrap: 'wrap', gap: '8px', marginTop: '10px' }}>
                {workflow.tags.map(tag => <span key={tag} class="badge badge-gray">{tag}</span>)}
                {workflow.trustScope.allowedApps.map(app => <span key={app} class="badge badge-gray">app:{app}</span>)}
                {workflow.trustScope.approvedRootPaths.slice(0, 2).map(path => <span key={path} class="badge badge-gray">path:{path}</span>)}
              </div>
            </div>
          ))}
        </div>
      )}

      {editing !== null && (
        <WorkflowModal
          workflow={editing === 'new' ? undefined : editing}
          onSave={handleSave}
          onClose={() => setEditing(null)}
        />
      )}

      {runsTarget && <WorkflowRunsPanel workflow={runsTarget} onClose={() => setRunsTarget(null)} />}
    </div>
  )
}
