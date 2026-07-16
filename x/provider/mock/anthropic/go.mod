module github.com/andrewhowdencom/ore/x/provider/mock/anthropic

go 1.26.2

require (
	github.com/andrewhowdencom/ore v1.2.3
	github.com/andrewhowdencom/ore/x/provider/anthropic v0.2.5
	github.com/andrewhowdencom/ore/x/provider/mock v0.1.0
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/andrewhowdencom/ore/x/provider/retry v0.0.3 // indirect
	github.com/andrewhowdencom/ore/x/wire/anthropic v0.2.2 // indirect
	github.com/anthropics/anthropic-sdk-go v1.50.1 // indirect
	github.com/bahlo/generic-list-go v0.2.0 // indirect
	github.com/buger/jsonparser v1.1.2 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/invopop/jsonschema v0.14.0 // indirect
	github.com/pb33f/ordered-map/v2 v2.3.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/standard-webhooks/standard-webhooks/libraries v0.0.1 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.69.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.yaml.in/yaml/v4 v4.0.0-rc.2 // indirect
	golang.org/x/sync v0.20.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/andrewhowdencom/ore => ../../../..

replace github.com/andrewhowdencom/ore/x/provider/mock => ..
