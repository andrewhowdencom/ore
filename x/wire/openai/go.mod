module github.com/andrewhowdencom/ore/x/wire/openai

go 1.26.2

require (
	github.com/andrewhowdencom/ore v0.11.0
	github.com/andrewhowdencom/ore/x/provider/retry v0.0.0
	github.com/andrewhowdencom/ore/x/tool v0.5.0
	github.com/openai/openai-go v1.12.0
	github.com/stretchr/testify v1.11.1
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.69.0
	go.opentelemetry.io/otel v1.44.0
	go.opentelemetry.io/otel/sdk v1.44.0
	go.opentelemetry.io/otel/trace v1.44.0
)

replace (
	github.com/andrewhowdencom/ore => ../../../
	github.com/andrewhowdencom/ore/x/provider/retry => ../retry
)

require (
	github.com/andrewhowdencom/ore/x/tool/truncate v0.1.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/tidwall/gjson v1.14.4 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/andrewhowdencom/ore => ../../..

replace github.com/andrewhowdencom/ore/x/tool => ../../tool
