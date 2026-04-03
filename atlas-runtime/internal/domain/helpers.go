package domain

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// writeJSON encodes v as JSON and writes it with the given HTTP status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	data, err := json.Marshal(v)
	if err != nil {
		http.Error(w, `{"error":"internal encoding error"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	w.Write(data)
}

// writeError writes a JSON {"error":"..."} response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}

// writeNotImplemented is the standard stub response for runtime features that
// are intentionally deferred.
func writeNotImplemented(w http.ResponseWriter, r *http.Request) {
	writeError(w, http.StatusNotImplemented,
		"This feature is not yet implemented in the Atlas runtime.")
}

// nowRFC3339 returns the current UTC time as an RFC3339 string.
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// execSecurityInDomain runs the macOS `security` CLI tool.
// Returns (stdout, exitCode, error).
func execSecurityInDomain(args ...string) (string, error) {
	cmd := exec.Command("security", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("security %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

// keychainItemExists returns true if the Keychain item exists (exit 0),
// false if not found (exit 44), and an error for any other failure.
func keychainItemExists(service, account string) (bool, error) {
	cmd := exec.Command("security", "find-generic-password",
		"-s", service, "-a", account)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		if exitErr.ExitCode() == 44 {
			return false, nil // item not found — normal first-run case
		}
	}
	return false, err
}

// newDomainUUID generates a random UUID v4.
func newDomainUUID() string {
	b := make([]byte, 16)
	rand.Read(b)
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// decodeJSON decodes the request body into v. Returns false and writes an
// error response on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body: "+err.Error())
		return false
	}
	return true
}

// decodeJSONLenient attempts to decode the request body into v, silently
// ignoring decode failures (e.g. empty body on DELETE requests).
func decodeJSONLenient(r *http.Request, v any) {
	json.NewDecoder(r.Body).Decode(v) //nolint:errcheck
}

// atomicWriteFile writes data to path using a temp-file + rename so that a
// crash mid-write never leaves a partial file. Follows the same convention as
// forge/store.go and config/store.go.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	tmp.Close()
	return os.Rename(tmpPath, path)
}
