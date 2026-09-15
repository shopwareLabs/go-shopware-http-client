// Package shopware provides a standalone HTTP client and Data Abstraction Layer
// (DAL) helpers for talking to the Shopware Admin API.
//
// The package is self-contained: it depends only on the standard library and
// golang.org/x/sync/singleflight, so it can be vendored or extracted into its
// own module without dragging application-specific code along. Anything
// application-specific (custom HTTP transports, extra request headers, SSRF
// protection) is injected via Config rather than imported.
//
// The high-level entry points are:
//
//   - Client       — authenticated HTTP client with OAuth token caching.
//   - Criteria      — fluent builder for DAL search/aggregation payloads.
//   - EntityRepository — typed CRUD/search over a single entity.
package shopware

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	// DefaultTimeout is applied to the internal http.Client when the caller
	// does not provide one.
	DefaultTimeout = 30 * time.Second

	// tokenExpiryMargin is subtracted from the OAuth token lifetime so a token
	// is treated as expired slightly before the server would reject it,
	// avoiding 401s from a token that lapses mid-request.
	tokenExpiryMargin = 30 * time.Second

	// minErrorStatusCode is the first HTTP status code treated as an error.
	minErrorStatusCode = 400
)

// Config configures a Client. Only BaseURL and the OAuth credentials are
// required; everything else has sensible defaults.
type Config struct {
	// BaseURL is the shop URL without a trailing "/api" (e.g.
	// "https://shop.example.com"). A trailing slash is trimmed.
	BaseURL string

	// Credentials selects the OAuth grant used to authenticate. Use
	// NewIntegrationCredentials (client_credentials) or NewPasswordCredentials
	// (admin user login), or supply a custom Credentials implementation.
	//
	// If nil, ClientID/ClientSecret are used as IntegrationCredentials.
	Credentials Credentials

	// ClientID and ClientSecret are a shorthand for integration credentials and
	// are only consulted when Credentials is nil.
	ClientID     string
	ClientSecret string

	// HTTPClient performs the actual requests. If nil, a default client with
	// DefaultTimeout is used. Inject your own to add instrumentation, SSRF
	// protection, or custom transports.
	HTTPClient *http.Client

	// Headers are attached to every request (token requests included). Use this
	// for deployment-specific headers such as a reverse-proxy auth token.
	Headers map[string]string

	// TokenStorage caches OAuth tokens. If nil, a process-local
	// InMemoryTokenStorage is used, caching the token for the lifetime of the
	// Client. Provide a distributed store (Redis, ...) to share tokens across
	// instances, or NewNoOpTokenStorage() to disable caching.
	TokenStorage TokenStorage

	// TokenStorageKey is the key under which this Client's token is stored. If
	// empty, a key derived from the credentials' identity is used. Set this when
	// several Clients share a TokenStorage but must not share tokens (e.g. the
	// same principal against different shops).
	TokenStorageKey string
}

// Client is an authenticated HTTP client for the Shopware Admin API. It caches
// the OAuth token, collapses concurrent token fetches into a single request,
// and transparently retries once on a 401 with a fresh token.
//
// A Client is safe for concurrent use.
type Client struct {
	baseURL     string
	credentials Credentials
	headers     map[string]string
	httpClient  *http.Client

	tokenStorage TokenStorage
	tokenKey     string

	// tokenGroup collapses concurrent token fetches (cold cache or post-401
	// re-auth) into a single in-flight request that all callers share.
	tokenGroup singleflight.Group

	// versionGroup collapses concurrent /_info/config fetches; version caches
	// the resolved Shopware version string.
	versionGroup singleflight.Group
	versionMu    sync.Mutex
	version      string
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// Response represents a response from the Shopware API.
type Response struct {
	StatusCode int
	Body       []byte
	Headers    http.Header
}

// JSON unmarshals the response body into target.
func (r *Response) JSON(target any) error {
	return json.Unmarshal(r.Body, target)
}

// APIError describes a non-2xx response from the API. When the body matches the
// Shopware errors envelope, Detail holds the first error's detail message.
type APIError struct {
	StatusCode int
	Body       string
	Detail     string
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("shopware api error (status %d): %s", e.StatusCode, e.Detail)
	}
	return fmt.Sprintf("shopware api error (status %d): %s", e.StatusCode, e.Body)
}

// NewClient creates a new Client from the given Config.
func NewClient(config Config) *Client {
	httpClient := config.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: DefaultTimeout}
	}

	storage := config.TokenStorage
	if storage == nil {
		storage = NewInMemoryTokenStorage()
	}

	credentials := config.Credentials
	if credentials == nil {
		credentials = NewIntegrationCredentials(config.ClientID, config.ClientSecret)
	}

	tokenKey := config.TokenStorageKey
	if tokenKey == "" {
		tokenKey = credentials.identity()
	}

	return &Client{
		baseURL:      strings.TrimRight(config.BaseURL, "/"),
		credentials:  credentials,
		headers:      config.Headers,
		httpClient:   httpClient,
		tokenStorage: storage,
		tokenKey:     tokenKey,
	}
}

// Authenticate verifies the credentials by fetching an access token. It returns
// nil on success. The context bounds the token request.
func (c *Client) Authenticate(ctx context.Context) error {
	_, err := c.getToken(ctx)
	return err
}

// Version returns the shop's Shopware version (e.g. "6.7.0.0"), fetching it
// from /api/_info/config once and caching the result. Concurrent callers share
// a single fetch. Endpoint helpers use it to gate version-specific behavior.
func (c *Client) Version(ctx context.Context) (string, error) {
	c.versionMu.Lock()
	cached := c.version
	c.versionMu.Unlock()
	if cached != "" {
		return cached, nil
	}

	v, err, _ := c.versionGroup.Do("version", func() (any, error) {
		resp, err := c.Get(ctx, "/_info/config")
		if err != nil {
			return "", err
		}
		var info struct {
			Version string `json:"version"`
		}
		if err := resp.JSON(&info); err != nil {
			return "", fmt.Errorf("decode _info/config: %w", err)
		}
		c.versionMu.Lock()
		c.version = info.Version
		c.versionMu.Unlock()
		return info.Version, nil
	})
	if err != nil {
		return "", err
	}
	return v.(string), nil
}

// SetAccessToken seeds the token cache with a pre-obtained access token and
// its expiry. Use it after an interactive PKCE login (via the pkce
// sub-package) to avoid an immediate refresh_token round-trip.
func (c *Client) SetAccessToken(ctx context.Context, token string, expiry time.Time) error {
	return c.tokenStorage.Set(ctx, c.tokenKey, token, expiry)
}

// cachedAccessToken returns a still-valid cached token, or "" if none. A token
// is treated as expired once it falls within tokenExpiryMargin of its real
// expiry, so it is never used in the window where the server might already
// reject it.
func (c *Client) cachedAccessToken(ctx context.Context) (string, error) {
	token, expiry, err := c.tokenStorage.Get(ctx, c.tokenKey)
	if err != nil {
		return "", fmt.Errorf("get token from storage: %w", err)
	}
	if token != "" && time.Now().Add(tokenExpiryMargin).Before(expiry) {
		return token, nil
	}
	return "", nil
}

// invalidateToken drops the cached token so the next request re-authenticates.
func (c *Client) invalidateToken(ctx context.Context) {
	_ = c.tokenStorage.Delete(ctx, c.tokenKey)
}

func (c *Client) getToken(ctx context.Context) (string, error) {
	if token, err := c.cachedAccessToken(ctx); err != nil {
		return "", err
	} else if token != "" {
		return token, nil
	}

	// Collapse concurrent fetches: only one goroutine performs the network
	// round-trip; the rest wait and share its result.
	token, err, _ := c.tokenGroup.Do("token", func() (any, error) {
		// Another goroutine may have populated the cache while we waited.
		if token, err := c.cachedAccessToken(ctx); err != nil {
			return "", err
		} else if token != "" {
			return token, nil
		}
		return c.fetchToken(ctx)
	})
	if err != nil {
		return "", err
	}
	return token.(string), nil
}

func (c *Client) fetchToken(ctx context.Context) (string, error) {
	body, err := json.Marshal(c.credentials.tokenRequest())
	if err != nil {
		return "", fmt.Errorf("marshal token request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/oauth/token", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	c.applyHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode >= minErrorStatusCode {
		return "", newAPIError(resp.StatusCode, respBody)
	}

	var tokenResp tokenResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}

	// Store the real expiry; the safety margin is applied when reading the
	// token back, so the storage is not coupled to this Client's margin.
	expiry := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	if err := c.tokenStorage.Set(ctx, c.tokenKey, tokenResp.AccessToken, expiry); err != nil {
		return "", fmt.Errorf("store token: %w", err)
	}

	// If the server returned a rotated refresh_token and our credentials
	// support it, persist the new value so subsequent refreshes work.
	if tokenResp.RefreshToken != "" {
		if updater, ok := c.credentials.(refreshTokenUpdater); ok {
			updater.setRefreshToken(tokenResp.RefreshToken)
		}
	}

	return tokenResp.AccessToken, nil
}

func (c *Client) applyHeaders(req *http.Request) {
	for k, v := range c.headers {
		req.Header.Set(k, v)
	}
}

// Get makes a GET request to /api+path.
func (c *Client) Get(ctx context.Context, path string) (*Response, error) {
	return c.Request(ctx, http.MethodGet, path, nil, nil)
}

// Post makes a POST request to /api+path with a JSON body.
func (c *Client) Post(ctx context.Context, path string, body any) (*Response, error) {
	return c.Request(ctx, http.MethodPost, path, body, nil)
}

// Put makes a PUT request to /api+path with a JSON body.
func (c *Client) Put(ctx context.Context, path string, body any) (*Response, error) {
	return c.Request(ctx, http.MethodPut, path, body, nil)
}

// Patch makes a PATCH request to /api+path with a JSON body.
func (c *Client) Patch(ctx context.Context, path string, body any) (*Response, error) {
	return c.Request(ctx, http.MethodPatch, path, body, nil)
}

// Delete makes a DELETE request to /api+path with an optional JSON body.
func (c *Client) Delete(ctx context.Context, path string, body any) (*Response, error) {
	return c.Request(ctx, http.MethodDelete, path, body, nil)
}

// Request makes an authenticated request to /api+path with a JSON body.
// extraHeaders are merged on top of the per-request defaults (e.g. DAL context
// headers).
func (c *Client) Request(ctx context.Context, method, path string, body any, extraHeaders map[string]string) (*Response, error) {
	bodyFactory := func() (io.Reader, error) {
		if body == nil {
			return nil, nil
		}
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		return bytes.NewReader(jsonBody), nil
	}

	headers := map[string]string{}
	if body != nil {
		headers["Content-Type"] = "application/json"
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}

	return c.do(ctx, method, path, bodyFactory, headers, true)
}

// RequestRaw makes an authenticated request to /api+path sending the body
// produced by bodyFactory verbatim, without JSON marshaling. The caller sets
// the Content-Type (and any other headers) via extraHeaders. Use it for file
// uploads (multipart/form-data) or other non-JSON payloads.
//
// bodyFactory is a factory, not a reader, because the request may be retried
// once after a 401 — the body must be reproducible. Return (nil, nil) for an
// empty body.
func (c *Client) RequestRaw(ctx context.Context, method, path string, bodyFactory func() (io.Reader, error), extraHeaders map[string]string) (*Response, error) {
	return c.do(ctx, method, path, bodyFactory, extraHeaders, true)
}

func (c *Client) do(ctx context.Context, method, path string, bodyFactory func() (io.Reader, error), extraHeaders map[string]string, retry bool) (*Response, error) {
	token, err := c.getToken(ctx)
	if err != nil {
		return nil, err
	}

	var reqBody io.Reader
	if bodyFactory != nil {
		reqBody, err = bodyFactory()
		if err != nil {
			return nil, err
		}
	}

	if path != "" && !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	url := c.baseURL + "/api" + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}

	c.applyHeaders(req)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: "redirect detected"}
	}

	if resp.StatusCode == http.StatusUnauthorized && retry {
		c.invalidateToken(ctx)
		return c.do(ctx, method, path, bodyFactory, extraHeaders, false)
	}

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= minErrorStatusCode {
		return nil, newAPIError(resp.StatusCode, respBody)
	}

	return &Response{
		StatusCode: resp.StatusCode,
		Body:       respBody,
		Headers:    resp.Header,
	}, nil
}

// newAPIError builds an APIError, extracting the first error detail from the
// Shopware errors envelope when present.
func newAPIError(statusCode int, body []byte) *APIError {
	apiErr := &APIError{StatusCode: statusCode, Body: string(body)}

	var envelope struct {
		Errors []struct {
			Detail string `json:"detail"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && len(envelope.Errors) > 0 {
		apiErr.Detail = envelope.Errors[0].Detail
	}
	return apiErr
}
