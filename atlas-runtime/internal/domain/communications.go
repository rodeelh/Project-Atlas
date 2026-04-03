package domain

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"atlas-runtime-go/internal/comms"
)

// CommunicationsDomain handles communications platform management routes natively.
//
// Routes owned:
//
//	GET  /communications
//	GET  /communications/channels
//	GET  /communications/platforms/:platform/setup
//	PUT  /communications/platforms/:platform
//	POST /communications/platforms/:platform/validate
//	GET  /telegram/chats
type CommunicationsDomain struct {
	svc *comms.Service
}

// NewCommunicationsDomain creates the CommunicationsDomain.
func NewCommunicationsDomain(svc *comms.Service) *CommunicationsDomain {
	return &CommunicationsDomain{svc: svc}
}

// Register mounts communications routes on the given router.
func (d *CommunicationsDomain) Register(r chi.Router) {
	r.Get("/communications", d.getSnapshot)
	r.Get("/communications/channels", d.getChannels)
	r.Get("/communications/platforms/{platform}/setup", d.getSetupValues)
	r.Put("/communications/platforms/{platform}", d.updatePlatform)
	r.Post("/communications/platforms/{platform}/validate", d.validatePlatform)
	r.Get("/telegram/chats", d.getTelegramChats)
}

func (d *CommunicationsDomain) getSnapshot(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.svc.Snapshot())
}

func (d *CommunicationsDomain) getChannels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.svc.Channels())
}

func (d *CommunicationsDomain) getTelegramChats(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, d.svc.TelegramSessions())
}

func (d *CommunicationsDomain) getSetupValues(w http.ResponseWriter, r *http.Request) {
	platform := chi.URLParam(r, "platform")
	values := d.svc.SetupValues(platform)
	writeJSON(w, http.StatusOK, map[string]any{"values": values})
}

func (d *CommunicationsDomain) updatePlatform(w http.ResponseWriter, r *http.Request) {
	platform := chi.URLParam(r, "platform")

	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &body) {
		return
	}

	status, err := d.svc.UpdatePlatform(platform, body.Enabled)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func (d *CommunicationsDomain) validatePlatform(w http.ResponseWriter, r *http.Request) {
	platform := chi.URLParam(r, "platform")

	var body struct {
		Credentials map[string]string `json:"credentials"`
		Config      *struct {
			DiscordClientID string `json:"discordClientID"`
		} `json:"config"`
	}
	// Body is optional — empty body means validate with Keychain credentials.
	if r.ContentLength > 0 {
		if !decodeJSON(w, r, &body) {
			return
		}
	}

	creds := body.Credentials
	if creds == nil {
		creds = map[string]string{}
	}
	discordClientID := ""
	if body.Config != nil {
		discordClientID = body.Config.DiscordClientID
	}

	status, err := d.svc.ValidatePlatform(platform, creds, discordClientID)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

// Ensure CommunicationsDomain implements Handler.
var _ Handler = (*CommunicationsDomain)(nil)
