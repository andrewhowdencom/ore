package codex

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultIssuer   = "https://auth.openai.com"
	defaultClientID = "app_EMoamEEZ73f0CkXaXp7hrann"
	oauthScope      = "openid profile email offline_access api.connectors.read api.connectors.invoke"
)

// ErrNotLoggedIn is returned by Invoke when no usable ChatGPT credentials exist.
var ErrNotLoggedIn = errors.New("codex: not logged in")

var userConfigDir = os.UserConfigDir

type credential struct {
	IDToken      string    `json:"id_token"`
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	AccountID    string    `json:"account_id"`
	ExpiresAt    time.Time `json:"expires_at"`
}

type authManager struct {
	mu            sync.Mutex
	credential    *credential
	path          string
	issuer        string
	clientID      string
	originator    string
	callbackPorts []int
	httpClient    *http.Client
}

func newAuthManager(cfg config) (*authManager, error) {
	m := &authManager{
		path: cfg.credentialPath, issuer: strings.TrimRight(cfg.issuerURL, "/"),
		clientID: cfg.clientID, originator: cfg.originator,
		callbackPorts: cfg.callbackPorts, httpClient: cfg.httpClient,
	}
	cred, err := loadCredential(cfg.credentialPath)
	if err != nil {
		return nil, err
	}
	m.credential = cred
	return m, nil
}

func (m *authManager) editRequest(ctx context.Context, req *http.Request) error {
	cred, err := m.currentCredential(ctx)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	req.Header.Set("ChatGPT-Account-ID", cred.AccountID)
	req.Header.Set("originator", m.originator)
	req.Header.Set("User-Agent", "ore")
	return nil
}

func (m *authManager) currentCredential(ctx context.Context) (credential, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.credential == nil {
		return credential{}, ErrNotLoggedIn
	}
	if m.credential.ExpiresAt.IsZero() || time.Until(m.credential.ExpiresAt) > 5*time.Minute {
		return *m.credential, nil
	}
	if m.credential.RefreshToken == "" {
		return credential{}, fmt.Errorf("%w: access token expired", ErrNotLoggedIn)
	}
	if err := m.refreshLocked(ctx); err != nil {
		return credential{}, err
	}
	return *m.credential, nil
}

func (m *authManager) refreshLocked(ctx context.Context) error {
	payload := map[string]string{"client_id": m.clientID, "grant_type": "refresh_token", "refresh_token": m.credential.RefreshToken}
	var refreshed tokenResponse
	if err := m.postJSON(ctx, m.issuer+"/oauth/token", payload, &refreshed); err != nil {
		return fmt.Errorf("codex: refresh credentials: %w", err)
	}
	if refreshed.AccessToken == "" {
		refreshed.AccessToken = m.credential.AccessToken
	}
	if refreshed.IDToken == "" {
		refreshed.IDToken = m.credential.IDToken
	}
	next, err := credentialFromTokens(refreshed, m.credential.RefreshToken)
	if err != nil {
		return fmt.Errorf("codex: refresh credentials: %w", err)
	}
	if next.AccountID != m.credential.AccountID {
		return errors.New("codex: refreshed credentials belong to a different ChatGPT account")
	}
	if err := saveCredential(m.path, next); err != nil {
		return err
	}
	m.credential = &next
	return nil
}

// Login describes an in-progress browser or device-code authorization. It
// deliberately exposes no tokens.
type Login struct {
	VerificationURL string
	UserCode        string
	done            chan struct{}
	cancel          context.CancelFunc
	once            sync.Once
	mu              sync.Mutex
	err             error
}

func newLogin(parent context.Context) (*Login, context.Context) {
	ctx, cancel := context.WithCancel(parent)
	return &Login{done: make(chan struct{}), cancel: cancel}, ctx
}

func (l *Login) finish(err error) {
	l.once.Do(func() {
		l.mu.Lock()
		l.err = err
		l.mu.Unlock()
		close(l.done)
		l.cancel()
	})
}

// Wait blocks until login succeeds, fails, is cancelled, or ctx expires.
func (l *Login) Wait(ctx context.Context) error {
	select {
	case <-l.done:
		l.mu.Lock()
		defer l.mu.Unlock()
		return l.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Cancel cancels an in-progress login.
func (l *Login) Cancel() { l.finish(context.Canceled) }

// StartDeviceLogin starts Codex device authorization and background polling.
// The caller renders VerificationURL and UserCode, then calls Wait.
func (p *Provider) StartDeviceLogin(ctx context.Context) (*Login, error) {
	login, loginCtx := newLogin(ctx)
	request := map[string]string{"client_id": p.auth.clientID}
	var response deviceCodeResponse
	if err := p.auth.postJSON(loginCtx, p.auth.issuer+"/api/accounts/deviceauth/usercode", request, &response); err != nil {
		login.finish(err)
		return nil, fmt.Errorf("codex: request device code: %w", err)
	}
	if response.DeviceAuthID == "" || response.UserCode == "" {
		login.finish(errors.New("malformed device-code response"))
		return nil, errors.New("codex: malformed device-code response")
	}
	login.VerificationURL = p.auth.issuer + "/codex/device"
	login.UserCode = response.UserCode
	go func() { login.finish(p.auth.completeDeviceLogin(loginCtx, response)) }()
	return login, nil
}

type deviceCodeResponse struct {
	DeviceAuthID string          `json:"device_auth_id"`
	UserCode     string          `json:"user_code"`
	Interval     json.RawMessage `json:"interval"`
}

func (r deviceCodeResponse) pollInterval() time.Duration {
	var seconds int
	if json.Unmarshal(r.Interval, &seconds) != nil {
		var text string
		if json.Unmarshal(r.Interval, &text) == nil {
			seconds, _ = strconv.Atoi(text)
		}
	}
	if seconds < 0 {
		seconds = 0
	}
	return time.Duration(seconds) * time.Second
}

func (m *authManager) completeDeviceLogin(ctx context.Context, code deviceCodeResponse) error {
	endpoint := m.issuer + "/api/accounts/deviceauth/token"
	for {
		var result deviceTokenResponse
		status, err := m.postJSONStatus(ctx, endpoint, map[string]string{"device_auth_id": code.DeviceAuthID, "user_code": code.UserCode}, &result)
		if err == nil && status >= 200 && status < 300 {
			return m.exchangeAndStore(ctx, result.AuthorizationCode, m.issuer+"/deviceauth/callback", result.CodeVerifier)
		}
		if status != http.StatusForbidden && status != http.StatusNotFound {
			if err != nil {
				return fmt.Errorf("codex: poll device login: %w", err)
			}
			return fmt.Errorf("codex: poll device login: HTTP %d", status)
		}
		timer := time.NewTimer(code.pollInterval())
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}

type deviceTokenResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

// StartBrowserLogin starts a loopback PKCE login. The application opens or
// renders VerificationURL; completion and cancellation are observed via Wait.
func (p *Provider) StartBrowserLogin(ctx context.Context) (*Login, error) {
	listener, err := listenLoopback(p.auth.callbackPorts)
	if err != nil {
		return nil, fmt.Errorf("codex: start browser callback: %w", err)
	}
	login, loginCtx := newLogin(ctx)
	verifier, challenge, err := newPKCE()
	if err != nil {
		listener.Close()
		return nil, err
	}
	state, err := randomURLToken(32)
	if err != nil {
		listener.Close()
		return nil, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://localhost:%d/auth/callback", port)
	values := url.Values{
		"response_type": {"code"}, "client_id": {p.auth.clientID}, "redirect_uri": {redirectURI},
		"scope": {oauthScope}, "code_challenge": {challenge}, "code_challenge_method": {"S256"},
		"id_token_add_organizations": {"true"}, "codex_cli_simplified_flow": {"true"},
		"state": {state}, "originator": {p.auth.originator},
	}
	login.VerificationURL = p.auth.issuer + "/oauth/authorize?" + values.Encode()
	mux := http.NewServeMux()
	server := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	mux.HandleFunc("/auth/callback", func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Query().Get("state") != state {
			http.Error(w, "invalid OAuth state", http.StatusBadRequest)
			login.finish(errors.New("codex: OAuth state mismatch"))
			return
		}
		if oauthErr := req.URL.Query().Get("error"); oauthErr != "" {
			http.Error(w, "login failed", http.StatusBadRequest)
			login.finish(fmt.Errorf("codex: OAuth authorization failed: %s", oauthErr))
			return
		}
		code := req.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			login.finish(errors.New("codex: OAuth callback missing code"))
			return
		}
		if err := p.auth.exchangeAndStore(loginCtx, code, redirectURI, verifier); err != nil {
			http.Error(w, "login failed", http.StatusBadGateway)
			login.finish(err)
			return
		}
		_, _ = io.WriteString(w, "Login complete. You may close this window.")
		login.finish(nil)
	})
	go func() {
		err := server.Serve(listener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			login.finish(fmt.Errorf("codex: browser callback: %w", err))
		}
	}()
	go func() {
		select {
		case <-ctx.Done():
			login.finish(ctx.Err())
		case <-login.done:
		}
	}()
	go func() {
		<-loginCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	return login, nil
}

func listenLoopback(ports []int) (net.Listener, error) {
	var last error
	for _, port := range ports {
		listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			return listener, nil
		}
		last = err
	}
	if len(ports) == 0 {
		return net.Listen("tcp", "127.0.0.1:0")
	}
	return nil, last
}

func newPKCE() (string, string, error) {
	verifier, err := randomURLToken(64)
	if err != nil {
		return "", "", fmt.Errorf("codex: generate PKCE verifier: %w", err)
	}
	digest := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(digest[:]), nil
}

func randomURLToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

type tokenResponse struct {
	IDToken      string `json:"id_token"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

func (m *authManager) exchangeAndStore(ctx context.Context, code, redirectURI, verifier string) error {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {redirectURI}, "client_id": {m.clientID}, "code_verifier": {verifier}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.issuer+"/oauth/token", strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("codex: create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("codex: exchange authorization code: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return safeHTTPError("token exchange", resp)
	}
	var tokens tokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tokens); err != nil {
		return fmt.Errorf("codex: decode token response: %w", err)
	}
	cred, err := credentialFromTokens(tokens, "")
	if err != nil {
		return fmt.Errorf("codex: validate token response: %w", err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := saveCredential(m.path, cred); err != nil {
		return err
	}
	m.credential = &cred
	return nil
}

func credentialFromTokens(tokens tokenResponse, fallbackRefresh string) (credential, error) {
	if tokens.AccessToken == "" {
		return credential{}, errors.New("missing access token")
	}
	if tokens.RefreshToken == "" {
		tokens.RefreshToken = fallbackRefresh
	}
	accountID, expiry := claimsFromJWT(tokens.AccessToken)
	if accountID == "" {
		var idExpiry time.Time
		accountID, idExpiry = claimsFromJWT(tokens.IDToken)
		if expiry.IsZero() {
			expiry = idExpiry
		}
	}
	if accountID == "" {
		return credential{}, errors.New("token lacks ChatGPT account ID")
	}
	return credential{IDToken: tokens.IDToken, AccessToken: tokens.AccessToken, RefreshToken: tokens.RefreshToken, AccountID: accountID, ExpiresAt: expiry}, nil
}

func claimsFromJWT(token string) (string, time.Time) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return "", time.Time{}
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", time.Time{}
	}
	var claims struct {
		Expires int64 `json:"exp"`
		Auth    struct {
			AccountID string `json:"chatgpt_account_id"`
		} `json:"https://api.openai.com/auth"`
		AccountID string `json:"chatgpt_account_id"`
	}
	if json.Unmarshal(payload, &claims) != nil {
		return "", time.Time{}
	}
	account := claims.Auth.AccountID
	if account == "" {
		account = claims.AccountID
	}
	var expiry time.Time
	if claims.Expires > 0 {
		expiry = time.Unix(claims.Expires, 0)
	}
	return account, expiry
}

// Logout best-effort revokes the credential and always removes local state.
func (p *Provider) Logout(ctx context.Context) error {
	p.auth.mu.Lock()
	defer p.auth.mu.Unlock()
	var revokeErr error
	if p.auth.credential != nil {
		token, hint := p.auth.credential.RefreshToken, "refresh_token"
		if token == "" {
			token, hint = p.auth.credential.AccessToken, "access_token"
		}
		if token != "" {
			payload := map[string]string{"token": token, "token_type_hint": hint}
			if hint == "refresh_token" {
				payload["client_id"] = p.auth.clientID
			}
			revokeErr = p.auth.postJSON(ctx, p.auth.issuer+"/oauth/revoke", payload, nil)
		}
	}
	p.auth.credential = nil
	if err := os.Remove(p.auth.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("codex: remove credentials: %w", err)
	}
	return revokeErr
}

func loadCredential(path string) (*credential, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("codex: stat credentials: %w", err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("codex: credential file %q must not be accessible by group or others", path)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("codex: open credentials: %w", err)
	}
	defer file.Close()
	var cred credential
	if err := json.NewDecoder(io.LimitReader(file, 1<<20)).Decode(&cred); err != nil {
		return nil, fmt.Errorf("codex: decode credentials: %w", err)
	}
	return &cred, nil
}

func saveCredential(path string, cred credential) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("codex: create credential directory: %w", err)
	}
	file, err := os.CreateTemp(dir, ".codex-credentials-*")
	if err != nil {
		return fmt.Errorf("codex: create credential file: %w", err)
	}
	temp := file.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(temp)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("codex: protect credential file: %w", err)
	}
	if err := json.NewEncoder(file).Encode(cred); err != nil {
		file.Close()
		return fmt.Errorf("codex: write credentials: %w", err)
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return fmt.Errorf("codex: sync credentials: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("codex: close credentials: %w", err)
	}
	if err := os.Rename(temp, path); err != nil {
		return fmt.Errorf("codex: replace credentials: %w", err)
	}
	ok = true
	return nil
}

func (m *authManager) postJSON(ctx context.Context, endpoint string, payload any, target any) error {
	status, err := m.postJSONStatus(ctx, endpoint, payload, target)
	if err != nil {
		return err
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("HTTP %d", status)
	}
	return nil
}

func (m *authManager) postJSONStatus(ctx context.Context, endpoint string, payload any, target any) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(body)))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("originator", m.originator)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64*1024))
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if target != nil {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(target); err != nil {
			return resp.StatusCode, err
		}
	}
	return resp.StatusCode, nil
}

func safeHTTPError(operation string, resp *http.Response) error {
	var detail struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&detail)
	message := detail.Error
	if detail.ErrorDescription != "" {
		message += ": " + detail.ErrorDescription
	}
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	return fmt.Errorf("codex: %s returned HTTP %d: %s", operation, resp.StatusCode, message)
}
