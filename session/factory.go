package session

import (
	"strconv"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"go.opentelemetry.io/otel/trace"
)

// Model metadata keys written by slash commands (e.g. x/tool/set_model)
// and read by the factory to derive a per-turn model spec.
//
// These constants are the framework contract. Slash handlers write
// under these keys; the factory reads them and translates the
// metadata into a models.Spec. They are duplicated in junk/stream.go
// for backwards compatibility; that duplication is removed in Task 10.
const (
	MetadataKeyModelName            = "ore.model.name"
	MetadataKeyModelThinkingLevel   = "ore.model.thinking_level"
	MetadataKeyModelTemperature     = "ore.model.temperature"
	MetadataKeyModelMaxOutputTokens = "ore.model.max_output_tokens"
)

// AgentFactory builds an *agent.Agent for a given Session. The factory
// reads session.Metadata and other session state to construct the
// agent's spec, transforms, and other per-turn configuration.
//
// The factory is invoked by Runner on every cognitive-pattern invocation;
// it is the canonical place where "session configuration influences
// agent design" is expressed.
type AgentFactory interface {
	Build(sess *Session) (*agent.Agent, error)
}

// DefaultFactory builds an agent from a Session's metadata, using the
// factory's configured Provider, Pattern, and Tracer. It is the
// reference implementation of AgentFactory.
//
// Path B (locked in): the agent continues to construct its own
// internal *loop.Step at agent.New time. Long-term subscribers
// attach to the session's step (via Session.Subscribe), which is
// distinct from the agent's step. Re-routing the agent's events
// through the session's step requires an agent.WithStep option
// (or equivalent), which is tracked separately as #523. Until
// that lands, the agent's internal step is unused for fanout —
// subscribers read events from the session's step directly,
// which receives lifecycle events emitted by the Runner and
// any PropertiesEvents emitted by Session.SetMetadata.
type DefaultFactory struct {
	Provider provider.Provider
	Pattern  cognitive.Pattern
	Tracer   trace.Tracer
}

// NewDefaultFactory constructs a DefaultFactory.
func NewDefaultFactory(p provider.Provider, pat cognitive.Pattern, tr trace.Tracer) *DefaultFactory {
	return &DefaultFactory{
		Provider: p,
		Pattern:  pat,
		Tracer:   tr,
	}
}

// Build constructs an *agent.Agent for the given session. It reads the
// session's metadata to derive a model spec, then constructs the agent
// with the factory's configured provider, pattern, and tracer.
//
// The session is bound as the agent's default state (for the auto-append
// behavior of TurnCompleteEvent). Per-call overrides by the pattern
// still win over the bound spec.
func (f *DefaultFactory) Build(sess *Session) (*agent.Agent, error) {
	spec := specFromMetadata(sess.AllMetadata())

	return agent.New(sess.ID(),
		agent.WithProvider(f.Provider),
		agent.WithSpec(spec),
		agent.WithPattern(f.Pattern),
		agent.WithTracer(f.Tracer),
		agent.WithState(sess.Thread()),
	), nil
}

// specFromMetadata derives a models.Spec from a metadata map. It returns
// a zero-valued Spec and false when no recognized metadata keys are
// set; callers should fall back to the step's default in that case.
//
// Recognized keys:
//
//	"ore.model.name"              → Spec.Name
//	"ore.model.thinking_level"    → Spec.ThinkingLevel
//	"ore.model.temperature"       → Spec.Temperature (parsed float)
//	"ore.model.max_output_tokens" → Spec.MaxOutputTokens (parsed int)
//
// Unknown keys are ignored. The slash command in x/tool/set_model is
// the canonical writer.
func specFromMetadata(meta map[string]string) models.Spec {
	name, hasName := meta[MetadataKeyModelName]
	if !hasName || name == "" {
		return models.Spec{}
	}
	spec := models.Spec{Name: name}

	if level, ok := meta[MetadataKeyModelThinkingLevel]; ok && level != "" {
		spec.ThinkingLevel = models.ThinkingLevel(level)
	}

	if tempStr, ok := meta[MetadataKeyModelTemperature]; ok && tempStr != "" {
		if t, err := strconv.ParseFloat(tempStr, 64); err == nil {
			spec.Temperature = &t
		}
	}

	if maxStr, ok := meta[MetadataKeyModelMaxOutputTokens]; ok && maxStr != "" {
		if n, err := strconv.ParseInt(maxStr, 10, 64); err == nil {
			spec.MaxOutputTokens = n
		}
	}

	return spec
}