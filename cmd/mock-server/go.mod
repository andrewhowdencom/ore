module github.com/andrewhowdencom/ore/cmd/mock-server

go 1.26.2

require (
	github.com/andrewhowdencom/ore/x/provider/mock v0.1.0
	github.com/andrewhowdencom/ore/x/provider/mock/anthropic v0.1.0
	github.com/andrewhowdencom/ore/x/provider/mock/openai v0.1.0
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/andrewhowdencom/ore => ../..

replace github.com/andrewhowdencom/ore/x/provider/mock => ../../x/provider/mock

replace github.com/andrewhowdencom/ore/x/provider/mock/anthropic => ../../x/provider/mock/anthropic

replace github.com/andrewhowdencom/ore/x/provider/mock/openai => ../../x/provider/mock/openai
