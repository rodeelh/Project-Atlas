package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

func (r *Registry) registerTerminal() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "terminal.run_command",
			Description: "Run a shell command by executable name and argument list. The command is executed directly (no shell), preventing injection. Returns combined stdout+stderr with the exit code.",
			Properties: map[string]ToolParam{
				"command": {
					Description: "The executable to run (e.g. 'git', 'ls', 'python3')",
					Type:        "string",
				},
				"args": {
					Description: "Arguments to pass to the command — each element is a separate argument, not a shell string",
					Type:        "array",
					Items:       &ToolParam{Type: "string"},
				},
				"workingDir": {
					Description: "Absolute path to run the command from (optional, defaults to user home directory)",
					Type:        "string",
				},
			},
			Required: []string{"command"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassDestructiveLocal,
		Fn:          terminalRunCommand,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "terminal.run_script",
			Description: "Execute a multi-line shell script via /bin/sh. Supports pipes, redirects, and shell expansion. Every call requires user approval unless auto_approve is set.",
			Properties: map[string]ToolParam{
				"script": {
					Description: "Shell script to execute (passed to /bin/sh -c)",
					Type:        "string",
				},
				"workingDir": {
					Description: "Absolute path to run the script from (optional, defaults to user home directory)",
					Type:        "string",
				},
			},
			Required: []string{"script"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassDestructiveLocal, // full shell — highest local risk
		Fn:          terminalRunScript,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "terminal.read_env",
			Description: "Read one or more environment variable values. Pass an empty array to list all non-sensitive variable names (values are not returned in list mode).",
			Properties: map[string]ToolParam{
				"keys": {
					Description: "Environment variable names to read (e.g. ['HOME', 'PATH']). Pass empty array to list all available non-sensitive variable names.",
					Type:        "array",
					Items:       &ToolParam{Type: "string"},
				},
			},
			Required: []string{"keys"},
		},
		PermLevel: "read",
		Fn:        terminalReadEnv,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "terminal.list_processes",
			Description: "List running processes, sorted by CPU usage. Optionally filter by process name substring.",
			Properties: map[string]ToolParam{
				"filter": {
					Description: "Optional name substring to filter results (case-insensitive)",
					Type:        "string",
				},
			},
			Required: []string{},
		},
		PermLevel: "read",
		Fn:        terminalListProcesses,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "terminal.kill_process",
			Description: "Send a signal to a process by PID. Defaults to SIGTERM. Use SIGKILL only if SIGTERM fails.",
			Properties: map[string]ToolParam{
				"pid": {
					Description: "Process ID to signal",
					Type:        "integer",
				},
				"signal": {
					Description: "Signal to send (default: TERM)",
					Type:        "string",
					Enum:        []string{"TERM", "KILL", "HUP", "INT"},
				},
			},
			Required: []string{"pid"},
		},
		PermLevel:   "execute",
		ActionClass: ActionClassDestructiveLocal,
		Fn:          terminalKillProcess,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "terminal.get_working_directory",
			Description: "Returns the current working directory of the Atlas runtime process.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel: "read",
		Fn:        terminalGetWorkingDirectory,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "terminal.which",
			Description: "Locate the full path of a command on PATH, or report that it is not installed.",
			Properties: map[string]ToolParam{
				"command": {
					Description: "Command name to locate (e.g. 'git', 'python3', 'ffmpeg')",
					Type:        "string",
				},
			},
			Required: []string{"command"},
		},
		PermLevel: "read",
		Fn:        terminalWhich,
	})
}

// ── constants & helpers ───────────────────────────────────────────────────────

const terminalMaxOutput = 8 * 1024 // 8 KB

// terminalBlocklist contains bare binary names that are blocked in run_command.
// run_script requires explicit user approval on every call, so it is not blocked here.
// Includes download and interpreter tools that could be used to fetch and execute
// arbitrary remote code — those remain available via run_script (which always asks).
var terminalBlocklist = []string{
	"rm", "rmdir", "mkfs", "dd", "shred", "fdisk",
	"sudo", "su", "chmod", "chown", "visudo",
	"curl", "wget", "python", "python3", "ruby", "node", "perl", "php",
}

// terminalSecretPatterns are substrings that mark an env var name as sensitive.
var terminalSecretPatterns = []string{
	"TOKEN", "KEY", "SECRET", "PASSWORD", "PASS", "AUTH", "CREDENTIAL", "CERT", "PRIVATE",
}

func terminalTruncate(s string) string {
	if len(s) <= terminalMaxOutput {
		return s
	}
	omitted := len(s) - terminalMaxOutput
	return s[:terminalMaxOutput] + fmt.Sprintf("\n[truncated — %d bytes omitted]", omitted)
}

func terminalCheckBlocklist(name string) error {
	base := strings.ToLower(filepath.Base(name))
	for _, blocked := range terminalBlocklist {
		if base == blocked {
			return fmt.Errorf("command %q is blocked for safety — use terminal.run_script (which requires explicit approval) if you truly need this", name)
		}
	}
	return nil
}

func terminalValidateWorkDir(dir string) error {
	if dir == "" {
		return nil
	}
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("workingDir must be an absolute path, got: %q", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("workingDir does not exist: %q", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("workingDir is not a directory: %q", dir)
	}
	return nil
}

// terminalDefaultDir returns the user's home directory as the default working dir.
func terminalDefaultDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return "/"
}

// terminalIsSensitiveEnvKey returns true if the variable name looks like a secret.
func terminalIsSensitiveEnvKey(name string) bool {
	upper := strings.ToUpper(name)
	for _, pat := range terminalSecretPatterns {
		if strings.Contains(upper, pat) {
			return true
		}
	}
	return false
}

// ── handlers ──────────────────────────────────────────────────────────────────

func terminalRunCommand(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Command    string   `json:"command"`
		Args       []string `json:"args"`
		WorkingDir string   `json:"workingDir"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	if err := terminalCheckBlocklist(p.Command); err != nil {
		return "", err
	}

	cwd := p.WorkingDir
	if cwd == "" {
		cwd = terminalDefaultDir()
	}
	if err := terminalValidateWorkDir(cwd); err != nil {
		return "", err
	}

	// Cannot use runCmd helper — need to set cmd.Dir.
	ctx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.Command, p.Args...)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", fmt.Errorf("failed to run %q: %w", p.Command, err)
		}
	}

	result := fmt.Sprintf("[exit %d]\n%s", exitCode, strings.TrimSpace(string(out)))
	return terminalTruncate(result), nil
}

func terminalRunScript(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Script     string `json:"script"`
		WorkingDir string `json:"workingDir"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Script == "" {
		return "", fmt.Errorf("script is required")
	}

	cwd := p.WorkingDir
	if cwd == "" {
		cwd = terminalDefaultDir()
	}
	if err := terminalValidateWorkDir(cwd); err != nil {
		return "", err
	}

	// Cannot use runCmd helper — need to set cmd.Dir.
	ctx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", p.Script)
	cmd.Dir = cwd
	out, err := cmd.CombinedOutput()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return "", fmt.Errorf("failed to run script: %w", err)
		}
	}

	result := fmt.Sprintf("[exit %d]\n%s", exitCode, strings.TrimSpace(string(out)))
	return terminalTruncate(result), nil
}

func terminalReadEnv(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Keys []string `json:"keys"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("keys is required")
	}

	// Value lookup mode.
	if len(p.Keys) > 0 {
		result := make(map[string]interface{}, len(p.Keys))
		for _, k := range p.Keys {
			if terminalIsSensitiveEnvKey(k) {
				result[k] = "[REDACTED — sensitive key]"
				continue
			}
			v, ok := os.LookupEnv(k)
			if ok {
				result[k] = v
			} else {
				result[k] = nil
			}
		}
		b, _ := json.MarshalIndent(result, "", "  ")
		return string(b), nil
	}

	// List-all mode — names only, secrets scrubbed.
	all := os.Environ()
	var names []string
	for _, pair := range all {
		name := pair
		if idx := strings.IndexByte(pair, '='); idx >= 0 {
			name = pair[:idx]
		}
		if !terminalIsSensitiveEnvKey(name) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, "\n"), nil
}

func terminalListProcesses(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Filter string `json:"filter"`
	}
	// Ignore unmarshal errors — filter is optional.
	json.Unmarshal(args, &p) //nolint:errcheck

	out, err := runCmd(ctx, "ps", "aux")
	if err != nil {
		return "", fmt.Errorf("failed to list processes: %w", err)
	}

	lines := strings.Split(out, "\n")
	if len(lines) == 0 {
		return "No processes found.", nil
	}

	// Keep the header.
	var result []string
	result = append(result, lines[0])

	filter := strings.ToLower(p.Filter)
	count := 0
	for _, line := range lines[1:] {
		if count >= 50 {
			break
		}
		if filter != "" && !strings.Contains(strings.ToLower(line), filter) {
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		result = append(result, line)
		count++
	}

	return strings.Join(result, "\n"), nil
}

func terminalKillProcess(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		PID    int    `json:"pid"`
		Signal string `json:"signal"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.PID == 0 {
		return "", fmt.Errorf("pid is required")
	}

	// Safety guards.
	if p.PID <= 0 {
		return "", fmt.Errorf("invalid PID: %d", p.PID)
	}
	if p.PID < 100 {
		return "", fmt.Errorf("refusing to signal PID %d — system/kernel processes (PID < 100) are protected", p.PID)
	}
	if p.PID == os.Getpid() {
		return "", fmt.Errorf("refusing to signal the Atlas runtime process itself (PID %d)", p.PID)
	}

	sig := p.Signal
	if sig == "" {
		sig = "TERM"
	}
	validSignals := map[string]bool{"TERM": true, "KILL": true, "HUP": true, "INT": true}
	if !validSignals[sig] {
		return "", fmt.Errorf("invalid signal %q — allowed: TERM, KILL, HUP, INT", sig)
	}

	ctx, cancel := context.WithTimeout(ctx, shellTimeout)
	defer cancel()

	if _, err := runCmd(ctx, "kill", "-"+sig, strconv.Itoa(p.PID)); err != nil {
		return "", fmt.Errorf("failed to signal PID %d: %w", p.PID, err)
	}

	return fmt.Sprintf("Sent SIG%s to PID %d.", sig, p.PID), nil
}

func terminalGetWorkingDirectory(_ context.Context, _ json.RawMessage) (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("could not determine working directory: %w", err)
	}
	return cwd, nil
}

func terminalWhich(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(args, &p); err != nil || p.Command == "" {
		return "", fmt.Errorf("command is required")
	}

	out, err := runCmd(ctx, "which", p.Command)
	if err != nil {
		return "command not found: " + p.Command, nil
	}
	return strings.TrimSpace(out), nil
}
