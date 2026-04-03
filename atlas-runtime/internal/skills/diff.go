package skills

import (
	"fmt"
	"strconv"
	"strings"
)

// ── Unified diff generation ───────────────────────────────────────────────────

// UnifiedDiff returns a unified diff comparing oldContent to newContent.
// oldPath / newPath appear in the --- / +++ header lines.
// Returns an empty string if contents are identical.
func UnifiedDiff(oldPath, newPath, oldContent, newContent string) string {
	if oldContent == newContent {
		return ""
	}
	old := diffSplitLines(oldContent)
	nw := diffSplitLines(newContent)
	ops := lcsEdits(old, nw)
	hunks := buildHunks(ops, 3)
	if len(hunks) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "--- %s\n", oldPath)
	fmt.Fprintf(&sb, "+++ %s\n", newPath)
	for _, h := range hunks {
		sb.WriteString(h)
	}
	return sb.String()
}

// diffSplitLines splits s into lines. Each element ends with "\n" except the
// last element when s does not end with a newline.
func diffSplitLines(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for i, p := range parts {
		if i < len(parts)-1 {
			out = append(out, p+"\n")
		} else if p != "" {
			out = append(out, p) // last line, no trailing newline
		}
	}
	return out
}

// ── Edit operations ───────────────────────────────────────────────────────────

type editKind int8

const (
	editKeep editKind = iota
	editDel
	editIns
)

type editOp struct {
	kind editKind
	text string
}

// lcsEdits computes the shortest edit script between old and nw using LCS.
// The result lists operations in forward order (keep/del/ins).
// Deletions are emitted before insertions at the same position.
func lcsEdits(old, nw []string) []editOp {
	m, n := len(old), len(nw)
	if m == 0 && n == 0 {
		return nil
	}

	// Build LCS table. int32 to keep memory manageable for large files.
	dp := make([][]int32, m+1)
	for i := range dp {
		dp[i] = make([]int32, n+1)
	}
	for i := 1; i <= m; i++ {
		for j := 1; j <= n; j++ {
			if old[i-1] == nw[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack to produce ops in reverse order, then flip.
	// Using dp[i][j-1] >= dp[i-1][j] for the insert branch ensures that
	// after reversal, deletions appear before insertions (standard diff style).
	ops := make([]editOp, 0, m+n)
	i, j := m, n
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && old[i-1] == nw[j-1]:
			ops = append(ops, editOp{editKeep, old[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			ops = append(ops, editOp{editIns, nw[j-1]})
			j--
		default:
			ops = append(ops, editOp{editDel, old[i-1]})
			i--
		}
	}
	// Reverse in place to get forward order.
	for l, r := 0, len(ops)-1; l < r; l, r = l+1, r-1 {
		ops[l], ops[r] = ops[r], ops[l]
	}
	return ops
}

// ── Hunk building ─────────────────────────────────────────────────────────────

type lineInfo struct {
	kind    editKind
	text    string
	oldLine int // 1-based; 0 for pure insertions
	newLine int // 1-based; 0 for pure deletions
}

// buildHunks converts edit ops to unified diff hunk strings with ctx context lines.
func buildHunks(ops []editOp, ctx int) []string {
	if len(ops) == 0 {
		return nil
	}

	// Annotate ops with file line numbers.
	infos := make([]lineInfo, len(ops))
	oldN, newN := 0, 0
	for i, op := range ops {
		switch op.kind {
		case editKeep:
			oldN++
			newN++
			infos[i] = lineInfo{editKeep, op.text, oldN, newN}
		case editDel:
			oldN++
			infos[i] = lineInfo{editDel, op.text, oldN, 0}
		case editIns:
			newN++
			infos[i] = lineInfo{editIns, op.text, 0, newN}
		}
	}

	// Mark every line within ctx distance of a change as hunk-eligible.
	inHunk := make([]bool, len(infos))
	for i, inf := range infos {
		if inf.kind == editKeep {
			continue
		}
		lo := max(0, i-ctx)
		hi := min(len(infos), i+ctx+1)
		for j := lo; j < hi; j++ {
			inHunk[j] = true
		}
	}

	// Collect contiguous marked segments as individual hunks.
	var hunks []string
	i := 0
	for i < len(infos) {
		if !inHunk[i] {
			i++
			continue
		}
		j := i
		for j < len(infos) && inHunk[j] {
			j++
		}
		hunks = append(hunks, formatHunk(infos[i:j]))
		i = j
	}
	return hunks
}

func formatHunk(lines []lineInfo) string {
	oldStart, oldCount := 0, 0
	newStart, newCount := 0, 0
	for _, l := range lines {
		if l.kind == editKeep || l.kind == editDel {
			if oldStart == 0 {
				oldStart = l.oldLine
			}
			oldCount++
		}
		if l.kind == editKeep || l.kind == editIns {
			if newStart == 0 {
				newStart = l.newLine
			}
			newCount++
		}
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "@@ -%d,%d +%d,%d @@\n", oldStart, oldCount, newStart, newCount)
	for _, l := range lines {
		var prefix string
		switch l.kind {
		case editKeep:
			prefix = " "
		case editDel:
			prefix = "-"
		case editIns:
			prefix = "+"
		}
		sb.WriteString(prefix)
		sb.WriteString(l.text)
		if !strings.HasSuffix(l.text, "\n") {
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// ── Patch application ─────────────────────────────────────────────────────────

// ApplyPatch applies a unified diff patch to original and returns the result.
// Returns an error if any hunk fails to apply.
func ApplyPatch(original, patch string) (string, error) {
	lines := diffSplitLines(original)
	hunks, err := parseHunks(patch)
	if err != nil {
		return "", fmt.Errorf("patch parse error: %w", err)
	}
	if len(hunks) == 0 {
		return original, nil
	}

	result := make([]string, 0, len(lines)+16)
	cursor := 0 // 0-based index into lines (old file)

	for hi, h := range hunks {
		oldStart := max(0, h.oldStart-1) // convert 1-based → 0-based
		if oldStart < cursor {
			return "", fmt.Errorf("hunk %d: overlaps previous hunk (old line %d, cursor %d)",
				hi+1, h.oldStart, cursor+1)
		}
		// Copy unchanged lines between cursor and hunk start.
		result = append(result, lines[cursor:oldStart]...)
		cursor = oldStart

		for _, op := range h.ops {
			switch op.kind {
			case editKeep:
				if cursor >= len(lines) {
					return "", fmt.Errorf("hunk %d: context line beyond end of file: %q", hi+1, op.text)
				}
				if stripNL(lines[cursor]) != stripNL(op.text) {
					return "", fmt.Errorf("hunk %d: context mismatch at line %d: expected %q got %q",
						hi+1, cursor+1, stripNL(op.text), stripNL(lines[cursor]))
				}
				result = append(result, lines[cursor])
				cursor++
			case editDel:
				if cursor >= len(lines) {
					return "", fmt.Errorf("hunk %d: deletion beyond end of file: %q", hi+1, op.text)
				}
				if stripNL(lines[cursor]) != stripNL(op.text) {
					return "", fmt.Errorf("hunk %d: deletion mismatch at line %d: expected %q got %q",
						hi+1, cursor+1, stripNL(op.text), stripNL(lines[cursor]))
				}
				cursor++ // skip (delete)
			case editIns:
				result = append(result, op.text)
			}
		}
	}
	// Append remaining lines after the last hunk.
	result = append(result, lines[cursor:]...)
	return strings.Join(result, ""), nil
}

func stripNL(s string) string {
	return strings.TrimRight(s, "\r\n")
}

// ── Patch parser ──────────────────────────────────────────────────────────────

type diffHunk struct {
	oldStart int
	oldCount int
	newStart int
	newCount int
	ops      []editOp
}

// parseHunks parses unified diff text into structured hunks.
// Skips --- / +++ header lines.
func parseHunks(patch string) ([]diffHunk, error) {
	var hunks []diffHunk
	var cur *diffHunk

	for _, rawLine := range strings.Split(patch, "\n") {
		if strings.HasPrefix(rawLine, "--- ") || strings.HasPrefix(rawLine, "+++ ") {
			continue
		}
		if strings.HasPrefix(rawLine, "@@") {
			h, err := parseHunkHeader(rawLine)
			if err != nil {
				return nil, err
			}
			hunks = append(hunks, h)
			cur = &hunks[len(hunks)-1]
			continue
		}
		if cur == nil || len(rawLine) == 0 {
			continue
		}
		prefix, rest := rawLine[0], rawLine[1:]+"\n"
		switch prefix {
		case ' ':
			cur.ops = append(cur.ops, editOp{editKeep, rest})
		case '-':
			cur.ops = append(cur.ops, editOp{editDel, rest})
		case '+':
			cur.ops = append(cur.ops, editOp{editIns, rest})
		case '\\':
			// "\ No newline at end of file" — strip the \n we added to the previous op.
			if len(cur.ops) > 0 {
				last := &cur.ops[len(cur.ops)-1]
				last.text = strings.TrimSuffix(last.text, "\n")
			}
		}
	}
	return hunks, nil
}

func parseHunkHeader(line string) (diffHunk, error) {
	// Expected: "@@ -a,b +c,d @@ ..."
	var h diffHunk
	after, ok := strings.CutPrefix(line, "@@ ")
	if !ok {
		return h, fmt.Errorf("invalid hunk header: %q", line)
	}
	fields := strings.Fields(after)
	if len(fields) < 2 {
		return h, fmt.Errorf("invalid hunk header fields: %q", line)
	}
	var err error
	h.oldStart, h.oldCount, err = parseDiffRange(fields[0])
	if err != nil {
		return h, fmt.Errorf("bad old range in %q: %w", line, err)
	}
	h.newStart, h.newCount, err = parseDiffRange(fields[1])
	if err != nil {
		return h, fmt.Errorf("bad new range in %q: %w", line, err)
	}
	return h, nil
}

// parseDiffRange parses "-a,b" or "+a,b" or "-a" (count defaults to 1).
func parseDiffRange(s string) (start, count int, err error) {
	if len(s) == 0 {
		return 0, 0, fmt.Errorf("empty range")
	}
	s = s[1:] // strip leading - or +
	if idx := strings.Index(s, ","); idx >= 0 {
		start, err = strconv.Atoi(s[:idx])
		if err != nil {
			return 0, 0, err
		}
		count, err = strconv.Atoi(s[idx+1:])
		return start, count, err
	}
	start, err = strconv.Atoi(s)
	return start, 1, err
}
