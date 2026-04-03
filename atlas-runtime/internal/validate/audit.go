package validate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const maxAuditRecords = 100

var auditMu sync.Mutex

// AppendAuditRecord appends a ValidationAuditRecord to api-validation-history.json.
// Enforces a maximum of 100 records, dropping the oldest when exceeded.
func AppendAuditRecord(supportDir string, rec AuditRecord) {
	if rec.ID == "" {
		b := make([]byte, 8)
		rand.Read(b) //nolint:errcheck
		rec.ID = hex.EncodeToString(b)
	}
	if rec.Timestamp == "" {
		rec.Timestamp = time.Now().UTC().Format(time.RFC3339)
	}

	auditMu.Lock()
	defer auditMu.Unlock()

	path := filepath.Join(supportDir, "api-validation-history.json")
	var records []AuditRecord

	if data, err := os.ReadFile(path); err == nil {
		json.Unmarshal(data, &records) //nolint:errcheck
	}

	records = append(records, rec)
	if len(records) > maxAuditRecords {
		records = records[len(records)-maxAuditRecords:]
	}

	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return
	}

	os.MkdirAll(supportDir, 0o700) //nolint:errcheck

	tmp, err := os.CreateTemp(filepath.Dir(path), "api-validation-history-*.json")
	if err != nil {
		return
	}
	tmpPath := tmp.Name()
	tmp.Write(data)  //nolint:errcheck
	tmp.Close()
	os.Rename(tmpPath, path) //nolint:errcheck
}
