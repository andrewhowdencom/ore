module github.com/andrewhowdencom/ore/x/provider/vercel

go 1.26.2

require (
	github.com/andrewhowdencom/ore v0.11.0
	github.com/andrewhowdencom/ore/x/wire/openai v0.5.0
	github.com/stretchr/testify v1.11.1
)

replace (
	github.com/andrewhowdencom/ore => ../../..
	github.com/andrewhowdencom/ore/x/wire/openai => ../../wire/openai
)
