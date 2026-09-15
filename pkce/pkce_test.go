package pkce

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestGenerateChallenge(t *testing.T) {
	verifier, challenge := GenerateChallenge()

	if len(verifier) < 43 || len(verifier) > 128 {
		t.Fatalf("verifier length %d not in [43, 128]", len(verifier))
	}
	if len(challenge) != 43 {
		t.Fatalf("challenge length %d, want 43", len(challenge))
	}

	// Verify base64url encoding is valid
	if _, err := base64.RawURLEncoding.DecodeString(verifier); err != nil {
		t.Errorf("verifier not valid base64url: %v", err)
	}
	if _, err := base64.RawURLEncoding.DecodeString(challenge); err != nil {
		t.Errorf("challenge not valid base64url: %v", err)
	}
}

func TestGenerateState(t *testing.T) {
	state := GenerateState()
	if len(state) != 43 {
		t.Fatalf("state length %d, want 43", len(state))
	}
	if _, err := base64.RawURLEncoding.DecodeString(state); err != nil {
		t.Errorf("state not valid base64url: %v", err)
	}
}

func TestAuthorizationURL(t *testing.T) {
	_, challenge := GenerateChallenge()
	state := GenerateState()

	config := Config{
		BaseURL:      "https://shop.example.com",
		ClientID:     "my-cli",
		Scope:        "write",
		CallbackHost: "127.0.0.1",
		CallbackPort: 8080,
	}

	authURL := AuthorizationURL(config, challenge, state)

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Path != "/api/oauth/authorize" {
		t.Errorf("path = %s, want /api/oauth/authorize", parsed.Path)
	}

	q := parsed.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %s, want code", q.Get("response_type"))
	}
	if q.Get("client_id") != "my-cli" {
		t.Errorf("client_id = %s, want my-cli", q.Get("client_id"))
	}
	if q.Get("code_challenge") != challenge {
		t.Errorf("code_challenge mismatch")
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %s, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("scope") != "write" {
		t.Errorf("scope = %s, want write", q.Get("scope"))
	}
	if q.Get("state") != state {
		t.Errorf("state mismatch")
	}
	expectedRedirect := "http://127.0.0.1:8080/callback"
	if q.Get("redirect_uri") != expectedRedirect {
		t.Errorf("redirect_uri = %s, want %s", q.Get("redirect_uri"), expectedRedirect)
	}
}

func TestAuthorizationURLDefaults(t *testing.T) {
	_, challenge := GenerateChallenge()
	state := GenerateState()

	config := Config{
		BaseURL: "https://shop.example.com",
		// ClientID, Scope left empty — should use defaults
	}

	authURL := AuthorizationURL(config, challenge, state)

	parsed, err := url.Parse(authURL)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()

	if q.Get("client_id") != "shopware-cli" {
		t.Errorf("client_id = %s, want shopware-cli (default)", q.Get("client_id"))
	}
	if q.Get("scope") != "write" {
		t.Errorf("scope = %s, want write (default)", q.Get("scope"))
	}
}

func TestExchangeRequestShape(t *testing.T) {
	var captured map[string]string
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/oauth/token" {
			// JSON body
			body, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(body, &captured)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
			return
		}
		http.Error(w, "not found", 404)
	}))
	defer tokenSrv.Close()

	_, _, _, err := Exchange(
		context.Background(),
		tokenSrv.URL,
		"shopware-cli",
		"http://127.0.0.1:9999/callback",
		"auth-code-123",
		"verifier-456",
	)
	if err != nil {
		t.Fatal(err)
	}

	if captured["grant_type"] != "authorization_code" {
		t.Errorf("grant_type = %s, want authorization_code", captured["grant_type"])
	}
	if captured["client_id"] != "shopware-cli" {
		t.Errorf("client_id = %s, want shopware-cli", captured["client_id"])
	}
	if captured["code"] != "auth-code-123" {
		t.Errorf("code = %s, want auth-code-123", captured["code"])
	}
	if captured["code_verifier"] != "verifier-456" {
		t.Errorf("code_verifier = %s, want verifier-456", captured["code_verifier"])
	}
	if captured["redirect_uri"] != "http://127.0.0.1:9999/callback" {
		t.Errorf("redirect_uri = %s, want http://127.0.0.1:9999/callback", captured["redirect_uri"])
	}
}

func TestExchangeResponse(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"tok","refresh_token":"ref","expires_in":1800}`))
	}))
	defer tokenSrv.Close()

	at, rt, exp, err := Exchange(
		context.Background(),
		tokenSrv.URL,
		"shopware-cli",
		"http://127.0.0.1:0/callback",
		"code",
		"verifier",
	)
	if err != nil {
		t.Fatal(err)
	}
	if at != "tok" {
		t.Errorf("access_token = %s, want tok", at)
	}
	if rt != "ref" {
		t.Errorf("refresh_token = %s, want ref", rt)
	}
	if exp != 1800 {
		t.Errorf("expires_in = %d, want 1800", exp)
	}
}

func TestExchangeError(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid grant", 400)
	}))
	defer tokenSrv.Close()

	_, _, _, err := Exchange(
		context.Background(),
		tokenSrv.URL,
		"shopware-cli",
		"http://127.0.0.1:0/callback",
		"code",
		"verifier",
	)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestLoginCallbackSuccess(t *testing.T) {
	var exchangedCode string
	var exchangedVerifier string

	// Mock Shopware server: authorize endpoint + token endpoint
	shopSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/oauth/authorize":
			// Just accept the request
			w.WriteHeader(http.StatusOK)
		case "/api/oauth/token":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			exchangedCode = body["code"]
			exchangedVerifier = body["code_verifier"]
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"at","refresh_token":"rt","expires_in":3600}`))
		default:
			http.Error(w, "not found", 404)
		}
	}))
	defer shopSrv.Close()

	// Start Login in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tokensCh := make(chan *Tokens, 1)
	errCh := make(chan error, 1)

	go func() {
		tokens, err := Login(ctx, Config{
			BaseURL:      shopSrv.URL,
			ClientID:     "shopware-cli",
			CallbackHost: "127.0.0.1",
			CallbackPort: 0, // auto port
			Timeout:      5 * time.Second,
			OpenURL: func(u string) error {
				// Simulate browser redirect: parse auth URL, then hit callback
				parsed, _ := url.Parse(u)
				q := parsed.Query()

				// Simulate successful authorization — hit the callback
				callbackURL := strings.Replace(u, "/api/oauth/authorize?", "/callback?", 1)
				cb, _ := url.Parse(callbackURL)
				cbQuery := cb.Query()
				cbQuery.Set("code", "simulated-code")
				cbQuery.Set("state", q.Get("state"))
				// Rebuild with the actual callback host/port from the redirect_uri
				redirectURI := q.Get("redirect_uri")
				resp, err := http.Get(redirectURI + "?code=" + url.QueryEscape("simulated-code") + "&state=" + url.QueryEscape(q.Get("state")))
				if err != nil {
					return err
				}
				_ = resp.Body.Close()
				return nil
			},
		})
		if err != nil {
			errCh <- err
		} else {
			tokensCh <- tokens
		}
	}()

	select {
	case err := <-errCh:
		t.Fatalf("Login failed: %v", err)
	case tokens := <-tokensCh:
		if tokens.AccessToken != "at" {
			t.Errorf("AccessToken = %s, want at", tokens.AccessToken)
		}
		if tokens.RefreshToken != "rt" {
			t.Errorf("RefreshToken = %s, want rt", tokens.RefreshToken)
		}
		if tokens.ClientID != "shopware-cli" {
			t.Errorf("ClientID = %s, want shopware-cli", tokens.ClientID)
		}
		if exchangedCode != "simulated-code" {
			t.Errorf("exchanged code = %s, want simulated-code", exchangedCode)
		}
		if exchangedVerifier == "" {
			t.Errorf("exchanged verifier is empty")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("Login timed out")
	}
}

func TestLoginCallbackError(t *testing.T) {
	shopSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer shopSrv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := Login(ctx, Config{
		BaseURL:      shopSrv.URL,
		ClientID:     "shopware-cli",
		CallbackHost: "127.0.0.1",
		CallbackPort: 0,
		Timeout:      2 * time.Second,
		OpenURL: func(u string) error {
			// Simulate access_denied
			parsed, _ := url.Parse(u)
			q := parsed.Query()
			redirectURI := q.Get("redirect_uri")
			// Parse redirect_uri to get host/port
			redirectParsed, _ := url.Parse(redirectURI)
			resp, err := http.Get("http://" + redirectParsed.Host + "/callback?error=access_denied&state=" + url.QueryEscape(q.Get("state")))
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected error on access_denied, got nil")
	}
	if !strings.Contains(err.Error(), "authorization denied") {
		t.Errorf("error = %v, want 'authorization denied'", err)
	}
}

func TestLoginStateMismatch(t *testing.T) {
	shopSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer shopSrv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	_, err := Login(ctx, Config{
		BaseURL:      shopSrv.URL,
		ClientID:     "shopware-cli",
		CallbackHost: "127.0.0.1",
		CallbackPort: 0,
		Timeout:      2 * time.Second,
		OpenURL: func(u string) error {
			parsed, _ := url.Parse(u)
			q := parsed.Query()
			redirectURI := q.Get("redirect_uri")
			redirectParsed, _ := url.Parse(redirectURI)
			// Send wrong state
			resp, err := http.Get("http://" + redirectParsed.Host + "/callback?code=abc&state=wrong-state")
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			return nil
		},
	})
	if err == nil {
		t.Fatal("expected state mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "state mismatch") {
		t.Errorf("error = %v, want 'state mismatch'", err)
	}
}

func TestLoginTimeout(t *testing.T) {
	shopSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer shopSrv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	_, err := Login(ctx, Config{
		BaseURL:      shopSrv.URL,
		ClientID:     "shopware-cli",
		CallbackHost: "127.0.0.1",
		CallbackPort: 0,
		Timeout:      500 * time.Millisecond,
		OpenURL:      func(string) error { return nil }, // Don't simulate callback
	})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want 'timed out'", err)
	}
	if elapsed < 400*time.Millisecond {
		t.Errorf("returned too quickly: %v", elapsed)
	}
}

func TestLoginContextCancellation(t *testing.T) {
	shopSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer shopSrv.Close()

	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := Login(ctx, Config{
		BaseURL:      shopSrv.URL,
		ClientID:     "shopware-cli",
		CallbackHost: "127.0.0.1",
		CallbackPort: 0,
		Timeout:      10 * time.Second,
		OpenURL:      func(string) error { return nil },
	})
	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

func TestTokensExpiry(t *testing.T) {
	now := time.Now()
	tokens := &Tokens{
		AccessToken:  "at",
		RefreshToken: "rt",
		ClientID:     "shopware-cli",
		BaseURL:      "https://shop.example.com",
		Expiry:       now.Add(3600 * time.Second),
	}
	if tokens.Expiry.Before(now) || tokens.Expiry.After(now.Add(3601*time.Second)) {
		t.Errorf("Expiry not ~1h from now: %v", tokens.Expiry)
	}
}

func TestApplyDefaults(t *testing.T) {
	config := applyDefaults(Config{})
	if config.ClientID != "shopware-cli" {
		t.Errorf("ClientID = %s, want shopware-cli", config.ClientID)
	}
	if config.Scope != "write" {
		t.Errorf("Scope = %s, want write", config.Scope)
	}
	if config.CallbackHost != "127.0.0.1" {
		t.Errorf("CallbackHost = %s, want 127.0.0.1", config.CallbackHost)
	}
	if config.Timeout != 10*time.Minute {
		t.Errorf("Timeout = %v, want 10m", config.Timeout)
	}
}

func TestExchangeJSONResponse(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"access_token":  "json-at",
			"refresh_token": "json-rt",
			"expires_in":    7200,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer tokenSrv.Close()

	at, rt, exp, err := Exchange(
		context.Background(),
		tokenSrv.URL,
		"shopware-cli",
		"http://127.0.0.1:0/callback",
		"code",
		"verifier",
	)
	if err != nil {
		t.Fatal(err)
	}
	if at != "json-at" {
		t.Errorf("access_token = %s, want json-at", at)
	}
	if rt != "json-rt" {
		t.Errorf("refresh_token = %s, want json-rt", rt)
	}
	if exp != 7200 {
		t.Errorf("expires_in = %d, want 7200", exp)
	}
}

func TestRedirectURI(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name:     "default host",
			config:   Config{CallbackPort: 8080},
			expected: "http://127.0.0.1:8080/callback",
		},
		{
			name:     "custom IPv4",
			config:   Config{CallbackHost: "192.168.1.1", CallbackPort: 3000},
			expected: "http://192.168.1.1:3000/callback",
		},
		{
			name:     "IPv6 localhost",
			config:   Config{CallbackHost: "::1", CallbackPort: 8080},
			expected: "http://[::1]:8080/callback",
		},
		{
			name:     "IPv6 full address",
			config:   Config{CallbackHost: "2001:db8::1", CallbackPort: 9000},
			expected: "http://[2001:db8::1]:9000/callback",
		},
		{
			name:     "hostname",
			config:   Config{CallbackHost: "localhost", CallbackPort: 8080},
			expected: "http://localhost:8080/callback",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := redirectURI(tt.config)
			if got != tt.expected {
				t.Errorf("redirectURI() = %q, want %q", got, tt.expected)
			}
		})
	}
}
