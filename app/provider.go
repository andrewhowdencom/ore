package app

import (
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/provider/openai"
)

func defaultProviderFactory(apiKey, model, baseURL string) (provider.Provider, error) {
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	return openai.New(apiKey, model, opts...), nil
}
