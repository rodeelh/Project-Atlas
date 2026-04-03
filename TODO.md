# TODO

## Recurring Maintenance

### Monthly — Engine LM Currency Check
Keep the Engine LM tab up to date with the latest models and tooling.

- **Starter model list** (`atlas-web/src/screens/AtlasEngine.tsx` — `PRIMARY_MODELS` / `ROUTER_MODELS`): check for newer releases from Gemma, Qwen, Mistral, Phi, and other families. Verify bartowski HuggingFace repos exist and Q4_K_M filenames are correct by pinging the HuggingFace API.
- **llama-server repo / release format** (`atlas-runtime/internal/engine/manager.go` — `DownloadBinary` and `Atlas/Makefile` — `download-engine` target): confirm the GitHub org (`ggml-org/llama.cpp`), release asset naming convention, and archive format (`.tar.gz`) haven't changed. Update `PINNED_VERSION` in `AtlasEngine.tsx` and the default in `postUpdate` if a newer stable build is available. Note: the Makefile `download-engine` target still references `ggerganov/llama.cpp` — update org + filename pattern if the release format changes again.
- **Optimization flags** (`manager.go` — `Start()`): review llama.cpp changelog for new Apple Silicon / Metal flags worth enabling by default. Current: flash-attn on, KV q4_0, no-mmap, defrag-thold 0.1, P-core threads.

---

## Architecture Improvements

Two high-priority improvements planned for the next sprint. Both are strictly additive — no existing skill, route, or config field is modified or removed.

---

### ARC-1 — Parallel Tool Execution

**Goal:** Cut multi-tool turn latency by 40–70% by running independent skill calls concurrently instead of serially.

**Key constraint:** Browser skills (`browser.*`) share a stateful go-rod Chrome process and must remain sequential. All other skills are stateless per-call and safe to parallelise.

**Implementation phases:**

**Phase 1.1 — `IsStateful()` method on Registry** (`internal/skills/registry.go`)
- Returns `true` for any `browser.*` or `browser__*` skill
- Centralises the check — future stateful skill groups (e.g. terminal sessions) add one line here

**Phase 1.2 — Parallel execution block in `loop.go`** (`internal/agent/loop.go`)
- Before the tool execution loop, check if any call in `canRun` is stateful
- If yes → existing serial path (unchanged)
- If no → fan out goroutines using index-preserving `results := make([]toolResult, len(canRun))`; each goroutine writes to `results[i]`; `wg.Wait()` then iterate in original order
- `tool_call` SSE events fire immediately as goroutines start (accurate — user sees all tools starting)
- `tool_finished`/`tool_failed` SSE events fire inside each goroutine as they complete (also accurate)
- `OAIMessage` appends happen in the outer loop after `wg.Wait()` — strictly ordered (required by OpenAI spec)

**Phase 1.3 — Tests**
- Two mock skills sleeping 100ms each: parallel path should complete in ~100ms, not 200ms
- Results appended in original call order regardless of goroutine completion order
- Batch with one `browser.*` call falls through to serial path
- One skill errors, others succeed — both paths append correct tool messages

**Rollout:** Single PR. Optional `LoopConfig.ParallelTools bool` feature flag for safe rollout if needed; remove after one week of stable operation.

---

### ARC-2 — Custom Skills

**Goal:** Allow users to install new skills as executables (any language) without recompiling Atlas. Skills appear in `GET /skills` with `"source": "custom"`. From the model's perspective there is no difference from built-ins.

**See:** CLAUDE.md "Custom Skills" section for protocol spec, manifest format, and directory layout.

**Implementation phases:**

**Phase 2.1 — Manifest + protocol** (design only — already defined in CLAUDE.md)
- `skill.json` manifest format finalised
- Subprocess JSON protocol (one line in, one line out) finalised

**Phase 2.2 — `PluginLoader` in `internal/skills/`** — new file `internal/skills/custom.go`
- `LoadCustomSkills(skillsDir string) error` scans for `skill.json` manifests
- Registers each action as a `SkillEntry` with a `FnResult` closure that spawns the subprocess
- Invalid manifests: log warn + skip, never crash
- Subprocess executor: `exec.CommandContext` with 30s deadline, write JSON to stdin, read JSON from stdout, size-limit output to 1 MB

**Phase 2.3 — Wire into `main.go`**
- `skillsDir := filepath.Join(config.SupportDir(), "skills")`
- `os.MkdirAll(skillsDir, 0755)` on startup
- `skillsRegistry.LoadCustomSkills(skillsDir)` after `NewRegistry`, before HTTP server starts

**Phase 2.4 — HTTP routes** (`internal/domain/features.go` + `internal/server/router.go`)
- `GET  /skills/custom` — list installed custom skills (id, name, version, actions)
- `POST /skills/install` — install from local path or URL (download zip → verify `skill.json` exists → move to `skillsDir/<id>`)
- `DELETE /skills/:id` — remove custom skill directory

**Phase 2.5 — `features/skills.go` listing**
- Extend `ListSkills()` to include custom skills with `"source": "custom"`
- Custom skills appear in `GET /skills` alongside built-in (`"source": "builtin"`) and forge (`"source": "forge"`)

**Phase 2.6 — Web UI** (Skills screen)
- Custom skills appear in the existing Skills list with a `Custom` badge (same as `Forge` badge pattern)
- Install button: URL or local path input
- Remove button per custom skill
- No separate screen needed

**Phase 2.7 — Tests**
- Shell script skill that echoes JSON: verify registration, execution, correct output
- Non-zero exit: graceful error wrapping
- Timeout: process killed, error returned
- Missing required manifest fields: skipped with warn log, no crash
- End-to-end: `POST /skills/install` → appears in `GET /skills` → agent loop calls it

**Rollout sequencing:**
```
Week 1  ARC-1 Parallel tool execution (Phase 1.1–1.3) — ship, soak 1 week
Week 2  ARC-2 Phase 2.1–2.3 — backend + subprocess executor only; test with shell script
Week 3  ARC-2 Phase 2.4–2.5 — HTTP routes + skill listing
Week 4  ARC-2 Phase 2.6–2.7 — Web UI + hardening (output size limit, action_class enforcement)
```

---

## v1.5 — MIND Tier 3: Research-Grade Improvements

These are high-potential improvements to Atlas's memory and personality systems that require experimentation before committing. Each is framed as a hypothesis to test, not a directive. All must work within the constraints of prompt engineering, external storage, and retrieval (no fine-tuning).

**Prerequisites:** Tier 1 (relevance-based retrieval, structured prompts, diff-based reflection, smart diary) and Tier 2 (LLM extraction, dream cycle, budget management) are complete.

---

### T3.1 — Agent-Managed Memory (MemGPT Pattern)

**Hypothesis:** Giving Atlas explicit tools to search, read, update, and delete its own memories would produce better memory quality than the current passive extraction pipeline.

**What to build:**
- Register three new skills:
  - `memory.search(query)` — searches memories by keyword/semantic match, returns top 5
  - `memory.save(category, title, content)` — creates or updates a memory
  - `memory.forget(id)` — deletes a memory by ID
- Add a system prompt instruction to MIND.md's `## Working Style`:
  > When you learn something new about the user or notice a pattern worth remembering, save it to memory. When information seems outdated, forget it. When you need context you don't have, search memory first.
- Keep the passive LLM extraction pipeline as a safety net (catches things Atlas forgets to save)

**Where:**
- New file: `internal/skills/memory_tools.go` — register the three skills
- `internal/skills/registry.go` — call `r.registerMemoryTools()`
- `internal/mind/seed.go` — add the instruction to the default Working Style section

**How to evaluate:**
- Run for 2 weeks with both agent-managed tools and passive extraction active
- Compare: Are agent-saved memories more relevant? More accurate? More timely?
- Track via `source` field: `"agent_tool"` vs `"llm_extraction"` vs `"user_explicit"`
- Check: Does the agent over-save (token cost) or forget things the user wanted kept?

**Risk:** The agent may save too aggressively (wasting tokens) or too timidly (missing important context). The system prompt instruction needs careful tuning. Start with `ActionClassLocalWrite` so saves are auto-approved.

**Prior art:** MemGPT/Letta (Packer et al., 2023). Their core finding: agents that self-manage memory via tool calls outperform systems with passive memory injection.

---

### T3.2 — Relational Memory Graph

**Hypothesis:** Storing memory as a graph (entities + relationships + temporal edges) rather than flat key-value pairs would enable Atlas to answer relational questions and detect patterns that flat memory cannot.

**What to build:**
- Two new SQLite tables:
  ```sql
  CREATE TABLE memory_entities (
    entity_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    entity_type TEXT NOT NULL,  -- "person", "project", "tool", "concept"
    first_seen TEXT NOT NULL,
    last_seen TEXT NOT NULL,
    metadata_json TEXT DEFAULT '{}'
  );

  CREATE TABLE memory_edges (
    edge_id TEXT PRIMARY KEY,
    source_entity TEXT NOT NULL REFERENCES memory_entities(entity_id),
    target_entity TEXT NOT NULL REFERENCES memory_entities(entity_id),
    relation TEXT NOT NULL,      -- "works_on", "prefers", "switched_from", "uses"
    created_at TEXT NOT NULL,
    weight REAL DEFAULT 1.0,
    metadata_json TEXT DEFAULT '{}'
  );
  ```
- Entity extraction: after each LLM memory extraction (T2.1), also extract entities and relationships
- Query support: `memory.search` (from T3.1) can traverse the graph for multi-hop queries
- Dream cycle integration: Phase 2 (merge) can also merge entity nodes

**Where:**
- `internal/storage/db.go` — new tables in `migrate()`, CRUD methods
- `internal/memory/llm.go` — extend extraction prompt to also return entities/edges
- `internal/mind/dream.go` — Phase 2 entity deduplication

**How to evaluate:**
- After 1 month, test qualitative queries:
  - "How has my project focus changed over time?"
  - "What tools do I use most with Atlas?"
  - "When did I switch from Swift to Go?"
- Compare answers with and without graph context

**Risk:** Over-engineering at Atlas's scale. Hundreds of memories don't need a graph — but as the system matures over months/years, relational context becomes increasingly valuable. Start minimal (entities only, no multi-hop traversal) and expand based on real usage.

**Prior art:** Zep/Graphiti (temporally-aware knowledge graph), Mem0 (hybrid graph + vector + key-value). Both demonstrate that relationships between facts matter more than individual facts at scale.

---

### T3.3 — Emotional/Tonal State Tracking

**Hypothesis:** Tracking the user's emotional state across turns (frustrated, excited, focused, casual) would allow Atlas to adapt its communication style in real time, beyond static "prefer concise" preferences.

**What to build:**
- Add a `tone` field to `TurnRecord` in `internal/mind/types.go`
- After each turn, a fast-model call classifies tone from a fixed set:
  - `focused`, `casual`, `frustrated`, `excited`, `uncertain`, `neutral`
- Store in a rolling 10-turn buffer (in-memory, not persisted — tone is ephemeral)
- Inject a one-line tone context into the system prompt when a shift is detected:
  > "Recent tone shift: user has become [frustrated] over the last 3 turns — be more careful and confirming."
- Only inject when tone changes significantly (e.g., 3+ turns of the same non-neutral tone)

**Where:**
- `internal/mind/types.go` — add `Tone string` to `TurnRecord`
- `internal/mind/tone.go` — new file: `ClassifyTone(ctx, provider, turn) string`, rolling buffer, shift detection
- `internal/chat/service.go` — call `ClassifyTone` post-turn, inject into system prompt when shift detected

**How to evaluate:**
- Track classification accuracy: manually review 50 turns and compare Atlas's tone classification to your own judgment
- Measure response quality: do responses feel more attuned when tone tracking is active?
- Check for false positives: does Atlas misread frustration as casualness (or vice versa)?

**Risk:** Tone classification is error-prone. Misreading the user's state could make Atlas feel tone-deaf. Needs:
- A confidence threshold (only act on high-confidence classifications)
- A "neutral" default (don't act if unsure)
- User override ("I'm not frustrated, I'm just being direct")

**Prior art:** Character.AI's affective alignment classifier (post-ranks responses for emotional consistency). A-Mem's context-aware note attributes.

---

### T3.4 — Cross-Conversation Continuity Scoring

**Hypothesis:** When a user returns after a gap (hours, days), the first turn of a new conversation should benefit from a "continuity prompt" that summarizes where things left off.

**What to build:**
- On new conversation start (no `conversationId` in request), check time since last conversation ended
- If < 24h gap:
  - Fetch last conversation's final 3 messages
  - Fetch most recent diary entry
  - Fetch any active project memories
  - Build a 2-3 sentence continuity block:
    > "Last session (2h ago): you were debugging the browser connection error. Diary note: 'Traced closed-connection error to a race in rod cleanup.' Active theory: bugs accumulate at integration seams (testing)."
- Inject as a `<session_continuity>` block at the end of the system prompt (recency position)
- If > 24h gap or first conversation ever: skip (MIND.md already provides long-term context)

**Where:**
- `internal/chat/service.go` — in `buildSystemPrompt`, add continuity block for new conversations
- `internal/storage/db.go` — `LastConversationTimestamp() (string, error)`, `LastNMessages(convID string, n int) ([]MessageRow, error)`

**How to evaluate:**
- Measure: do users need fewer clarifying turns to resume work after a break?
- Compare: first-turn quality with and without continuity prompt
- Track: how often is the continuity block actually relevant (user continues previous work vs. starts something new)?

**Risk:** The continuity block may be irrelevant if the user is starting fresh work. The 24h threshold needs tuning. Consider adding a "context: fresh start" signal that the user can send to skip continuity injection.

**Dependency:** Benefits from T2.2 (dream cycle) which produces meaningful session summaries, and T1.3 (smart diary) which provides descriptive entries to quote.

---

### T3.5 — Embedding-Based Memory Retrieval

**Hypothesis:** Keyword matching (T1.1) misses semantic relationships ("I prefer terse answers" should match when the user says "can you be more brief"). Embedding vectors would capture meaning rather than just lexical overlap.

**What to build:**
- On memory insert/update, call the active provider's embedding endpoint:
  - OpenAI: `text-embedding-3-small` (1536 dims)
  - Gemini: `text-embedding-004`
  - Anthropic: no native embeddings — fall back to keyword matching
- Store embedding as JSON blob in a new `embedding TEXT` column on the `memories` table
- On retrieval, embed the user's current message and compute cosine similarity
- Combined scoring: `embedding_sim * 0.4 + keyword_overlap * 0.2 + importance * 0.2 + recency * 0.2`

**Where:**
- `internal/storage/db.go` — add `embedding` column, update `SaveMemory`/`UpdateMemory`
- `internal/agent/embed.go` — new file: `Embed(ctx, provider, text) ([]float64, error)` dispatcher
- `internal/storage/db.go` — update `RelevantMemories` to use cosine similarity when embeddings are available

**How to evaluate:**
- Compare retrieval quality: for 50 test queries, rank memories with keyword-only vs keyword+embedding
- Measure latency: embedding calls add ~100ms per memory insert and ~100ms per retrieval
- Check provider switching: switching providers means all embeddings need regeneration (expensive)

**Risk and deferral rationale:** This adds latency, cost, and provider lock-in. At Atlas's current scale (dozens to low hundreds of memories), keyword matching from T1.1 may be sufficient. Only pursue this if keyword retrieval proves inadequate after real-world usage. If implemented, add a config flag (`EnableEmbeddingRetrieval`) that defaults to `false`.

**Prior art:** Mem0 (hybrid vector + graph + key-value), Zep (embedding-enhanced retrieval achieving 94.8% on Deep Memory Retrieval benchmark).

---

## v2 — Token Budget Tier 3: Advanced Optimisations

These techniques require significant investment, experimentation, or external infrastructure. None are blocking for v1. Revisit after token budget Tier 1 and Tier 2 are stable in production and real per-turn costs are measurable.

**Prerequisites:** Tier 1 and Tier 2 token optimisations shipped and measured (diary window, history window, selective tool injection, Anthropic prompt caching, selective MIND injection, history context notes).

---

### TK-T3.1 — Learned Tool Compression

**Hypothesis:** After observing which tools are actually called across N turns, compress the schemas of rarely-used tools into shorter "summary" descriptions without losing model comprehension. High-frequency tools retain full schemas.

**What to build:**
- Track per-tool call frequency in a new `tool_usage_stats` SQLite table (tool name, call count, last called)
- After 30 days of data, classify tools as hot (called >10×) or cold (called 0–2×)
- Cold tools get a compressed schema: name + a one-line description only, no parameters
- Hot tools retain full schemas always; medium tools retain full schemas on matching intent
- Add a `GET /status/tool-heat` endpoint to surface the heat map

**Where:**
- `internal/storage/db.go` — `tool_usage_stats` table, increment and read methods
- `internal/agent/loop.go` — increment counter after each tool call (in the existing tool-execution block)
- `internal/skills/registry.go` — `CompressedToolDefs(hot map[string]bool) []map[string]any`

**Expected savings:** 20–40% reduction in tool schema tokens for mature installations where most tools have a defined heat map.

**Risk:** Compressing parameter schemas could cause the model to call a tool with wrong arguments. Cold tools that are suddenly needed (e.g. a rarely-used vault tool) may fail silently. Mitigate by keeping at least the parameter list for any tool that has been called in the last 30 days.

---

### TK-T3.2 — AI-Summarised Conversation History

**Hypothesis:** An LLM-produced summary of older conversation history (messages beyond the verbatim window) is more useful than the compact context note currently injected, and cheaper than including verbatim messages.

**What to build:**
- After each turn where `len(history) > 12`, fire a non-blocking goroutine to generate a 3–5 sentence conversation summary using the fast model
- Store the summary in a new `conversation_summaries` SQLite table (keyed by conversation ID)
- On the next turn for that conversation, if a summary exists and `len(history) > limit`, inject the summary as a `<conversation_summary>` block instead of the compact context note (T2-2)
- Invalidate the summary whenever new messages are added (simple: delete on every `SaveMessage` and regenerate post-turn)

**Where:**
- `internal/storage/db.go` — `conversation_summaries` table, `SaveSummary`, `GetSummary`, `DeleteSummary`
- `internal/chat/service.go` — post-turn goroutine for summary generation, retrieval at turn start
- `internal/mind/` — `SummarizeConversation(ctx, provider, messages) string`

**Expected savings:** ~800–2,500 tokens/turn on conversations > 15 messages, relative to including verbatim messages.

**Risk:** LLM summarisation adds ~500ms latency to the post-turn pipeline (non-blocking, so transparent to the user). Summary quality varies — the model may drop important technical details. Mitigate by always keeping the last 8 messages verbatim regardless of summary availability.

---

### TK-T3.3 — Chunked MIND.md with Vector Retrieval

**Hypothesis:** Replacing the monolithic MIND.md with a chunked vector store and retrieving only the 3–5 most relevant chunks per turn would reduce MIND injection tokens by 60–70% with no quality loss.

**What to build:**
- On MIND.md write, parse into chunks (one chunk per `## ` section), embed each chunk
- Store chunks in `mind_chunks` SQLite table with embedding vector (from T3.5 embedding infrastructure)
- On each turn, embed the user message and retrieve top-3 chunks by cosine similarity
- Inject retrieved chunks as `<atlas_identity>` instead of full MIND.md

**Where:**
- `internal/mind/chunks.go` — new file: chunking, embedding, retrieval
- `internal/storage/db.go` — `mind_chunks` table
- `internal/chat/service.go` — call `mind.RetrieveChunks(ctx, provider, userMessage, supportDir)` in `buildSystemPrompt`

**Expected savings:** MIND.md currently ~1,019 tokens injected in full. With chunked retrieval, inject ~300–400 tokens (3 relevant chunks). Savings: ~600–700 tokens/turn.

**Dependency:** Requires T3.5 embedding infrastructure (embedding endpoint per provider). Anthropic has no native embedding API — would fall back to full MIND injection for Anthropic users.

---

### TK-T3.4 — OpenAI Automatic Prompt Caching Audit

**Hypothesis:** OpenAI automatically caches the longest common prefix of repeated requests. Atlas's prompts qualify if the prefix (system + tool schemas) is stable turn-to-turn. Verifying this is happening (and maximising the cacheable prefix length) could halve OpenAI input costs with zero code change.

**What to build:**
- Add a `cache_read_input_tokens` field to `TokenUsage` (OpenAI includes this in the usage response since 2024)
- Log it alongside `in`/`out` in the Turn Complete log entry
- If cache hit rate is below 80%, investigate what is changing in the prefix between turns (likely: timestamp in system prompt, or randomised ordering in tool definitions)
- Fix the ordering issue: sort `ToolDefinitions()` by tool name for stable serialisation

**Where:**
- `internal/agent/provider.go` — parse `usage.prompt_tokens_details.cached_tokens` from OpenAI response
- `internal/agent/loop.go` — surface in `TokenUsage`, log via `AddSessionTokens`
- `internal/skills/registry.go` — sort tool output in `ToolDefinitions()` and `SelectiveToolDefs()` for stable prefix

**Expected savings:** Up to 50% of OpenAI input token cost if caching is verified and prefix is stable. Zero additional cost to implement.
