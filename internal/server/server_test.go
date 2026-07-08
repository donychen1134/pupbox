package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAccessTokenDisabledByDefault(t *testing.T) {
	srv := New(Config{})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestAccessTokenProtectsAPIs(t *testing.T) {
	srv := New(Config{AccessToken: "secret-token"})

	for _, tc := range []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "health", method: http.MethodGet, path: "/api/health"},
		{name: "activities", method: http.MethodGet, path: "/api/activities"},
		{name: "chat", method: http.MethodPost, path: "/api/chat", body: `{"text":"嗯嗯"}`},
		{name: "speech", method: http.MethodPost, path: "/api/speech", body: `{"text":"汪。"}`},
		{name: "voice", method: http.MethodPost, path: "/api/voice"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			srv.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusUnauthorized, rec.Body.String())
			}
		})
	}
}

func TestAccessTokenAcceptsBearerHeader(t *testing.T) {
	srv := New(Config{AccessToken: "secret-token"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/chat?tts=off", strings.NewReader(`{"text":"豆豆讲故事"}`))
	req.Header.Set("Authorization", "Bearer secret-token")
	req.Header.Set("Content-Type", "application/json")
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response chatResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Source != "activity:story" {
		t.Fatalf("source = %q, want activity:story", response.Source)
	}
}

func TestAccessTokenAcceptsQueryToken(t *testing.T) {
	srv := New(Config{AccessToken: "secret-token"})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/health?token=secret-token", nil)
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response["auth_required"] != true {
		t.Fatalf("auth_required = %v, want true", response["auth_required"])
	}
}
