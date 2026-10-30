package research

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"time"
)

const elevenLabsAPI = "https://api.elevenlabs.io"

var errVoiceCredentials = errors.New("voice credentials rejected")
var errVoiceBusy = errors.New("voice service busy")
var errVoiceUnavailable = errors.New("voice service unavailable")

type VoiceSession struct {
	SignedURL string `json:"signed_url"`
}

// The browser receives only a temporary session URL. The workspace API token,
// ElevenLabs API key, and database credentials never go to ElevenLabs clients.
func (s *Server) voiceSession(w http.ResponseWriter, r *http.Request) {
	if s.Config.ElevenLabsAPIKey == "" || s.Config.ElevenLabsAgentID == "" {
		apiError(w, http.StatusServiceUnavailable, "voice_not_configured", "Kairo voice is not configured. Check voice settings in the root .env.")
		return
	}
	client := &http.Client{Timeout: 10 * time.Second}
	session, err := requestVoiceSession(r.Context(), client, elevenLabsAPI, s.Config.ElevenLabsAPIKey, s.Config.ElevenLabsAgentID)
	if errors.Is(err, errVoiceCredentials) {
		apiError(w, http.StatusBadGateway, "voice_credentials_rejected", "ElevenLabs could not authorize this agent. Check its API key and agent access.")
		return
	}
	if errors.Is(err, errVoiceBusy) {
		apiError(w, http.StatusServiceUnavailable, "voice_busy", "Voice is busy right now. Please try again shortly.")
		return
	}
	if err != nil {
		apiError(w, http.StatusBadGateway, "voice_unavailable", "Could not start Kairo voice. Please try again.")
		return
	}
	respond(w, http.StatusOK, session)
}

func requestVoiceSession(ctx context.Context, client *http.Client, baseURL, key, agentID string) (VoiceSession, error) {
	var session VoiceSession
	endpoint := baseURL + "/v1/convai/conversation/get-signed-url?" + url.Values{"agent_id": {agentID}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return session, errVoiceUnavailable
	}
	req.Header.Set("xi-api-key", key)
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return session, errVoiceUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return session, errVoiceCredentials
	case http.StatusTooManyRequests:
		return session, errVoiceBusy
	case http.StatusOK:
	default:
		return session, errVoiceUnavailable
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&session); err != nil {
		return VoiceSession{}, errVoiceUnavailable
	}
	signed, err := url.Parse(session.SignedURL)
	if err != nil || signed.Scheme != "wss" || signed.Host != "api.elevenlabs.io" || signed.Path != "/v1/convai/conversation" || signed.User != nil || signed.RawQuery == "" || signed.Fragment != "" {
		return VoiceSession{}, errVoiceUnavailable
	}
	return session, nil
}
