#!/bin/bash
# update-claude-md.sh
#
# Claude Code Stop hook — keeps CLAUDE.md in sync after structural changes.
# Fires at the end of every Claude Code session. If signal files changed,
# runs a headless claude to update only the affected sections of CLAUDE.md.
#
# Signal files (changes here = CLAUDE.md may need updating):
#   - internal/skills/registry.go      → new skill registered
#   - internal/features/skills.go      → skills catalog updated
#   - internal/domain/*.go             → new route added to a domain
#   - internal/forge/types.go          → Forge types changed
#   - internal/config/snapshot.go      → new config field
#   - atlas-web/src/App.tsx            → new web screen added
#   - atlas-web/src/api/contracts.ts   → new API type added
#   - atlas-web/src/api/client.ts      → new client method added

set -euo pipefail

# ── Read hook context ────────────────────────────────────────────────────────

INPUT=$(cat)

# CRITICAL: Prevent infinite loops — if we're already inside a stop-hook
# continuation, exit immediately.
STOP_ACTIVE=$(python3 -c "
import sys, json
try:
    d = json.loads('''$INPUT''')
    print(str(d.get('stop_hook_active', False)).lower())
except:
    print('false')
" 2>/dev/null || echo "false")

if [ "$STOP_ACTIVE" = "true" ]; then
    exit 0
fi

# ── Locate project roots ─────────────────────────────────────────────────────

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ATLAS_ROOT="$(dirname "$SCRIPT_DIR")"          # Atlas/
PROJECT_ROOT="$(dirname "$ATLAS_ROOT")"        # Project Atlas/
CLAUDE_MD="$ATLAS_ROOT/CLAUDE.md"

if [ ! -f "$CLAUDE_MD" ]; then
    exit 0
fi

# ── Detect structural changes ────────────────────────────────────────────────

cd "$PROJECT_ROOT"

CHANGED=$(git diff HEAD --name-only 2>/dev/null || echo "")

if [ -z "$CHANGED" ]; then
    exit 0
fi

SIGNAL_PATTERNS=(
    "internal/skills/registry.go"
    "internal/features/skills.go"
    "internal/domain/"
    "internal/forge/types.go"
    "internal/config/snapshot.go"
    "atlas-web/src/App.tsx"
    "atlas-web/src/api/contracts.ts"
    "atlas-web/src/api/client.ts"
)

MATCHED=()
for pattern in "${SIGNAL_PATTERNS[@]}"; do
    while IFS= read -r file; do
        if [[ "$file" == *"$pattern"* ]]; then
            MATCHED+=("$PROJECT_ROOT/$file")
        fi
    done <<< "$CHANGED"
done

# Deduplicate
if [ ${#MATCHED[@]} -eq 0 ]; then
    exit 0
fi

UNIQUE_MATCHED=($(printf '%s\n' "${MATCHED[@]}" | sort -u))

# ── Build focused update prompt ───────────────────────────────────────────────

FILE_LIST=""
for f in "${UNIQUE_MATCHED[@]}"; do
    FILE_LIST+="  - $f"$'\n'
done

PROMPT="You are updating the CLAUDE.md navigation reference for Project Atlas.

The following structural files changed in this session:
$FILE_LIST
Instructions:
1. Read CLAUDE.md at: $CLAUDE_MD
2. Read each changed file listed above.
3. Identify what specifically changed (new skill, new route, new config field, new screen, new API type).
4. Edit CLAUDE.md — update ONLY the sections directly affected by these changes.
5. Do not rewrite, reformat, or touch sections that are unaffected.
6. Keep all edits minimal and precise — same style and density as the existing content.

CLAUDE.md path: $CLAUDE_MD"

# ── Run headless Claude to update CLAUDE.md ───────────────────────────────────
# --bare:  skips hooks + CLAUDE.md auto-discovery → no recursion risk
# --print: non-interactive output mode
# --allowedTools: explicitly limits to Read + Edit only

cd "$ATLAS_ROOT"
claude --bare --print \
    --allowedTools "Read,Edit" \
    "$PROMPT" \
    > /dev/null 2>&1

exit 0
