package models

import "fmt"

// ThinkingLevel is a portable, qualitative description of how much
// reasoning effort a model should spend on a turn. Adapters translate
// the level into their provider's wire format at request time.
//
// Levels are case-sensitive lowercase strings. The level is the user's
// intent; the adapter is the translator. The empty string is not a
// valid level — callers should substitute their own default (commonly
// ThinkingLevelOff) before calling ParseThinkingLevel.
//
// This type was moved from the provider package so that Spec can
// carry it as a field without creating a cycle (the provider package
// depends on models for the Spec type).
type ThinkingLevel string

const (
	// ThinkingLevelOff disables extended thinking. Adapters must not
	// send a `thinking` field (or equivalent) when this level is
	// requested; the request is identical to a non-thinking request.
	ThinkingLevelOff ThinkingLevel = "off"

	// ThinkingLevelMinimal asks for the smallest amount of thinking
	// the provider supports. Useful as a low-cost pipeline probe.
	ThinkingLevelMinimal ThinkingLevel = "minimal"

	// ThinkingLevelLow asks for a small amount of thinking.
	ThinkingLevelLow ThinkingLevel = "low"

	// ThinkingLevelMedium asks for a moderate amount of thinking.
	// Recommended default for reasoning-capable models when the
	// application or user opts in.
	ThinkingLevelMedium ThinkingLevel = "medium"

	// ThinkingLevelHigh asks for a substantial amount of thinking.
	ThinkingLevelHigh ThinkingLevel = "high"

	// ThinkingLevelMax asks for the maximum amount of thinking the
	// provider allows, while still leaving room for the visible
	// response. Adapters may clamp this to their maximum.
	ThinkingLevelMax ThinkingLevel = "max"
)

// Valid reports whether the level is one of the defined constants.
// The empty string is not valid.
func (l ThinkingLevel) Valid() bool {
	switch l {
	case ThinkingLevelOff, ThinkingLevelMinimal, ThinkingLevelLow,
		ThinkingLevelMedium, ThinkingLevelHigh, ThinkingLevelMax:
		return true
	}
	return false
}

// ParseThinkingLevel parses a string into a ThinkingLevel. The empty
// string is treated as a parse error — callers should substitute their
// own default (commonly ThinkingLevelOff) before calling. Levels are
// case-sensitive lowercase.
func ParseThinkingLevel(s string) (ThinkingLevel, error) {
	l := ThinkingLevel(s)
	if !l.Valid() {
		return "", fmt.Errorf("invalid thinking level %q: must be one of off, minimal, low, medium, high, max", s)
	}
	return l, nil
}
