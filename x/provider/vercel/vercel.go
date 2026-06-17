package vercel

import (
	"github.com/andrewhowdencom/ore/provider"
	openaiwire "github.com/andrewhowdencom/ore/x/wire/openai"
)

// nameResolver looks up the canonical spec name in the generated
// lookup table and returns the Vercel AI Gateway wire name. On miss
// it returns the canonical name verbatim so callers can still
// request a model by its gateway id (e.g. for models that have not
// yet been added to the catalog).
func nameResolver(canonical string) string {
	if v, ok := nameLookup[canonical]; ok {
		return v
	}
	return canonical
}

// New constructs a Vercel AI Gateway provider backed by the OpenAI
// wire. The wire's base-URL inspection selects bearer-token auth
// automatically; the name-resolver is the only first-party
// customization.
//
// The returned value implements [provider.Provider] but is the
// wire's concrete *Provider type; callers should depend on the
// interface.
func New(apiKey string) (provider.Provider, error) {
	return openaiwire.New(
		openaiwire.WithAPIKey(apiKey),
		openaiwire.WithBaseURL(openaiBaseURL),
		openaiwire.WithNameResolver(nameResolver),
	)
}
