package app

import (
	"context"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockConduit struct {
	startCalled bool
}

func (m *mockConduit) Start(ctx context.Context) error {
	m.startCalled = true
	<-ctx.Done()
	return ctx.Err()
}

type mockHandler struct{}

func (m *mockHandler) Handle(ctx context.Context, art artifact.Artifact, s state.State) error {
	return nil
}

type mockProvider struct{}

func (m *mockProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	close(ch)
	return nil
}

type mockStore struct{}

func (m *mockStore) Create() (*thread.Thread, error) {
	return &thread.Thread{ID: "test"}, nil
}
func (m *mockStore) Get(id string) (*thread.Thread, bool) {
	return &thread.Thread{ID: id}, true
}
func (m *mockStore) GetBy(key, value string) (*thread.Thread, bool) {
	return nil, false
}
func (m *mockStore) Save(t *thread.Thread) error { return nil }
func (m *mockStore) Delete(id string) bool { return false }
func (m *mockStore) List() ([]*thread.Thread, error) { return nil, nil }

func TestRunAgent_Success(t *testing.T) {
	v := viper.New()
	v.Set("log_level", "info")
	v.Set("api_key", "test-key")
	v.Set("model", "gpt-4o")
	v.Set("base_url", "")
	v.Set("store_dir", "")

	cmd := &cobra.Command{Use: "test"}

	mc := &mockConduit{}
	cfg := &appConfig{
		conduits: []ConduitRegistration{
			{
				Name: "http",
				Factory: func(mgr *session.Manager, opts map[string]any) (conduit.Conduit, error) {
					return mc, nil
				},
			},
		},
		providerFactory: func(apiKey, model, baseURL string) (provider.Provider, error) {
			return &mockProvider{}, nil
		},
		storeFactory: func(dir string) (thread.Store, error) {
			return &mockStore{}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cfg.contextFactory = func() (context.Context, func()) {
		return ctx, cancel
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	err := runAgent(cmd, v, cfg)
	require.NoError(t, err)
	assert.True(t, mc.startCalled)
}

func TestRunAgent_MissingAPIKey(t *testing.T) {
	v := viper.New()
	v.Set("log_level", "info")
	v.Set("api_key", "")

	cmd := &cobra.Command{Use: "test"}
	cfg := &appConfig{}

	err := runAgent(cmd, v, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "api-key is required")
}

func TestRunAgent_HandlerFactoryNotInvoked(t *testing.T) {
	// Handler factories are created lazily by session.NewManager and only
	// invoked when a stream is created. runAgent itself does not eagerly
	// validate handler factories, so a bad handler factory does not cause
	// runAgent to fail. This test documents that behavior.
	v := viper.New()
	v.Set("log_level", "info")
	v.Set("api_key", "test-key")
	v.Set("model", "gpt-4o")
	v.Set("base_url", "")
	v.Set("store_dir", "")

	cmd := &cobra.Command{Use: "test"}

	mc := &mockConduit{}
	cfg := &appConfig{
		handlers: []HandlerRegistration{
			{
				Name: "bad",
				Factory: func(opts map[string]any) (loop.Handler, error) {
					return nil, assert.AnError
				},
			},
		},
		conduits: []ConduitRegistration{
			{
				Name: "http",
				Factory: func(mgr *session.Manager, opts map[string]any) (conduit.Conduit, error) {
					return mc, nil
				},
			},
		},
		providerFactory: func(apiKey, model, baseURL string) (provider.Provider, error) {
			return &mockProvider{}, nil
		},
		storeFactory: func(dir string) (thread.Store, error) {
			return &mockStore{}, nil
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	cfg.contextFactory = func() (context.Context, func()) {
		return ctx, cancel
	}

	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	// runAgent succeeds because handler factory is never called (no stream created)
	err := runAgent(cmd, v, cfg)
	require.NoError(t, err)
}

func TestRunAgent_ConduitError(t *testing.T) {
	v := viper.New()
	v.Set("log_level", "info")
	v.Set("api_key", "test-key")
	v.Set("model", "gpt-4o")
	v.Set("base_url", "")
	v.Set("store_dir", "")

	cmd := &cobra.Command{Use: "test"}

	cfg := &appConfig{
		conduits: []ConduitRegistration{
			{
				Name: "bad",
				Factory: func(mgr *session.Manager, opts map[string]any) (conduit.Conduit, error) {
					return nil, assert.AnError
				},
			},
		},
		providerFactory: func(apiKey, model, baseURL string) (provider.Provider, error) {
			return &mockProvider{}, nil
		},
		storeFactory: func(dir string) (thread.Store, error) {
			return &mockStore{}, nil
		},
	}

	err := runAgent(cmd, v, cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create conduit bad")
}

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"debug", "DEBUG", false},
		{"info", "INFO", false},
		{"warn", "WARN", false},
		{"warning", "WARN", false},
		{"error", "ERROR", false},
		{"invalid", "INFO", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := parseLogLevel(tt.input)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.String())
		})
	}
}
