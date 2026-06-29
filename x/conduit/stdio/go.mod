module github.com/andrewhowdencom/ore/x/conduit/stdio

go 1.26.2

replace github.com/andrewhowdencom/ore => ../../..

replace github.com/andrewhowdencom/ore/x/conduit => ..

require (
	github.com/andrewhowdencom/ore v0.13.0
	github.com/andrewhowdencom/ore/x/conduit v0.1.5
	github.com/stretchr/testify v1.11.1
	go.opentelemetry.io/otel/trace v1.44.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
