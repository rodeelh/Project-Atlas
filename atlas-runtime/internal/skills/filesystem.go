package skills

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// errWalkLimit is returned by WalkDir callbacks to stop early once the result
// cap is reached. Using a named sentinel avoids fragile string comparisons.
var errWalkLimit = errors.New("walk limit reached")

const (
	noRootsMsg  = "No file system roots approved. Add approved directories in Atlas Settings → Skills → File System."
	maxFileSize = 50 * 1024 // 50KB
)

func (r *Registry) registerFilesystem() {
	supportDir := r.supportDir

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "fs.list_directory",
			Description: "Lists files and directories at the given path (must be within an approved root).",
			Properties: map[string]ToolParam{
				"path": {Description: "Absolute path to the directory to list", Type: "string"},
			},
			Required: []string{"path"},
		},
		PermLevel: "read",
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return fsListDirectory(ctx, args, supportDir)
		},
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "fs.read_file",
			Description: "Reads the content of a file (max 50KB, must be within an approved root).",
			Properties: map[string]ToolParam{
				"path": {Description: "Absolute path to the file to read", Type: "string"},
			},
			Required: []string{"path"},
		},
		PermLevel: "read",
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return fsReadFile(ctx, args, supportDir)
		},
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "fs.get_metadata",
			Description: "Returns size, modification time, and type for a path (must be within an approved root).",
			Properties: map[string]ToolParam{
				"path": {Description: "Absolute path to the file or directory", Type: "string"},
			},
			Required: []string{"path"},
		},
		PermLevel: "read",
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return fsGetMetadata(ctx, args, supportDir)
		},
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "fs.search",
			Description: "Finds files matching a glob pattern under a path (must be within an approved root).",
			Properties: map[string]ToolParam{
				"path":    {Description: "Root directory to search", Type: "string"},
				"pattern": {Description: "Glob pattern (e.g. '*.go', '**/*.txt')", Type: "string"},
			},
			Required: []string{"path", "pattern"},
		},
		PermLevel: "read",
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return fsSearch(ctx, args, supportDir)
		},
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "fs.content_search",
			Description: "Searches file contents for a text query under a path (must be within an approved root).",
			Properties: map[string]ToolParam{
				"path":  {Description: "Root directory to search", Type: "string"},
				"query": {Description: "Text to search for in file contents", Type: "string"},
			},
			Required: []string{"path", "query"},
		},
		PermLevel: "read",
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return fsContentSearch(ctx, args, supportDir)
		},
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "fs.write_file",
			Description: "Writes content to a file (creates or overwrites). Requires an approved root. Returns a unified diff showing what changed.",
			Properties: map[string]ToolParam{
				"path":           {Description: "Absolute path to write to", Type: "string"},
				"content":        {Description: "Full content to write", Type: "string"},
				"create_parents": {Description: "Create missing parent directories if true", Type: "boolean"},
			},
			Required: []string{"path", "content"},
		},
		PermLevel: "draft",
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return fsWriteFile(ctx, args, supportDir)
		},
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "fs.patch_file",
			Description: "Applies a unified diff patch to an existing file. Use this for targeted edits to large files. Requires an approved root.",
			Properties: map[string]ToolParam{
				"path":  {Description: "Absolute path to the file to patch", Type: "string"},
				"patch": {Description: "Unified diff patch string (--- / +++ / @@ hunks)", Type: "string"},
			},
			Required: []string{"path", "patch"},
		},
		PermLevel: "draft",
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return fsPatchFile(ctx, args, supportDir)
		},
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "fs.create_directory",
			Description: "Creates a directory at the given path (must be within an approved root).",
			Properties: map[string]ToolParam{
				"path":           {Description: "Absolute path of the directory to create", Type: "string"},
				"create_parents": {Description: "Create missing parent directories if true (like mkdir -p)", Type: "boolean"},
			},
			Required: []string{"path"},
		},
		PermLevel: "draft",
		Fn: func(ctx context.Context, args json.RawMessage) (string, error) {
			return fsCreateDirectory(ctx, args, supportDir)
		},
	})
}

// ── approved roots ────────────────────────────────────────────────────────────

// FsRoot is a single approved file-system root entry persisted in go-fs-roots.json.
type FsRoot struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

// NewFsRootID generates a random 8-byte hex ID for a new root entry.
func NewFsRootID() string {
	b := make([]byte, 8)
	rand.Read(b) //nolint:errcheck
	return hex.EncodeToString(b)
}

// LoadFsRoots reads go-fs-roots.json and returns the approved roots.
// Supports both the legacy []string format and the current []FsRoot format.
func LoadFsRoots(supportDir string) ([]FsRoot, error) {
	data, err := os.ReadFile(filepath.Join(supportDir, "go-fs-roots.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// Try new []FsRoot format first.
	var roots []FsRoot
	if err := json.Unmarshal(data, &roots); err == nil {
		return roots, nil
	}
	// Fall back to legacy []string format and migrate IDs on the fly.
	var paths []string
	if err := json.Unmarshal(data, &paths); err != nil {
		return nil, err
	}
	out := make([]FsRoot, len(paths))
	for i, p := range paths {
		out[i] = FsRoot{ID: NewFsRootID(), Path: p}
	}
	return out, nil
}

// SaveFsRoots writes roots atomically to go-fs-roots.json.
func SaveFsRoots(supportDir string, roots []FsRoot) error {
	data, err := json.MarshalIndent(roots, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(supportDir, "go-fs-roots.json")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// loadApprovedRoots returns just the path strings for internal fs enforcement.
func loadApprovedRoots(supportDir string) ([]string, error) {
	roots, err := LoadFsRoots(supportDir)
	if err != nil {
		return nil, err
	}
	paths := make([]string, len(roots))
	for i, r := range roots {
		paths[i] = r.Path
	}
	return paths, nil
}

func checkApproved(path string, roots []string) error {
	if len(roots) == 0 {
		return fmt.Errorf(noRootsMsg)
	}
	clean := filepath.Clean(path)
	for _, root := range roots {
		cleanRoot := filepath.Clean(root)
		if strings.HasPrefix(clean, cleanRoot+string(filepath.Separator)) || clean == cleanRoot {
			return nil
		}
	}
	return fmt.Errorf("path %q is not within any approved root. Approved roots: %s", path, strings.Join(roots, ", "))
}

// ── fs.list_directory ─────────────────────────────────────────────────────────

func fsListDirectory(_ context.Context, args json.RawMessage, supportDir string) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	roots, err := loadApprovedRoots(supportDir)
	if err != nil {
		return "", err
	}
	if err := checkApproved(p.Path, roots); err != nil {
		return "", err
	}

	entries, err := os.ReadDir(p.Path)
	if err != nil {
		return "", fmt.Errorf("could not read directory: %w", err)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Contents of %s:\n", p.Path))
	for _, e := range entries {
		info, _ := e.Info()
		typeMark := ""
		if e.IsDir() {
			typeMark = "/"
		}
		size := ""
		if info != nil && !e.IsDir() {
			size = fmt.Sprintf(" (%d bytes)", info.Size())
		}
		sb.WriteString(fmt.Sprintf("  %s%s%s\n", e.Name(), typeMark, size))
	}
	if len(entries) == 0 {
		sb.WriteString("  (empty directory)\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── fs.read_file ──────────────────────────────────────────────────────────────

func fsReadFile(_ context.Context, args json.RawMessage, supportDir string) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	roots, err := loadApprovedRoots(supportDir)
	if err != nil {
		return "", err
	}
	if err := checkApproved(p.Path, roots); err != nil {
		return "", err
	}

	f, err := os.Open(p.Path)
	if err != nil {
		return "", fmt.Errorf("could not open file: %w", err)
	}
	defer f.Close()

	content, err := io.ReadAll(io.LimitReader(f, maxFileSize))
	if err != nil {
		return "", fmt.Errorf("could not read file: %w", err)
	}

	result := string(content)
	info, _ := f.Stat()
	if info != nil && info.Size() > maxFileSize {
		result += "\n... [file truncated at 50KB]"
	}
	return result, nil
}

// ── fs.get_metadata ───────────────────────────────────────────────────────────

func fsGetMetadata(_ context.Context, args json.RawMessage, supportDir string) (string, error) {
	var p struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	roots, err := loadApprovedRoots(supportDir)
	if err != nil {
		return "", err
	}
	if err := checkApproved(p.Path, roots); err != nil {
		return "", err
	}

	info, err := os.Stat(p.Path)
	if err != nil {
		return "", fmt.Errorf("could not stat path: %w", err)
	}

	fileType := "file"
	if info.IsDir() {
		fileType = "directory"
	}

	return fmt.Sprintf("Path: %s\nType: %s\nSize: %d bytes\nModified: %s\nMode: %s",
		p.Path, fileType, info.Size(),
		info.ModTime().UTC().Format("2006-01-02 15:04:05 UTC"),
		info.Mode().String(),
	), nil
}

// ── fs.search ─────────────────────────────────────────────────────────────────

func fsSearch(_ context.Context, args json.RawMessage, supportDir string) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Pattern string `json:"pattern"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Path == "" || p.Pattern == "" {
		return "", fmt.Errorf("path and pattern are required")
	}

	roots, err := loadApprovedRoots(supportDir)
	if err != nil {
		return "", err
	}
	if err := checkApproved(p.Path, roots); err != nil {
		return "", err
	}

	var matches []string
	err = filepath.WalkDir(p.Path, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil // skip unreadable dirs
		}
		matched, matchErr := filepath.Match(p.Pattern, d.Name())
		if matchErr != nil {
			return matchErr
		}
		if matched {
			matches = append(matches, path)
		}
		if len(matches) >= 100 {
			return errWalkLimit
		}
		return nil
	})
	if err != nil && !errors.Is(err, errWalkLimit) {
		return "", fmt.Errorf("search error: %w", err)
	}

	if len(matches) == 0 {
		return fmt.Sprintf("No files matching %q found under %s", p.Pattern, p.Path), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Files matching %q under %s:\n", p.Pattern, p.Path))
	for _, m := range matches {
		sb.WriteString("  " + m + "\n")
	}
	if len(matches) == 100 {
		sb.WriteString("  ... (results limited to 100)\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── fs.content_search ─────────────────────────────────────────────────────────

func fsContentSearch(_ context.Context, args json.RawMessage, supportDir string) (string, error) {
	var p struct {
		Path  string `json:"path"`
		Query string `json:"query"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Path == "" || p.Query == "" {
		return "", fmt.Errorf("path and query are required")
	}

	roots, err := loadApprovedRoots(supportDir)
	if err != nil {
		return "", err
	}
	if err := checkApproved(p.Path, roots); err != nil {
		return "", err
	}

	type match struct {
		File string
		Line int
		Text string
	}
	var matches []match

	err = filepath.WalkDir(p.Path, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() {
			return nil
		}
		// Skip large files.
		info, _ := d.Info()
		if info != nil && info.Size() > maxFileSize {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}

		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if strings.Contains(line, p.Query) {
				matches = append(matches, match{File: path, Line: i + 1, Text: strings.TrimSpace(line)})
				if len(matches) >= 50 {
					return errWalkLimit
				}
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, errWalkLimit) {
		return "", fmt.Errorf("content search error: %w", err)
	}

	if len(matches) == 0 {
		return fmt.Sprintf("No files containing %q found under %s", p.Query, p.Path), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Files containing %q under %s:\n", p.Query, p.Path))
	for _, m := range matches {
		// Trim long lines.
		text := m.Text
		if len(text) > 120 {
			text = text[:120] + "..."
		}
		sb.WriteString(fmt.Sprintf("  %s:%d: %s\n", m.File, m.Line, text))
	}
	if len(matches) == 50 {
		sb.WriteString("  ... (results limited to 50)\n")
	}
	return strings.TrimRight(sb.String(), "\n"), nil
}

// ── fs.write_file ─────────────────────────────────────────────────────────────

func fsWriteFile(_ context.Context, args json.RawMessage, supportDir string) (string, error) {
	var p struct {
		Path          string `json:"path"`
		Content       string `json:"content"`
		CreateParents bool   `json:"create_parents"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Path == "" {
		return "", fmt.Errorf("path and content are required")
	}

	roots, err := loadApprovedRoots(supportDir)
	if err != nil {
		return "", err
	}
	if err := checkApproved(p.Path, roots); err != nil {
		return "", err
	}

	if p.CreateParents {
		if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
			return "", fmt.Errorf("could not create parent directories: %w", err)
		}
	}

	// Read existing content so we can generate a diff.
	var oldContent string
	isNew := false
	existing, readErr := os.ReadFile(p.Path)
	if os.IsNotExist(readErr) {
		isNew = true
	} else if readErr != nil {
		return "", fmt.Errorf("could not read existing file: %w", readErr)
	} else {
		oldContent = string(existing)
	}

	// Atomic write: temp file in same directory, then rename.
	dir := filepath.Dir(p.Path)
	tmp, err := os.CreateTemp(dir, ".atlas-write-*.tmp")
	if err != nil {
		return "", fmt.Errorf("could not create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(p.Content); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("could not write file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("could not close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, p.Path); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("could not rename file: %w", err)
	}

	if isNew {
		return fmt.Sprintf("Created %s (%d bytes)", p.Path, len(p.Content)), nil
	}
	diff := UnifiedDiff(p.Path, p.Path, oldContent, p.Content)
	if diff == "" {
		return fmt.Sprintf("No changes — %s content is identical", p.Path), nil
	}
	return fmt.Sprintf("Updated %s\n\n%s", p.Path, diff), nil
}

// ── fs.patch_file ─────────────────────────────────────────────────────────────

func fsPatchFile(_ context.Context, args json.RawMessage, supportDir string) (string, error) {
	var p struct {
		Path  string `json:"path"`
		Patch string `json:"patch"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Path == "" || p.Patch == "" {
		return "", fmt.Errorf("path and patch are required")
	}

	roots, err := loadApprovedRoots(supportDir)
	if err != nil {
		return "", err
	}
	if err := checkApproved(p.Path, roots); err != nil {
		return "", err
	}

	existing, err := os.ReadFile(p.Path)
	if err != nil {
		return "", fmt.Errorf("could not read file: %w", err)
	}
	oldContent := string(existing)

	newContent, err := ApplyPatch(oldContent, p.Patch)
	if err != nil {
		return "", fmt.Errorf("patch failed: %w", err)
	}

	// Atomic write.
	dir := filepath.Dir(p.Path)
	tmp, err := os.CreateTemp(dir, ".atlas-patch-*.tmp")
	if err != nil {
		return "", fmt.Errorf("could not create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.WriteString(newContent); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return "", fmt.Errorf("could not write patched content: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("could not close temp file: %w", err)
	}
	if err := os.Rename(tmpPath, p.Path); err != nil {
		os.Remove(tmpPath)
		return "", fmt.Errorf("could not rename patched file: %w", err)
	}

	diff := UnifiedDiff(p.Path, p.Path, oldContent, newContent)
	return fmt.Sprintf("Patched %s\n\n%s", p.Path, diff), nil
}

// ── fs.create_directory ───────────────────────────────────────────────────────

func fsCreateDirectory(_ context.Context, args json.RawMessage, supportDir string) (string, error) {
	var p struct {
		Path          string `json:"path"`
		CreateParents bool   `json:"create_parents"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Path == "" {
		return "", fmt.Errorf("path is required")
	}

	roots, err := loadApprovedRoots(supportDir)
	if err != nil {
		return "", err
	}
	if err := checkApproved(p.Path, roots); err != nil {
		return "", err
	}

	if p.CreateParents {
		if err := os.MkdirAll(p.Path, 0o755); err != nil {
			return "", fmt.Errorf("could not create directories: %w", err)
		}
		return fmt.Sprintf("Created directory (with parents): %s", p.Path), nil
	}
	if err := os.Mkdir(p.Path, 0o755); err != nil {
		return "", fmt.Errorf("could not create directory: %w", err)
	}
	return fmt.Sprintf("Created directory: %s", p.Path), nil
}
