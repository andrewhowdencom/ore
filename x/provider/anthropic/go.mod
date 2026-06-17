module github.com/andrewhowdencom/ore/x/provider/anthropic

go 1.26.2

require (
	github.com/andrewhowdencom/ore v0.11.0
	github.com/andrewhowdencom/ore/x/wire/anthropic v0.5.0
	github.com/stretchr/testify v1.11.1
)

replace (
	github.com/andrewhowdencom/ore => ../../..
	github.com/andrewhowdencom/ore/x/wire/anthropic => ../../wire/anthropic
)
