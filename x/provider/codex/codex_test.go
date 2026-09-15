package codex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeviceLoginPersistsCredentialsAndAuthorizesInvoke(t *testing.T) {
	var polls atomic.Int32
	var inferenceHeaders http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/accounts/deviceauth/usercode":
			writeJSON(w, map[string]any{"device_auth_id": "device-1", "user_code": "ABCD-1234", "interval": "0"})
		case "/api/accounts/deviceauth/token":
			if polls.Add(1) == 1 {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			writeJSON(w, map[string]string{"authorization_code": "auth-code", "code_verifier": "verifier"})
		case "/oauth/token":
			require.Equal(t, "authorization_code", readForm(t, r).Get("grant_type"))
			writeJSON(w, testTokens("account-1", time.Now().Add(time.Hour), "refresh-1"))
		case "/responses":
			inferenceHeaders = r.Header.Clone()
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"type\":\"response.output_text.delta\",\"delta\":\"ok\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\"}}\n\n")
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "credentials.json")
	p, err := New(WithIssuer(server.URL), WithResponsesEndpoint(server.URL+"/responses"), WithCredentialPath(path))
	require.NoError(t, err)
	assert.False(t, p.LoggedIn())
	login, err := p.StartDeviceLogin(context.Background())
	require.NoError(t, err)
	assert.Equal(t, server.URL+"/codex/device", login.VerificationURL)
	assert.Equal(t, "ABCD-1234", login.UserCode)
	require.NoError(t, login.Wait(context.Background()))
	assert.True(t, p.LoggedIn())

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	ch := make(chan artifact.Artifact, 4)
	require.NoError(t, p.Invoke(context.Background(), ledger.NewThread(), models.Spec{Name: "gpt-test"}, ch))
	assert.True(t, strings.HasPrefix(inferenceHeaders.Get("Authorization"), "Bearer "))
	assert.Equal(t, "account-1", inferenceHeaders.Get("ChatGPT-Account-ID"))
	assert.Equal(t, "ore", inferenceHeaders.Get("originator"))

	p2, err := New(WithIssuer(server.URL), WithResponsesEndpoint(server.URL+"/responses"), WithCredentialPath(path))
	require.NoError(t, err)
	require.NoError(t, p2.Invoke(context.Background(), ledger.NewThread(), models.Spec{Name: "gpt-test"}, make(chan artifact.Artifact, 4)))
}

func TestBrowserLoginCompletesThroughLoopbackCallback(t *testing.T) {
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/oauth/token", r.URL.Path)
		form := readForm(t, r)
		require.Equal(t, "browser-code", form.Get("code"))
		require.NotEmpty(t, form.Get("code_verifier"))
		writeJSON(w, testTokens("browser-account", time.Now().Add(time.Hour), "refresh"))
	}))
	defer issuer.Close()
	p, err := New(WithIssuer(issuer.URL), WithCredentialPath(filepath.Join(t.TempDir(), "credentials.json")), WithCallbackPorts())
	require.NoError(t, err)
	login, err := p.StartBrowserLogin(context.Background())
	require.NoError(t, err)
	authURL, err := url.Parse(login.VerificationURL)
	require.NoError(t, err)
	callback := authURL.Query().Get("redirect_uri") + "?code=browser-code&state=" + url.QueryEscape(authURL.Query().Get("state"))
	resp, err := http.Get(callback)
	require.NoError(t, err)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	require.NoError(t, login.Wait(context.Background()))
}

func TestConcurrentExpiredCredentialRefreshIsCoalesced(t *testing.T) {
	var refreshes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		refreshes.Add(1)
		var body map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		assert.Equal(t, "refresh_token", body["grant_type"])
		writeJSON(w, testTokens("account-1", time.Now().Add(time.Hour), "refresh-2"))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "credentials.json")
	require.NoError(t, saveCredential(path, credential{AccessToken: "expired", RefreshToken: "refresh-1", AccountID: "account-1", ExpiresAt: time.Now().Add(-time.Minute)}))
	p, err := New(WithIssuer(server.URL), WithCredentialPath(path))
	require.NoError(t, err)
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := p.auth.currentCredential(context.Background()); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	assert.Equal(t, int32(1), refreshes.Load())
}

func TestDeviceLoginCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/accounts/deviceauth/usercode" {
			writeJSON(w, map[string]any{"device_auth_id": "device-1", "user_code": "CODE", "interval": "1"})
			return
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	p, err := New(WithIssuer(server.URL), WithCredentialPath(filepath.Join(t.TempDir(), "credentials.json")))
	require.NoError(t, err)
	login, err := p.StartDeviceLogin(context.Background())
	require.NoError(t, err)
	login.Cancel()
	assert.ErrorIs(t, login.Wait(context.Background()), context.Canceled)
}

func TestBrowserLoginParentCancellationIsObservable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p, err := New(WithIssuer("http://issuer.invalid"), WithCredentialPath(filepath.Join(t.TempDir(), "credentials.json")), WithCallbackPorts())
	require.NoError(t, err)
	login, err := p.StartBrowserLogin(ctx)
	require.NoError(t, err)
	cancel()
	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	assert.ErrorIs(t, login.Wait(waitCtx), context.Canceled)
}

func TestMalformedDeviceCodeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, map[string]string{"user_code": "CODE"}) }))
	defer server.Close()
	p, err := New(WithIssuer(server.URL), WithCredentialPath(filepath.Join(t.TempDir(), "credentials.json")))
	require.NoError(t, err)
	_, err = p.StartDeviceLogin(context.Background())
	require.ErrorContains(t, err, "malformed device-code response")
}

func TestLogoutRevokesAndDeletesCredential(t *testing.T) {
	var revoked map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/oauth/revoke", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&revoked))
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "credentials.json")
	require.NoError(t, saveCredential(path, credential{AccessToken: "access", RefreshToken: "refresh", AccountID: "account"}))
	p, err := New(WithIssuer(server.URL), WithCredentialPath(path))
	require.NoError(t, err)
	require.NoError(t, p.Logout(context.Background()))
	assert.False(t, p.LoggedIn())
	assert.Equal(t, "refresh", revoked["token"])
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist)
	_, err = p.auth.currentCredential(context.Background())
	assert.ErrorIs(t, err, ErrNotLoggedIn)
}

func TestRejectsInsecureCredentialPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.json")
	require.NoError(t, os.WriteFile(path, []byte(`{}`), 0o644))
	_, err := New(WithCredentialPath(path))
	require.ErrorContains(t, err, "must not be accessible")
}

func testTokens(account string, expiry time.Time, refresh string) map[string]string {
	return map[string]string{"id_token": jwt(account, expiry), "access_token": jwt(account, expiry), "refresh_token": refresh}
}

func jwt(account string, expiry time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
	payload, _ := json.Marshal(map[string]any{"exp": expiry.Unix(), "https://api.openai.com/auth": map[string]string{"chatgpt_account_id": account}})
	return fmt.Sprintf("%s.%s.", header, base64.RawURLEncoding.EncodeToString(payload))
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func readForm(t *testing.T, r *http.Request) url.Values {
	t.Helper()
	require.NoError(t, r.ParseForm())
	return r.Form
}
