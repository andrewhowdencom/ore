module github.com/andrewhowdencom/ore/examples/verifier-chat

go 1.26.2

require (
	github.com/andrewhowdencom/ore v0.11.0
	github.com/andrewhowdencom/ore/x/provider/openai v0.0.0
	github.com/andrewhowdencom/ore/x/tool v0.5.0
	github.com/andrewhowdencom/ore/x/tool/filesystem v0.0.0
	github.com/andrewhowdencom/ore/x/verifier v0.1.1
)

require (
	github.com/andrewhowdencom/ore/x/tool/truncate v0.1.0 // indirect
	github.com/andrewhowdencom/ore/x/wire/openai v0.5.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/openai/openai-go v1.12.0 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.69.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
)

replace (
	github.com/andrewhowdencom/ore => ../..
	github.com/andrewhowdencom/ore/x/provider/openai => ../../x/provider/openai
	github.com/andrewhowdencom/ore/x/tool => ../../x/tool
	github.com/andrewhowdencom/ore/x/tool/filesystem => ../../x/tool/filesystem
	github.com/andrewhowdencom/ore/x/verifier => ../../x/verifier
	github.com/andrewhowdencom/ore/x/wire/openai => ../../x/wire/openai
)
