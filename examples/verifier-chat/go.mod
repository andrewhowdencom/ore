module github.com/andrewhowdencom/ore/examples/verifier-chat

go 1.26.2

require (
	github.com/andrewhowdencom/ore v0.7.0
	github.com/andrewhowdencom/ore/x/provider/openai v0.4.3
	github.com/andrewhowdencom/ore/x/tool v0.4.3
	github.com/andrewhowdencom/ore/x/tool/filesystem v0.4.2
	github.com/andrewhowdencom/ore/x/verifier v0.1.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/openai/openai-go v1.12.0 // indirect
	github.com/tidwall/gjson v1.14.4 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.69.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
)

replace github.com/andrewhowdencom/ore v0.0.0 => ../..

replace github.com/andrewhowdencom/ore/x/provider/openai v0.0.0 => ../../x/provider/openai

replace github.com/andrewhowdencom/ore/x/tool v0.0.0 => ../../x/tool

replace github.com/andrewhowdencom/ore/x/tool/filesystem v0.0.0 => ../../x/tool/filesystem

replace github.com/andrewhowdencom/ore/x/verifier v0.0.0 => ../../x/verifier
