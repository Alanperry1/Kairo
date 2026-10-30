package research

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVoiceSessionExchangesKeyForTemporaryURL(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/v1/convai/conversation/get-signed-url" || r.URL.Query().Get("agent_id") != "agent_test" || r.Header.Get("xi-api-key") != "private-test-key" {
			t.Error("incorrect authenticated ElevenLabs request")
		}
		fmt.Fprint(w, `{"signed_url":"wss://api.elevenlabs.io/v1/convai/conversation?token=temporary"}`)
	}))
	defer upstream.Close()
	result, err := requestVoiceSession(context.Background(), upstream.Client(), upstream.URL, "private-test-key", "agent_test")
	if err != nil || !strings.Contains(result.SignedURL, "token=temporary") {
		t.Fatalf("session exchange failed: %v", err)
	}
	if strings.Contains(result.SignedURL, "private-test-key") {
		t.Fatal("API key leaked into browser URL")
	}
}

func TestVoiceUpstreamFailuresAndInvalidURLs(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
	}{
		{"credentials", 401, `{"detail":"secret-provider-error"}`, errVoiceCredentials},
		{"busy", 429, `{}`, errVoiceBusy},
		{"upstream", 500, `{}`, errVoiceUnavailable},
		{"malformed", 200, `not-json`, errVoiceUnavailable},
		{"missing", 200, `{}`, errVoiceUnavailable},
		{"wrong_host", 200, `{"signed_url":"wss://attacker.example/v1/convai/conversation?x=1"}`, errVoiceUnavailable},
		{"wrong_scheme", 200, `{"signed_url":"https://api.elevenlabs.io/v1/convai/conversation?x=1"}`, errVoiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); fmt.Fprint(w, tc.body) }))
			defer upstream.Close()
			result, err := requestVoiceSession(context.Background(), upstream.Client(), upstream.URL, "private-test-key", "agent_test")
			if !errors.Is(err, tc.want) || result.SignedURL != "" {
				t.Fatalf("unexpected failure mapping: %v", err)
			}
		})
	}
}

func TestVoiceEndpointRequiresWorkspaceAuthentication(t *testing.T) {
	s := &Server{Config: Config{Token: "workspace-private-token"}}
	h := s.Handler()
	unauth := httptest.NewRecorder()
	h.ServeHTTP(unauth, httptest.NewRequest("POST", "/api/v1/voice/session", nil))
	if unauth.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated session returned %d", unauth.Code)
	}
	req := httptest.NewRequest("POST", "/api/v1/voice/session", nil)
	req.Header.Set("Authorization", "Bearer workspace-private-token")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "voice_not_configured") {
		t.Fatalf("missing configuration not reported: %d", w.Code)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("voice session response must not be cached")
	}
}
