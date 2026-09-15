// Package pkce provides an end-to-end OAuth 2.0 authorization-code flow with
// PKCE (Proof Key for Code Exchange) for public clients of the Shopware Admin
// API. It mirrors the flow introduced in Shopware PR #20277.
//
// Typical usage:
//
//	tokens, err := pkce.Login(ctx, pkce.Config{
//	    BaseURL: "https://shop.example.com",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	creds := shopware.NewRefreshTokenCredentials(tokens.ClientID, tokens.RefreshToken)
//	client := shopware.NewClient(shopware.Config{
//	    BaseURL:     tokens.BaseURL,
//	    Credentials: creds,
//	})
//	// Seed the access token so the first API call doesn't need a refresh.
//	_ = client.SetAccessToken(ctx, tokens.AccessToken, tokens.Expiry)
//
// The package uses only the standard library.
package pkce

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Tokens holds the result of a successful PKCE login.
type Tokens struct {
	BaseURL      string
	ClientID     string
	AccessToken  string
	RefreshToken string
	Expiry       time.Time // absolute expiry of the access token
}

// Config configures the PKCE login flow. Zero values use sensible defaults.
type Config struct {
	// BaseURL is the Shopware instance URL without a trailing "/api"
	// (e.g. "https://shop.example.com"). Required.
	BaseURL string

	// ClientID is the public OAuth client ID registered in Shopware.
	// Default: "shopware-cli".
	ClientID string

	// Scope is the OAuth scope. Default: "write".
	Scope string

	// CallbackHost is the loopback host for the callback server.
	// Default: "127.0.0.1".
	CallbackHost string

	// CallbackPort is the port for the callback server. Zero means the OS
	// assigns a random available port. Default: 0.
	CallbackPort int

	// Timeout limits how long Login waits for the user to complete
	// authorization in the browser. Default: 10 minutes.
	Timeout time.Duration

	// OpenURL, if non-nil, is called with the authorization URL to open the
	// browser. Default: uses the OS-specific open command (open, start, xdg-open).
	OpenURL func(string) error

	// HTTPClient performs the token exchange request. If nil, http.DefaultClient
	// is used. Inject your own to add custom transports or timeouts.
	HTTPClient *http.Client
}

// GenerateChallenge creates a PKCE code verifier and its S256 challenge.
// The verifier is a 43–128 character unreserved string (RFC 7636).
func GenerateChallenge() (verifier, challenge string) {
	// 32 random bytes → base64url = 43 characters
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	verifier = base64.RawURLEncoding.EncodeToString(b)

	h := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(h[:])
	return verifier, challenge
}

// GenerateState creates a random 32-byte CSRF state parameter.
func GenerateState() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// AuthorizationURL builds the Shopware authorization endpoint URL with the
// given parameters.
func AuthorizationURL(config Config, challenge, state string) string {
	u := strings.TrimRight(config.BaseURL, "/") + "/api/oauth/authorize"

	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID(config.ClientID))
	q.Set("redirect_uri", redirectURI(config))
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("scope", scope(config.Scope))
	q.Set("state", state)

	return u + "?" + q.Encode()
}

// exchangeWithClient performs the token exchange using the provided HTTP client.
func exchangeWithClient(ctx context.Context, httpClient *http.Client, baseURL, clientID, redirectURI, code, verifier string) (accessToken, refreshToken string, expiresIn int, err error) {
	payload := map[string]string{
		"grant_type":    "authorization_code",
		"client_id":     clientID,
		"code":          code,
		"redirect_uri":  redirectURI,
		"code_verifier": verifier,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", 0, fmt.Errorf("marshal token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/api/oauth/token", bytes.NewReader(body))
	if err != nil {
		return "", "", 0, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", 0, fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", 0, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return "", "", 0, fmt.Errorf("token error (status %d): %s", resp.StatusCode, respBody)
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", "", 0, fmt.Errorf("decode token response: %w", err)
	}
	return tokenResp.AccessToken, tokenResp.RefreshToken, tokenResp.ExpiresIn, nil
}

// Exchange sends the authorization code and code verifier to the token
// endpoint and returns the parsed token response. It POSTs a JSON body
// matching the core Client's fetchToken format.
func Exchange(ctx context.Context, baseURL, clientID, redirectURI, code, verifier string) (accessToken, refreshToken string, expiresIn int, err error) {
	return exchangeWithClient(ctx, http.DefaultClient, baseURL, clientID, redirectURI, code, verifier)
}

// Login performs the full PKCE authorization-code flow: generates a challenge,
// opens the browser, listens for the callback on a loopback address, and
// exchanges the code for tokens.
func Login(ctx context.Context, config Config) (*Tokens, error) {
	config = applyDefaults(config)

	// Start callback server first so we know the actual port.
	listener, err := net.Listen("tcp", net.JoinHostPort(config.CallbackHost, fmt.Sprintf("%d", config.CallbackPort)))
	if err != nil {
		return nil, fmt.Errorf("listen for callback: %w", err)
	}
	defer func() { _ = listener.Close() }()

	actualAddr := listener.Addr().(*net.TCPAddr)
	actualRedirect := fmt.Sprintf("http://%s/callback", actualAddr.String())

	verifier, challenge := GenerateChallenge()
	state := GenerateState()

	// Build auth URL with the actual redirect URI (correct port).
	authURL := strings.TrimRight(config.BaseURL, "/") + "/api/oauth/authorize"
	q := url.Values{}
	q.Set("response_type", "code")
	q.Set("client_id", clientID(config.ClientID))
	q.Set("redirect_uri", actualRedirect)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("scope", scope(config.Scope))
	q.Set("state", state)
	authURL += "?" + q.Encode()

	resultCh := make(chan loginResult, 1)
	readyCh := make(chan struct{})

	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			query := r.URL.Query()
			if errParam := query.Get("error"); errParam != "" {
				http.Error(w, "Authorization denied", http.StatusBadRequest)
				resultCh <- loginResult{err: fmt.Errorf("authorization denied: %s", errParam)}
				return
			}

			returnedState := query.Get("state")
			if returnedState != state {
				http.Error(w, "State mismatch", http.StatusBadRequest)
				resultCh <- loginResult{err: fmt.Errorf("state mismatch (CSRF?)")}
				return
			}

			code := query.Get("code")
			if code == "" {
				http.Error(w, "No code", http.StatusBadRequest)
				resultCh <- loginResult{err: fmt.Errorf("no code in callback")}
				return
			}

			_, _ = fmt.Fprintln(w, "<html><body><h1>Authorization successful</h1><p>You can close this tab.</p></body></html>")
			resultCh <- loginResult{code: code}
		}),
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	// Serve in background — signal ready when Serve starts accepting.
	go func() {
		close(readyCh)
		_ = server.Serve(listener)
	}()

	// Wait for the server to be ready before opening the browser.
	select {
	case <-readyCh:
		// Server is accepting connections
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	// Open browser
	openURL := config.OpenURL
	if openURL == nil {
		openURL = defaultOpenURL
	}
	if err := openURL(authURL); err != nil {
		// Non-fatal: print the URL instead
		fmt.Fprintf(os.Stderr, "Open this URL in your browser:\n%s\n\n", authURL)
	} else {
		fmt.Fprintln(os.Stderr, "Browser opened. Waiting for authorization...")
	}

	// Wait for result or timeout
	timeout := config.Timeout
	if timeout == 0 {
		timeout = 10 * time.Minute
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		_ = server.Close()
		return nil, ctx.Err()
	case <-timer.C:
		_ = server.Close()
		return nil, fmt.Errorf("authorization timed out after %v", timeout)
	case result := <-resultCh:
		_ = server.Close()
		if result.err != nil {
			return nil, result.err
		}

		// Exchange code for tokens
		httpClient := config.HTTPClient
		if httpClient == nil {
			httpClient = http.DefaultClient
		}
		accessToken, refreshToken, expiresIn, err := exchangeWithClient(
			ctx,
			httpClient,
			config.BaseURL,
			clientID(config.ClientID),
			actualRedirect,
			result.code,
			verifier,
		)
		if err != nil {
			return nil, fmt.Errorf("exchange code: %w", err)
		}

		return &Tokens{
			BaseURL:      config.BaseURL,
			ClientID:     clientID(config.ClientID),
			AccessToken:  accessToken,
			RefreshToken: refreshToken,
			Expiry:       time.Now().Add(time.Duration(expiresIn) * time.Second),
		}, nil
	}
}

type loginResult struct {
	code string
	err  error
}

func applyDefaults(config Config) Config {
	if config.ClientID == "" {
		config.ClientID = "shopware-cli"
	}
	if config.Scope == "" {
		config.Scope = "write"
	}
	if config.CallbackHost == "" {
		config.CallbackHost = "127.0.0.1"
	}
	if config.Timeout == 0 {
		config.Timeout = 10 * time.Minute
	}
	return config
}

func clientID(v string) string {
	if v == "" {
		return "shopware-cli"
	}
	return v
}

func scope(v string) string {
	if v == "" {
		return "write"
	}
	return v
}

func redirectURI(config Config) string {
	host := config.CallbackHost
	if host == "" {
		host = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s/callback", net.JoinHostPort(host, fmt.Sprintf("%d", config.CallbackPort)))
}

func defaultOpenURL(u string) error {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "linux":
		cmd = "xdg-open"
		args = []string{u}
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", "", strings.ReplaceAll(u, "&", "^&")}
	case "darwin":
		cmd = "open"
		args = []string{u}
	default:
		return fmt.Errorf("unsupported platform: %s", runtime.GOOS)
	}
	return exec.Command(cmd, args...).Start()
}
