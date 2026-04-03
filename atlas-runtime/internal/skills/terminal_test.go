package skills

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// ── terminalCheckBlocklist ────────────────────────────────────────────────────

func TestTerminalBlocklist_BlockedNames(t *testing.T) {
	blocked := []string{
		"rm", "rmdir", "mkfs", "dd", "shred", "fdisk",
		"sudo", "su", "chmod", "chown", "visudo",
		"curl", "wget", "python", "python3", "ruby", "node", "perl", "php",
	}
	for _, name := range blocked {
		if err := terminalCheckBlocklist(name); err == nil {
			t.Errorf("expected %q to be blocked, but it was allowed", name)
		}
	}
}

func TestTerminalBlocklist_AllowedNames(t *testing.T) {
	allowed := []string{"git", "ls", "cat", "echo", "grep", "find", "which", "env"}
	for _, name := range allowed {
		if err := terminalCheckBlocklist(name); err != nil {
			t.Errorf("expected %q to be allowed, got error: %v", name, err)
		}
	}
}

func TestTerminalBlocklist_PathPrefixed(t *testing.T) {
	// Blocked even when a full path is supplied — filepath.Base strips the prefix.
	paths := []string{"/bin/rm", "/usr/bin/sudo", "/usr/local/bin/python3"}
	for _, p := range paths {
		if err := terminalCheckBlocklist(p); err == nil {
			t.Errorf("expected path %q to be blocked, but it was allowed", p)
		}
	}
}

func TestTerminalBlocklist_CaseInsensitive(t *testing.T) {
	// macOS is case-insensitive; the check lowercases the base name.
	if err := terminalCheckBlocklist("RM"); err == nil {
		t.Error("expected 'RM' to be blocked (case-insensitive)")
	}
}

// ── terminalIsSensitiveEnvKey ─────────────────────────────────────────────────

func TestIsSensitiveEnvKey_Sensitive(t *testing.T) {
	sensitive := []string{
		"ANTHROPIC_API_KEY", "OPENAI_API_KEY", "SECRET", "PASSWORD",
		"AUTH_TOKEN", "PRIVATE_KEY", "CREDENTIAL", "CERT_PATH",
		"DB_PASS", "AWS_SECRET_ACCESS_KEY",
	}
	for _, k := range sensitive {
		if !terminalIsSensitiveEnvKey(k) {
			t.Errorf("expected %q to be classified as sensitive", k)
		}
	}
}

func TestIsSensitiveEnvKey_NonSensitive(t *testing.T) {
	nonSensitive := []string{
		"HOME", "PATH", "TERM", "SHELL", "USER", "LANG",
		"GOPATH", "GOROOT", "PWD", "TMPDIR",
	}
	for _, k := range nonSensitive {
		if terminalIsSensitiveEnvKey(k) {
			t.Errorf("expected %q to be non-sensitive, but it was flagged", k)
		}
	}
}

// ── terminalKillProcess safety guards ────────────────────────────────────────

func TestKillProcess_ZeroPID(t *testing.T) {
	args := json.RawMessage(`{"pid":0}`)
	_, err := terminalKillProcess(context.Background(), args)
	if err == nil {
		t.Error("expected error for PID 0")
	}
}

func TestKillProcess_NegativePID(t *testing.T) {
	args := json.RawMessage(`{"pid":-1}`)
	_, err := terminalKillProcess(context.Background(), args)
	if err == nil {
		t.Error("expected error for negative PID")
	}
}

func TestKillProcess_SystemPID(t *testing.T) {
	// PIDs < 100 are system/kernel processes and must be refused.
	for _, pid := range []int{1, 2, 50, 99} {
		data, _ := json.Marshal(map[string]int{"pid": pid})
		_, err := terminalKillProcess(context.Background(), data)
		if err == nil {
			t.Errorf("expected error for system PID %d", pid)
		}
		if err != nil && !strings.Contains(err.Error(), "protected") && !strings.Contains(err.Error(), "required") {
			t.Errorf("PID %d: unexpected error text: %v", pid, err)
		}
	}
}

func TestKillProcess_OwnPID(t *testing.T) {
	data, _ := json.Marshal(map[string]int{"pid": os.Getpid()})
	_, err := terminalKillProcess(context.Background(), data)
	if err == nil {
		t.Error("expected error when signalling own PID")
	}
}

func TestKillProcess_InvalidSignal(t *testing.T) {
	// Use a real-looking but disallowed signal name.
	args := json.RawMessage(`{"pid":1000,"signal":"USR1"}`)
	_, err := terminalKillProcess(context.Background(), args)
	if err == nil {
		t.Error("expected error for invalid signal USR1")
	}
}

// ── terminalReadEnv — sensitive key redaction in lookup mode ─────────────────

func TestReadEnv_SensitiveKeysAreRedacted(t *testing.T) {
	// Plant a fake secret in the environment for this test.
	os.Setenv("ATLAS_TEST_SECRET_KEY", "super-secret-value")
	defer os.Unsetenv("ATLAS_TEST_SECRET_KEY")

	args := json.RawMessage(`{"keys":["ATLAS_TEST_SECRET_KEY","HOME"]}`)
	out, err := terminalReadEnv(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("output is not valid JSON: %v\n%s", err, out)
	}

	if result["ATLAS_TEST_SECRET_KEY"] != "[REDACTED — sensitive key]" {
		t.Errorf("sensitive key not redacted: got %v", result["ATLAS_TEST_SECRET_KEY"])
	}
	// Non-sensitive key should still be returned.
	if result["HOME"] == nil || result["HOME"] == "[REDACTED — sensitive key]" {
		t.Errorf("non-sensitive key HOME should have its value, got: %v", result["HOME"])
	}
}

func TestReadEnv_NonSensitiveKeysReturned(t *testing.T) {
	args := json.RawMessage(`{"keys":["HOME","PATH"]}`)
	out, err := terminalReadEnv(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result map[string]interface{}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["HOME"] == nil {
		t.Error("HOME should be present and non-nil")
	}
}

func TestReadEnv_ListModeFiltersSensitiveNames(t *testing.T) {
	os.Setenv("ATLAS_TEST_TOKEN", "should-not-appear-as-name")
	defer os.Unsetenv("ATLAS_TEST_TOKEN")

	// Empty keys → list-all mode; result is newline-separated names.
	args := json.RawMessage(`{"keys":[]}`)
	out, err := terminalReadEnv(context.Background(), args)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "ATLAS_TEST_TOKEN") {
		t.Error("sensitive variable name should not appear in list-all output")
	}
}
