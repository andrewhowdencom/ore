package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"

	"github.com/andrewhowdencom/ore/x/conduit"
	"go.opentelemetry.io/otel/trace"
)

// Descriptor enumerates the capabilities of the Telegram conduit.
var Descriptor = conduit.Descriptor{
	Name:        "Telegram",
	Description: "Telegram Bot API conduit via long-polling",
	Capabilities: []conduit.Capability{
		conduit.CapEventSource,
		conduit.CapAcceptText,
		conduit.CapRenderTurn,
	},
}

// telegramConduit implements conduit.Conduit for the Telegram Bot API.
type telegramConduit struct {
	mgr      *junk.Manager
	botToken string
	client   *http.Client
	timeout  int // getUpdates timeout in seconds
	baseURL  string
	tracer   trace.Tracer
}

// Option configures the telegramConduit via functional options.
type Option func(*telegramConduit)

// WithBotToken sets the Telegram bot token.
func WithBotToken(token string) Option {
	return func(c *telegramConduit) {
		c.botToken = token
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(c *telegramConduit) {
		c.client = client
	}
}

// WithGetUpdatesTimeout sets the long-polling timeout in seconds (default 30).
func WithGetUpdatesTimeout(seconds int) Option {
	return func(c *telegramConduit) {
		c.timeout = seconds
	}
}

// WithTracer configures an OpenTelemetry tracer for the Telegram conduit.
func WithTracer(tracer trace.Tracer) Option {
	return func(c *telegramConduit) {
		c.tracer = tracer
	}
}

// withBaseURL sets the Telegram API base URL. Used only for testing.
func withBaseURL(url string) Option {
	return func(c *telegramConduit) {
		c.baseURL = url
	}
}

// New creates a new Telegram conduit that implements conduit.Conduit.
func New(mgr *junk.Manager, opts ...Option) (conduit.Conduit, error) {
	if mgr == nil {
		return nil, fmt.Errorf("session manager is required")
	}
	c := &telegramConduit{
		mgr:     mgr,
		client:  &http.Client{Timeout: 60 * time.Second},
		timeout: 30,
		baseURL: "https://api.telegram.org",
	}
	for _, opt := range opts {
		opt(c)
	}
	return c, nil
}

// Start implements conduit.Conduit. It validates the bot token, registers a
// turn_complete sink, starts the long-polling loop, and blocks until ctx is
// cancelled.
func (c *telegramConduit) Start(ctx context.Context) error {
	if c.botToken == "" {
		return fmt.Errorf("bot token is required")
	}

	// Validate token and get bot user ID for echo suppression.
	botUserID, err := c.getMe(ctx)
	if err != nil {
		return fmt.Errorf("validate bot token: %w", err)
	}

	// Register sink for turn_complete events from all streams.
	cleanup := c.mgr.RegisterSink([]string{"turn_complete"}, func(streamID string, event loop.OutputEvent) {
		if p, _ := loop.ProvenanceFrom(event.Context()); p != "telegram" {
			return
		}

		tc, ok := event.(loop.TurnCompleteEvent)
		if !ok {
			return
		}

		// Only reply to assistant turns.
		if tc.Turn.Role != ledger.RoleAssistant {
			return
		}

		// Extract all text artifacts from the turn.
		var parts []string
		for _, art := range tc.Turn.Artifacts {
			if t, ok := art.(artifact.Text); ok {
				parts = append(parts, t.Content)
			}
		}
		if len(parts) == 0 {
			return
		}
		text := strings.Join(parts, "\n")

		chatID, err := strconv.ParseInt(streamID, 10, 64)
		if err != nil {
			slog.Error("telegram: invalid chat_id in streamID", "streamID", streamID, "err", err)
			return
		}

		if err := c.sendMessage(ctx, chatID, text); err != nil {
			slog.Error("telegram: sendMessage failed", "chat_id", chatID, "err", err)
		}
	})
	defer cleanup()

	// Start polling loop.
	go c.poll(ctx, botUserID)

	// Block until shutdown.
	<-ctx.Done()

	return nil
}

// poll runs the long-polling getUpdates loop in a background goroutine.
func (c *telegramConduit) poll(ctx context.Context, botUserID int64) {
	offset := 0
	for {
		if err := ctx.Err(); err != nil {
			return
		}

		updates, err := c.getUpdates(ctx, offset)
		if err != nil {
			slog.Error("telegram: getUpdates failed", "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
			continue
		}

		for _, update := range updates {
			if err := ctx.Err(); err != nil {
				return
			}

			if update.Message == nil || update.Message.Text == "" || update.Message.Chat == nil {
				continue
			}
			if update.Message.From != nil && update.Message.From.ID == botUserID {
				continue
			}

			chatIDStr := strconv.FormatInt(update.Message.Chat.ID, 10)
			stream, err := c.getOrCreateStream(chatIDStr)
			if err != nil {
				slog.Error("telegram: get or create stream failed", "chat_id", chatIDStr, "err", err)
				continue
			}

			turnCtx := ctx
			var span trace.Span
			if c.tracer != nil {
				turnCtx, span = c.tracer.Start(turnCtx, "telegram.turn", trace.WithSpanKind(trace.SpanKindServer))
			}

			event := junk.UserMessageEvent{
				Content: update.Message.Text,
				Ctx:     loop.WithProvenance(turnCtx, "telegram"),
			}
			if err := stream.Submit(event); err != nil {
				slog.Error("telegram: submit event failed", "chat_id", chatIDStr, "err", err)
			}
			if span != nil {
				span.End()
			}

			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
		}
	}
}

// getOrCreateStream attempts to attach to an existing stream for the given
// thread ID (Telegram chat_id). If the thread does not exist in the store, it
// creates one with the chat_id as its deterministic ID so that future sessions
// for the same chat can be resumed.
func (c *telegramConduit) getOrCreateStream(chatIDStr string) (*junk.Stream, error) {
	stream, err := c.mgr.Attach(chatIDStr)
	if err == nil {
		return stream, nil
	}

	// Thread not found; create it with the chat_id as the thread ID.
	stream, err = c.mgr.CreateWithID(chatIDStr)
	if err != nil {
		return nil, fmt.Errorf("create thread: %w", err)
	}

	return stream, nil
}

// Telegram API structs.

type update struct {
	UpdateID int      `json:"update_id"`
	Message  *message `json:"message"`
}

type message struct {
	MessageID int    `json:"message_id"`
	From      *user  `json:"from"`
	Chat      *chat  `json:"chat"`
	Text      string `json:"text"`
}

type user struct {
	ID       int64  `json:"id"`
	IsBot    bool   `json:"is_bot"`
	Username string `json:"username"`
}

type chat struct {
	ID int64 `json:"id"`
}

type getUpdatesReq struct {
	Offset  int `json:"offset"`
	Limit   int `json:"limit"`
	Timeout int `json:"timeout"`
}

type getUpdatesResp struct {
	OK     bool     `json:"ok"`
	Result []update `json:"result"`
}

type sendMessageReq struct {
	ChatID int64  `json:"chat_id"`
	Text   string `json:"text"`
}

type sendMessageResp struct {
	OK bool `json:"ok"`
}

type getMeResp struct {
	OK     bool `json:"ok"`
	Result user `json:"result"`
}

// getMe calls the Telegram getMe endpoint to validate the token and return
// the bot's user ID.
func (c *telegramConduit) getMe(ctx context.Context) (int64, error) {
	url := fmt.Sprintf("%s/bot%s/getMe", c.baseURL, c.botToken)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("create getMe request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("getMe request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("getMe returned status %d", resp.StatusCode)
	}

	var result getMeResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0, fmt.Errorf("decode getMe response: %w", err)
	}

	if !result.OK {
		return 0, fmt.Errorf("getMe returned ok=false")
	}

	return result.Result.ID, nil
}

// getUpdates calls the Telegram getUpdates endpoint with long-polling.
func (c *telegramConduit) getUpdates(ctx context.Context, offset int) ([]update, error) {
	url := fmt.Sprintf("%s/bot%s/getUpdates", c.baseURL, c.botToken)

	payload := getUpdatesReq{
		Offset:  offset,
		Limit:   100,
		Timeout: c.timeout,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal getUpdates request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create getUpdates request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("getUpdates request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getUpdates returned status %d", resp.StatusCode)
	}

	var result getUpdatesResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode getUpdates response: %w", err)
	}

	if !result.OK {
		return nil, fmt.Errorf("getUpdates returned ok=false")
	}

	return result.Result, nil
}

// sendMessage sends a text message to a chat.
func (c *telegramConduit) sendMessage(ctx context.Context, chatID int64, text string) error {
	url := fmt.Sprintf("%s/bot%s/sendMessage", c.baseURL, c.botToken)

	payload := sendMessageReq{
		ChatID: chatID,
		Text:   text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal sendMessage request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create sendMessage request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("sendMessage request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sendMessage returned status %d", resp.StatusCode)
	}

	var result sendMessageResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode sendMessage response: %w", err)
	}

	if !result.OK {
		return fmt.Errorf("sendMessage returned ok=false")
	}

	return nil
}
