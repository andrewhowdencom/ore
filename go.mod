module github.com/andrewhowdencom/ore

go 1.26.2

require github.com/stretchr/testify v1.11.1

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/kr/pretty v0.3.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/rogpeppe/go-internal v1.14.1 // indirect
	gopkg.in/check.v1 v1.0.0-20201130134442-10cb98267c6c // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/andrewhowdencom/ore/x/conduit/http => ./x/conduit/http

replace github.com/andrewhowdencom/ore/x/conduit/tui => ./x/conduit/tui

replace github.com/andrewhowdencom/ore/x/tool/calculator => ./x/tool/calculator

replace github.com/andrewhowdencom/ore/x/provider/openai => ./x/provider/openai

replace github.com/andrewhowdencom/ore/x/tool/bash => ./x/tool/bash

replace github.com/andrewhowdencom/ore/x/tool/filesystem => ./x/tool/filesystem

replace github.com/andrewhowdencom/ore/x/tool/skills => ./x/tool/skills
