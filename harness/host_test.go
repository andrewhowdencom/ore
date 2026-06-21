package harness

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var _ conduit.Conduit = (*mockConduit)(nil)

type mockConduit struct {
	startFunc func(ctx context.Context) error
}

func (m *mockConduit) Start(ctx context.Context) error {
	return m.startFunc(ctx)
}

func TestNew(t *testing.T) {
	h := New(nil)
	require.NotNil(t, h)
}

func TestHost_Add(t *testing.T) {
	h := New(nil)
	m := &mockConduit{startFunc: func(context.Context) error { return nil }}
	h.Add(m)
	require.Len(t, h.conduits, 1)
}

func TestHost_Add_Nil(t *testing.T) {
	h := New(nil)
	h.Add(nil)
	require.Len(t, h.conduits, 0)
}

func TestHost_Run(t *testing.T) {
	tests := []struct {
		name      string
		conduits  []func(context.Context) error
		wantErr   string
		cancelCtx bool
	}{
		{
			name:     "no conduits",
			conduits: nil,
		},
		{
			name: "single conduit succeeds immediately",
			conduits: []func(context.Context) error{
				func(context.Context) error { return nil },
			},
		},
		{
			name: "single conduit errors immediately",
			conduits: []func(context.Context) error{
				func(context.Context) error { return errors.New("conduit error") },
			},
			wantErr: "conduit error",
		},
		{
			name: "multiple conduits all succeed",
			conduits: []func(context.Context) error{
				func(context.Context) error { return nil },
				func(context.Context) error { return nil },
			},
		},
		{
			name: "multiple conduits one errors cancels others",
			conduits: []func(context.Context) error{
				func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				},
				func(context.Context) error {
					return errors.New("early error")
				},
			},
			wantErr: "early error",
		},
		{
			name: "context cancellation",
			conduits: []func(context.Context) error{
				func(ctx context.Context) error {
					<-ctx.Done()
					return ctx.Err()
				},
			},
			cancelCtx: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := New(nil)
			for _, fn := range tt.conduits {
				h.Add(&mockConduit{startFunc: fn})
			}

			ctx := context.Background()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				go func() {
					time.Sleep(50 * time.Millisecond)
					cancel()
				}()
			}

			err := h.Run(ctx)
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}

			if tt.cancelCtx {
				require.NoError(t, err)
				return
			}

			require.NoError(t, err)
		})
	}
}

func TestHost_Run_ConcurrentStartup(t *testing.T) {
	h := New(nil)
	started := make(chan struct{}, 3)

	for i := 0; i < 3; i++ {
		h.Add(&mockConduit{startFunc: func(ctx context.Context) error {
			started <- struct{}{}
			<-ctx.Done()
			return ctx.Err()
		}})
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		// Wait for all conduits to start, then cancel
		for i := 0; i < 3; i++ {
			<-started
		}
		cancel()
	}()

	// Run returns nil when context is cancelled externally
	err := h.Run(ctx)
	require.NoError(t, err)
}