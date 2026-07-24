// Package main is a reference application demonstrating the
// x/conduit/tui conduit wired together with the session.Runner and
// agent.Agent primitives. It shows the canonical "dumb pipe" pattern:
// the TUI accepts an already-attached *session.Session, exposes user
// actions on an outbound channel via Events(), and emits
// session.UserMessageEvent / session.InterruptEvent values that the
// application feeds into runner.Run.
//
// Usage:
//
//	ORE_API_KEY=... go run ./examples/tui-chat
//
// Type a message and press Enter. Press Esc to emit an interrupt
// event (does not quit). Press Ctrl+C (or send SIGINT) to interrupt
// any in-flight turn and quit. ORE_THREAD_ID optionally resumes an
// existing thread; otherwise a new thread ID is generated.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/x/conduit/tui"
	"github.com/andrewhowdencom/ore/x/provider/openai"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("fatal error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 1. Build the provider from the environment. The agent factory
	//    below uses this provider; the runner never touches it
	//    directly — the agent does.
	apiKey := os.Getenv("ORE_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("ORE_API_KEY not set")
	}
	modelName := os.Getenv("ORE_MODEL")
	if modelName == "" {
		modelName = "gpt-4o"
	}
	var providerOpts []openai.Option
	providerOpts = append(providerOpts, openai.WithAPIKey(apiKey))
	if baseURL := os.Getenv("ORE_BASE_URL"); baseURL != "" {
		providerOpts = append(providerOpts, openai.WithBaseURL(baseURL))
	}
	prov, err := openai.New(providerOpts...)
	if err != nil {
		return fmt.Errorf("create openai provider: %w", err)
	}

	// 2. Build a session factory. DefaultFactory derives a per-turn
	//    models.Spec from session metadata; absent metadata, the
	//    agent's default Spec (set below via WithSpec) is used.
	factory := session.NewDefaultFactory(prov, &cognitive.ReAct{}, nil)

	// 3. Build the runner. It owns the AgentFactory and drives
	//    inference against sessions. Run is synchronous — events are
	//    processed in the caller's goroutine.
	runner := session.NewRunner(session.WithFactory(factory))

	// 4. Construct (or attach to) a session. The thread ID may be
	//    supplied via ORE_THREAD_ID to resume; otherwise we generate
	//    a new one. The session is a pure-data primitive; the runner
	//    does not know it exists until we Register it.
	threadID := os.Getenv("ORE_THREAD_ID")
	if threadID == "" {
		// Stdlib-only thread id: time-based nanos with a "tui-chat-"
		// prefix to keep it readable in logs.
		threadID = "tui-chat-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	sess := session.New(threadID, ledger.NewThread())
	runner.Register(sess)
	defer sess.Close()

	// 5. Seed default metadata on the session before constructing the
	//    TUI. The TUI subscribes to live events only, so any
	//    metadata seeded here is what the status bar shows on the
	//    first frame.
	sess.SetMetadata("thread_id", threadID)
	sess.SetMetadata("model", modelName)
	if cwd, err := os.Getwd(); err == nil {
		sess.SetMetadata("cwd", cwd)
	}

	// 6. Construct the TUI conduit. Pass the cancel func so the TUI
	//    can unwind the shared context when the user presses Ctrl+C
	//    or Esc. The same cancel func is held by the application
	//    for SIGINT, so a single signal unwinds the UI, any
	//    in-flight runner.Run, and the runner pump.
	tuiC, err := tui.New(sess,
		tui.WithName("ore"),
		tui.WithCancelFunc(cancel),
	)
	if err != nil {
		return fmt.Errorf("create tui conduit: %w", err)
	}

	// 7. Wire SIGINT to the shared cancel func. Bubble Tea already
	//    handles Ctrl+C inside the UI, but OS-level SIGINT (e.g.
	//    `kill -INT <pid>`) and terminal close signals also need a
	//    path. This single cancel() call closes all three doors.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig, ok := <-sigCh
		if !ok {
			return
		}
		slog.Info("signal received; cancelling", "signal", sig.String())
		cancel()
	}()

	// 8. Run the runner pump. The TUI emits session.Event values on
	//    its Events() channel; we submit each into runner.Run against
	//    the shared context so a single cancel unwinds both the UI
	//    loop and any in-flight inference.
	events := tuiC.(*tui.TUI).Events()
	pumpDone := make(chan struct{})
	go func() {
		defer close(pumpDone)
		for evt := range events {
			if err := runner.Run(ctx, sess, evt); err != nil {
				slog.Error("runner.Run failed", "err", err)
				cancel()
				return
			}
		}
	}()

	// 9. Start the TUI. Blocks until the user quits (Ctrl+C), the
	//    SIGINT handler cancels ctx, or a fatal error occurs.
	startErr := tuiC.Start(ctx)

	// 10. Cancel to unblock the runner pump if it is waiting on
	//     runner.Run, then wait for it to drain.
	cancel()
	<-pumpDone

	if startErr != nil {
		return fmt.Errorf("tui.Start: %w", startErr)
	}
	return nil
}
