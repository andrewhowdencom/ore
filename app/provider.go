package app

import (
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/provider/openai"
	"github.com/andrewhowdencom/ore/thread"
)

func defaultProviderFactory(apiKey, model, baseURL string) (provider.Provider, error) {
	var opts []openai.Option
	if baseURL != "" {
		opts = append(opts, openai.WithBaseURL(baseURL))
	}
	return openai.New(apiKey, model, opts...), nil
}

func defaultStoreFactory(dir string) (thread.Store, error) {
	if dir == "" {
		return thread.NewMemoryStore(), nil
	}
	return thread.NewJSONStore(dir)
}
