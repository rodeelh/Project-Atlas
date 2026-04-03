package skills

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"atlas-runtime-go/internal/creds"
	"github.com/pquerna/otp/totp"
)

func (r *Registry) registerVault() {
	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "vault.store",
			Description: "Store a new credential in the Atlas secure vault. Use this whenever the agent creates an account, generates a password, receives an API key, or discovers any secret that should be saved for future use.",
			Properties: map[string]ToolParam{
				"service":     {Description: "Hostname or service name, e.g. gmail.com or github.com", Type: "string"},
				"label":       {Description: "Human-readable name for this credential, e.g. 'Gmail – Atlas Agent Account'", Type: "string"},
				"username":    {Description: "Username, email address, or account ID", Type: "string"},
				"password":    {Description: "Password, token, or secret value", Type: "string"},
				"totp_secret": {Description: "Base32-encoded TOTP seed for 2FA (the manual setup key shown when enabling 2FA on a site) — optional", Type: "string"},
				"notes":       {Description: "Free-form context, e.g. why this account was created or what it is used for — optional", Type: "string"},
			},
			Required: []string{"service", "label", "username", "password"},
		},
		PermLevel: "execute",
		Fn:        vaultStore,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "vault.lookup",
			Description: "Look up stored credentials for a service. Returns username, password, and TOTP secret if present. Use before attempting to log in to a site to retrieve saved credentials.",
			Properties: map[string]ToolParam{
				"service": {Description: "Hostname or service name to search for, e.g. gmail.com", Type: "string"},
			},
			Required: []string{"service"},
		},
		PermLevel: "read",
		Fn:        vaultLookup,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "vault.list",
			Description: "List all stored vault entries. Passwords and TOTP secrets are omitted from the output for safety — use vault.lookup to retrieve secrets for a specific service.",
			Properties:  map[string]ToolParam{},
			Required:    []string{},
		},
		PermLevel: "read",
		Fn:        vaultList,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "vault.update",
			Description: "Update an existing vault entry by ID. Only fields with non-empty values are changed — omit a field to leave it unchanged.",
			Properties: map[string]ToolParam{
				"id":          {Description: "Entry ID to update (from vault.list or returned by vault.store)", Type: "string"},
				"label":       {Description: "New label — omit to keep existing", Type: "string"},
				"username":    {Description: "New username — omit to keep existing", Type: "string"},
				"password":    {Description: "New password — omit to keep existing", Type: "string"},
				"totp_secret": {Description: "New TOTP base32 seed — omit to keep existing", Type: "string"},
				"notes":       {Description: "New notes — omit to keep existing", Type: "string"},
			},
			Required: []string{"id"},
		},
		PermLevel: "execute",
		Fn:        vaultUpdate,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "vault.delete",
			Description: "Permanently delete a vault entry by ID. This cannot be undone.",
			Properties: map[string]ToolParam{
				"id": {Description: "Entry ID to delete (from vault.list)", Type: "string"},
			},
			Required: []string{"id"},
		},
		PermLevel: "execute",
		Fn:        vaultDelete,
	})

	r.register(SkillEntry{
		Def: ToolDef{
			Name:        "vault.totp_generate",
			Description: "Generate the current TOTP 2FA code for a vault entry that has a TOTP secret configured. The code is time-based and valid for the number of seconds shown in the response.",
			Properties: map[string]ToolParam{
				"service": {Description: "Hostname or service name to generate the code for, e.g. github.com", Type: "string"},
				"id":      {Description: "Specific vault entry ID — optional, defaults to the first matching entry for the given service", Type: "string"},
			},
			Required: []string{"service"},
		},
		PermLevel: "read",
		Fn:        vaultTOTPGenerate,
	})
}

// ── skill functions ───────────────────────────────────────────────────────────

func vaultStore(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Service    string `json:"service"`
		Label      string `json:"label"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		TOTPSecret string `json:"totp_secret"`
		Notes      string `json:"notes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("vault.store: invalid args: %w", err)
	}
	if p.Service == "" || p.Label == "" || p.Username == "" || p.Password == "" {
		return "", fmt.Errorf("vault.store: service, label, username, and password are required")
	}

	entry := creds.VaultEntry{
		Service:    p.Service,
		Label:      p.Label,
		Username:   p.Username,
		Password:   p.Password,
		TOTPSecret: p.TOTPSecret,
		Notes:      p.Notes,
	}

	id, err := creds.VaultStore(entry)
	if err != nil {
		return "", fmt.Errorf("vault.store: %w", err)
	}

	extra := ""
	if p.TOTPSecret != "" {
		extra = " (TOTP 2FA configured)"
	}
	return fmt.Sprintf("Stored credential for %s in vault. Entry ID: %s%s", p.Service, id, extra), nil
}

func vaultLookup(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Service string `json:"service"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("vault.lookup: invalid args: %w", err)
	}
	if p.Service == "" {
		return "", fmt.Errorf("vault.lookup: service is required")
	}

	entries, err := creds.VaultRead()
	if err != nil {
		return "", fmt.Errorf("vault.lookup: %w", err)
	}

	service := strings.ToLower(strings.TrimSpace(p.Service))
	var matches []creds.VaultEntry
	for _, e := range entries {
		svc := strings.ToLower(e.Service)
		if svc == service || strings.Contains(svc, service) || strings.Contains(service, svc) {
			matches = append(matches, e)
		}
	}

	if len(matches) == 0 {
		return fmt.Sprintf("No credentials found for %q in vault.", p.Service), nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Found %d credential(s) for %q:\n\n", len(matches), p.Service)
	for _, m := range matches {
		fmt.Fprintf(&sb, "ID: %s\nService: %s\nLabel: %s\nUsername: %s\nPassword: %s\n",
			m.ID, m.Service, m.Label, m.Username, m.Password)
		if m.TOTPSecret != "" {
			fmt.Fprintf(&sb, "TOTP secret: %s\n", m.TOTPSecret)
		}
		if m.Notes != "" {
			fmt.Fprintf(&sb, "Notes: %s\n", m.Notes)
		}
		fmt.Fprintln(&sb)
	}
	return strings.TrimSpace(sb.String()), nil
}

func vaultList(_ context.Context, _ json.RawMessage) (string, error) {
	entries, err := creds.VaultRead()
	if err != nil {
		return "", fmt.Errorf("vault.list: %w", err)
	}
	if len(entries) == 0 {
		return "Vault is empty. No credentials stored.", nil
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%d credential(s) in vault:\n\n", len(entries))
	for _, e := range entries {
		totp2fa := ""
		if e.TOTPSecret != "" {
			totp2fa = " [2FA]"
		}
		fmt.Fprintf(&sb, "ID: %s\nService: %s\nLabel: %s\nUsername: %s%s\nCreated: %s\n\n",
			e.ID, e.Service, e.Label, e.Username, totp2fa, e.CreatedAt)
	}
	return strings.TrimSpace(sb.String()), nil
}

func vaultUpdate(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID         string `json:"id"`
		Label      string `json:"label"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		TOTPSecret string `json:"totp_secret"`
		Notes      string `json:"notes"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("vault.update: invalid args: %w", err)
	}
	if p.ID == "" {
		return "", fmt.Errorf("vault.update: id is required")
	}

	if err := creds.VaultUpdate(p.ID, creds.VaultEntry{
		Label:      p.Label,
		Username:   p.Username,
		Password:   p.Password,
		TOTPSecret: p.TOTPSecret,
		Notes:      p.Notes,
	}); err != nil {
		return "", fmt.Errorf("vault.update: %w", err)
	}
	return fmt.Sprintf("Vault entry %s updated.", p.ID), nil
}

func vaultDelete(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("vault.delete: invalid args: %w", err)
	}
	if p.ID == "" {
		return "", fmt.Errorf("vault.delete: id is required")
	}
	if err := creds.VaultDelete(p.ID); err != nil {
		return "", fmt.Errorf("vault.delete: %w", err)
	}
	return fmt.Sprintf("Vault entry %s permanently deleted.", p.ID), nil
}

func vaultTOTPGenerate(_ context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Service string `json:"service"`
		ID      string `json:"id"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("vault.totp_generate: invalid args: %w", err)
	}
	if p.Service == "" {
		return "", fmt.Errorf("vault.totp_generate: service is required")
	}

	entries, err := creds.VaultRead()
	if err != nil {
		return "", fmt.Errorf("vault.totp_generate: %w", err)
	}

	service := strings.ToLower(strings.TrimSpace(p.Service))
	var match *creds.VaultEntry
	for i := range entries {
		e := &entries[i]
		// Specific ID takes priority.
		if p.ID != "" && e.ID == p.ID {
			match = e
			break
		}
		// Otherwise match by service, require TOTP configured.
		if p.ID == "" && e.TOTPSecret != "" {
			svc := strings.ToLower(e.Service)
			if svc == service || strings.Contains(svc, service) || strings.Contains(service, svc) {
				match = e
				break
			}
		}
	}

	if match == nil {
		return fmt.Sprintf("No vault entry with a TOTP secret found for %q. Use vault.update to add a TOTP secret to an existing entry, or vault.store to create a new one.", p.Service), nil
	}
	if match.TOTPSecret == "" {
		return fmt.Sprintf("Vault entry %q (%s) does not have a TOTP secret configured.", match.Label, match.ID), nil
	}

	now := time.Now()
	code, err := totp.GenerateCode(strings.ToUpper(strings.TrimSpace(match.TOTPSecret)), now)
	if err != nil {
		return "", fmt.Errorf("vault.totp_generate: invalid TOTP secret for %q: %w", match.Service, err)
	}

	secondsRemaining := 30 - (now.Unix() % 30)
	return fmt.Sprintf("TOTP code for %s (%s): %s — valid for %d more second(s)",
		match.Service, match.Label, code, secondsRemaining), nil
}
