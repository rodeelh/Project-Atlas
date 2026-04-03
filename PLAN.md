# PLAN

## V1.0

### Context
- Atlas Teams is the headline new capability for the next major product phase.
- Desktop Operator and Proactive Atlas already exist in partial form and should mature alongside Teams, but Teams is the primary new feature.
- Atlas remains the primary agent at all times. Team members extend Atlas but do not replace it.
- Before starting this work, the app will first be migrated to Go + TypeScript rather than SwiftUI.

### Product Pillars
- New: Atlas Teams
- Expanded: Desktop Operator
- Expanded: Proactive Atlas

### Atlas Teams Foundation

#### Core rule
- Atlas is always the primary agent.
- Team members extend Atlas’s capability, but they do not replace Atlas.
- Atlas remains:
  - the main user-facing intelligence
  - the orchestrator
  - the memory owner
  - the final presenter of results
  - the fallback worker when no suitable specialist exists

#### Atlas operating modes
- Solo Atlas
  - Atlas handles the task directly.
- Atlas + Specialist
  - Atlas owns the task but delegates one focused part to a specialist.
- Atlas as Team Lead
  - Atlas breaks a task into multiple parts and coordinates several specialists.

#### Product principles
- Atlas never refuses ordinary work because the user has no team.
- Delegation is additive, not required.
- A small team does not weaken Atlas.
- Atlas should explain delegation simply when useful.
- Atlas is never just a router.

### Team Member Templates

#### Scout
- Role: Research Specialist
- Job: gather facts, references, docs, comparisons, and external context
- Best for: research-heavy prompts, competitive scans, documentation lookups
- Default skills: search, documentation, web/research tools
- Default autonomy: assistive

#### Builder
- Role: Drafting and Implementation Specialist
- Job: produce first-pass artifacts
- Best for: code drafts, written drafts, structured plans, implementation passes
- Default skills: build/write/implementation-oriented skills
- Default autonomy: on-demand

#### Reviewer
- Role: Quality Specialist
- Job: inspect outputs for risk, clarity, correctness, and regressions
- Best for: code review, plan review, edge-case checking, polish checks
- Default skills: review, validation, testing, critique-oriented skills
- Default autonomy: assistive or post-task trigger

#### Operator
- Role: Execution Specialist
- Job: carry out bounded actions across desktop, workflows, and tools
- Best for: deterministic execution, operational steps, environment interaction
- Default skills: desktop/operator/workflow/action skills
- Default autonomy: on-demand

#### Monitor
- Role: Watcher
- Job: observe important state changes and surface issues or opportunities
- Best for: approvals, daemon issues, failed automations, stale workflows, connection problems
- Default skills: monitoring/status/logs/workflow/notification skills
- Default autonomy: bounded autonomous

### Agent Data Model
- Each Atlas Team member should have:
  - `id`
  - `name`
  - `templateRole`
  - `mission`
  - `personaStyle`
  - `allowedSkills`
  - `allowedToolClasses`
  - `autonomyMode`
  - `activationRules`
  - `status`
  - `currentTaskID`
  - `recentOutputs`
  - `taskHistory`
  - `usageStats`
  - `createdAt`
  - `updatedAt`
  - `isEnabled`

#### Suggested enums
- Autonomy mode
  - `on_demand`
  - `assistive`
  - `bounded_autonomous`
- Status
  - `idle`
  - `working`
  - `waiting`
  - `blocked`
  - `needs_review`
  - `done`

### Team HQ Interface

#### Core layout
- Atlas station
- team workshop floor
- activity rail
- task/output inspector

#### Atlas station
- Atlas should have a dedicated central station, visually distinct from team members.
- Show:
  - Atlas name
  - current mode
  - current task summary
  - active delegation summary
  - quick actions

#### Team member stations
- Each team member should appear as a station, not just a boring card.
- Show:
  - agent name
  - role
  - status
  - current task or idle message
  - recent output snippet
  - last active
  - quick actions

#### Activity rail
- Show:
  - Atlas assigned Scout a research task
  - Builder completed a draft
  - Reviewer flagged a risk
  - Monitor detected a daemon issue
  - Operator is waiting on approval

#### Empty state
- When no team members exist, Team HQ should still feel intentional.
- Reinforce:
  - Atlas already works on its own
  - specialists can help Atlas with focused jobs

### Task Lifecycle
- Task states:
  - created
  - planned
  - assigned
  - in_progress
  - blocked
  - awaiting_review
  - completed
  - failed
  - canceled
  - recovered
- Step states:
  - queued
  - active
  - waiting
  - blocked
  - done
  - failed
- Agent states:
  - idle
  - working
  - waiting
  - blocked
  - done

#### Delegation patterns
- Atlas direct
- Single specialist assist
- Sequential squad
- Parallel squad

### Creation And Editing

#### Creation entry points
- Team HQ
- Atlas chat
- Suggested actions

#### Creation flow
1. Choose template
2. Name the agent
3. Define mission
4. Choose allowed skills
5. Choose autonomy level
6. Review and create

#### Editing flow
- Editable fields:
  - name
  - mission
  - allowed skills
  - autonomy mode
  - enabled/disabled

#### Disable vs retire
- Disable
  - temporarily unavailable
- Retire / Remove
  - no longer part of the active team
  - history preserved

### Chat Command Grammar

#### Create
- “Create a new research agent”
- “Make a reviewer for release quality”
- “Add a monitor for approvals”

#### Edit
- “Rename Scout to Pathfinder”
- “Give Builder access to documentation skills too”
- “Turn Reviewer into on-demand only”

#### Assign
- “Ask Scout to research this”
- “Have Builder draft a first pass”
- “Send this to the team”

#### Inspect
- “What is Scout doing?”
- “Show me blocked agents”
- “What did Builder finish today?”

#### Recommend
- “Do I need another team member?”
- “What agent am I missing?”

### Bounded Autonomy + Trigger Model

#### Core rule
- Agents never free-roam.
- Autonomy in V1.0 is always:
  - bounded
  - trigger-based
  - Atlas-mediated
  - auditable

#### Autonomy levels
- On Demand
  - never self-activates
- Assistive
  - Atlas may invoke when relevant inside a live task
- Bounded Autonomous
  - Atlas may consider activation when specific triggers occur

#### Trigger types
- System Health
- Workflow / Automation
- Approvals
- Communications
- Scheduled Review
- Atlas In-Task Assist

#### Trigger evaluation flow
1. Event occurs
2. Atlas receives trigger
3. Atlas filters candidates
4. Atlas decides
5. Agent runs
6. Atlas interprets result

#### Guardrails
- Atlas must evaluate first
- No risky silent action
- Cooldowns
- Trigger deduplication
- Atlas fallback

#### Recommended V1.0 autonomous scenarios
- Daemon issue detected
- Automation fails
- Approval backlog grows
- Build-review chain
- Morning workspace check

### Runtime Architecture

#### Five layers
1. `AGENTS.md` definition layer
2. team sync layer
3. orchestration layer
4. persistence/runtime state layer
5. snapshot/API layer

#### Core runtime components
- `TeamDefinitionStore`
- `TeamDefinitionSyncEngine`
- `TeamOrchestrator`
- `TeamRuntimeStore`
- `TeamSnapshotAssembler`

#### Storage model
- File-based:
  - `AGENTS.md`
- Structured persistence:
  - SQLite/runtime storage, likely alongside `atlas.sqlite3`

#### Suggested persistent entities
- `AgentDefinitionRecord`
- `AgentRuntimeRecord`
- `TeamTaskRecord`
- `TeamTaskStepRecord`
- `TeamEventRecord`
- `AgentMetricsRecord`

#### `AGENTS.md` sync pipeline
- On startup:
  1. load `AGENTS.md`
  2. parse definitions
  3. validate
  4. sync to `AgentDefinitionRecord`
  5. ensure `AgentRuntimeRecord` exists
- On file change:
  1. detect change
  2. reparse
  3. diff against stored definitions
  4. apply updates
  5. emit team events
- On UI/chat edits:
  1. write updated `AGENTS.md`
  2. trigger sync
  3. return updated snapshot

### `AGENTS.md` Canonical Role
- `AGENTS.md` is the canonical team-definition file.
- Structured runtime persistence holds all operational state.
- UI reads runtime snapshots, not the file directly.
- Atlas remains fully functional without `AGENTS.md`.

#### Recommended path
- `/Users/ralhassan/Library/Application Support/ProjectAtlas/AGENTS.md`

### `AGENTS.md` Format
- Structured markdown with one section per agent.

#### File structure
- `# Atlas Team`
- `## Atlas`
- `## Team Members`
- `### Agent Name`
  - `- ID: ...`
  - `- Role: ...`
  - `- Mission: ...`
  - `- Style: ...`
  - `- Allowed Skills: ...`
  - `- Allowed Tool Classes: ...`
  - `- Autonomy: ...`
  - `- Activation: ...`
  - `- Enabled: yes|no`

#### Required fields
- `ID`
- `Role`
- `Mission`
- `Allowed Skills`
- `Autonomy`
- `Enabled`

### Team HQ API Snapshot Schema

#### `GET /team`
- Returns:
  - Atlas node
  - agents
  - activity
  - blocked items
  - suggested actions

#### Additional endpoints
- `GET /team/agents`
- `GET /team/agents/:id`
- `POST /team/agents`
- `PUT /team/agents/:id`
- `POST /team/agents/:id/pause`
- `POST /team/agents/:id/resume`
- `POST /team/agents/:id/disable`
- `POST /team/agents/:id/enable`
- `GET /team/tasks`
- `GET /team/tasks/:id`
- `POST /team/tasks`
- `POST /team/tasks/:id/cancel`
- `GET /team/events`
- `POST /team/sync`

### Implementation Roadmap

#### Milestone 1: Foundations
- `AGENTS.md` file support
- parser and writer
- sync engine
- structured persistence for definitions and runtime state

#### Milestone 2: Team HQ Skeleton
- Team HQ route/screen
- Atlas station
- empty state
- team member stations
- agent detail shell
- `/team` and `/team/agents` APIs

#### Milestone 3: Creation and Editing
- create team member flow
- template picker
- edit agent flow
- enable/disable/pause/resume
- chat-based creation/edit commands

#### Milestone 4: Single-Agent Delegation
- `TeamTask` and `TeamTaskStep` persistence
- single-agent assignment
- Atlas-mediated delegation
- output and activity history

#### Milestone 5: Sequential Team Workflows
- sequential orchestration mode
- bounded multi-step tasks
- Scout → Builder → Reviewer handoff
- activity rail improvements

#### Milestone 6: Bounded Autonomy
- trigger coordinator
- bounded autonomy rules
- trigger event storage
- cooldowns/deduping
- first autonomous scenarios for Monitor and Reviewer

#### Milestone 7: Metrics and Polish
- productivity/activity metrics
- richer detail views
- suggested actions
- workshop visual polish
- token/cost efficiency tuning

### MVP Recommendation
- Milestone 1
- Milestone 2
- Milestone 3
- Milestone 4
- part of Milestone 5

This yields:
- persistent team members
- Team HQ
- creation/editing
- single-agent delegation
- one simple sequential team flow

### Risks
- Overbuilding the runtime before the first useful experience
- Overcomplicating autonomy
- Letting agents become full independent copilots too early
- UI charm outrunning clarity

### Success Criteria
- users can create a specialist quickly
- understand what each agent does
- assign work confidently
- see what agents are doing
- trust outputs and boundaries
- feel that Atlas is coordinating a real team, not just faking parallelism
