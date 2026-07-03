module github.com/andrewhowdencom/ore/x/export

go 1.26.2

require github.com/andrewhowdencom/ore v1.0.0

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
)

replace github.com/andrewhowdencom/ore => ../..
