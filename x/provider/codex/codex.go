package codex

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/wire/openai/responses"
	"go.opentelemetry.io/otel/trace"
)

const defaultResponsesEndpoint = "https://chatgpt.com/backend-api/codex/responses"

// Option configures the Codex service provider.
type Option func(*config)

type config struct {
	httpClient     *http.Client
	responsesURL   string
	issuerURL      string
	clientID       string
	credentialPath string
	originator     string
	callbackPorts  []int
	tracer         trace.Tracer
}

// WithHTTPClient replaces the client used for OAuth and inference.
func WithHTTPClient(client *http.Client) Option { return func(c *config) { c.httpClient = client } }

// WithResponsesEndpoint overrides the Codex Responses endpoint. It is useful
// for compatible gateways and tests.
func WithResponsesEndpoint(endpoint string) Option {
	return func(c *config) { c.responsesURL = endpoint }
}

// WithIssuer overrides the OAuth issuer. It is primarily useful for tests.
func WithIssuer(issuer string) Option { return func(c *config) { c.issuerURL = issuer } }

// WithClientID overrides the Codex OAuth client ID.
func WithClientID(clientID string) Option { return func(c *config) { c.clientID = clientID } }

// WithCredentialPath selects ore's private credential file. The provider
// never reads Codex's auth.json.
func WithCredentialPath(path string) Option { return func(c *config) { c.credentialPath = path } }

// WithOriginator sets the service's originator header. The default is "ore".
func WithOriginator(originator string) Option { return func(c *config) { c.originator = originator } }

// WithCallbackPorts selects ports tried by browser login, in order.
func WithCallbackPorts(ports ...int) Option {
	return func(c *config) { c.callbackPorts = append([]int(nil), ports...) }
}

// WithTracer enables tracing in the Responses wire.
func WithTracer(tracer trace.Tracer) Option { return func(c *config) { c.tracer = tracer } }

// Provider composes Codex account management with the OpenAI Responses wire.
type Provider struct {
	auth *authManager
	wire *responses.Provider
}

// New constructs an experimental Codex provider. Construction does not require
// an existing login; Invoke returns ErrNotLoggedIn until login completes.
func New(opts ...Option) (*Provider, error) {
	cfg := config{
		httpClient: http.DefaultClient, responsesURL: defaultResponsesEndpoint,
		issuerURL: defaultIssuer, clientID: defaultClientID,
		originator: "ore", callbackPorts: []int{1455, 1457},
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.credentialPath == "" {
		path, err := defaultCredentialPath()
		if err != nil {
			return nil, err
		}
		cfg.credentialPath = path
	}
	if cfg.httpClient == nil {
		return nil, fmt.Errorf("codex: HTTP client is nil")
	}
	auth, err := newAuthManager(cfg)
	if err != nil {
		return nil, err
	}
	wire, err := responses.New(
		responses.WithEndpoint(cfg.responsesURL),
		responses.WithHTTPClient(cfg.httpClient),
		responses.WithTracer(cfg.tracer),
		responses.WithRequestEditor(auth.editRequest),
	)
	if err != nil {
		return nil, fmt.Errorf("codex: construct Responses wire: %w", err)
	}
	return &Provider{auth: auth, wire: wire}, nil
}

// LoggedIn reports whether credentials are present. Invoke may still refresh
// them before use, so this is an account-state signal rather than a token check.
func (p *Provider) LoggedIn() bool {
	p.auth.mu.Lock()
	defer p.auth.mu.Unlock()
	return p.auth.credential != nil
}

func defaultCredentialPath() (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", fmt.Errorf("codex: resolve config directory: %w", err)
	}
	return filepath.Join(dir, "ore", "codex-credentials.json"), nil
}

// Invoke implements provider.Provider through the reusable Responses wire.
func (p *Provider) Invoke(ctx context.Context, state ledger.State, spec models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	return p.wire.Invoke(ctx, state, spec, ch, opts...)
}

// WithTools configures tools for one invocation.
func WithTools(tools []tool.Tool) provider.InvokeOption { return responses.WithTools(tools) }

// WithSessionID sets the Responses prompt cache key for one invocation.
func WithSessionID(id string) provider.InvokeOption { return responses.WithSessionID(id) }

var _ provider.Provider = (*Provider)(nil)
