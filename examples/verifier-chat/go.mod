module github.com/andrewhowdencom/ore/examples/verifier-chat

go 1.26.2

require (
	github.com/andrewhowdencom/ore v0.0.0
	github.com/andrewhowdencom/ore/x/provider/openai v0.0.0
	github.com/andrewhowdencom/ore/x/tool v0.0.0
	github.com/andrewhowdencom/ore/x/tool/filesystem v0.0.0
	github.com/andrewhowdencom/ore/x/verifier v0.0.0
)

replace github.com/andrewhowdencom/ore v0.0.0 => ../..

replace github.com/andrewhowdencom/ore/x/provider/openai v0.0.0 => ../../x/provider/openai

replace github.com/andrewhowdencom/ore/x/tool v0.0.0 => ../../x/tool

replace github.com/andrewhowdencom/ore/x/tool/filesystem v0.0.0 => ../../x/tool/filesystem

replace github.com/andrewhowdencom/ore/x/verifier v0.0.0 => ../../x/verifier
