package main

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// Blueprint is the top-level forge configuration read from a YAML file.
//
// A minimal blueprint looks like:
//
//	dist:
//	  name: my-agent
//	  output_path: ./my-agent
//	conduits:
//	  - module: github.com/andrewhowdencom/ore/x/conduit/http
//
// Required fields:
//   - dist.name: binary name used in go.mod and as the default output file name
//   - dist.output_path: destination path for the compiled binary (relative paths
//     are resolved against the current working directory)
//   - conduits: must contain at least one entry with a non-empty module path
type Blueprint struct {
	Dist     Dist            `yaml:"dist"`
	Conduits []ConduitConfig `yaml:"conduits"`
}

// Dist describes the distribution (compiled binary) to produce.
type Dist struct {
	Name       string `yaml:"name"`
	OutputPath string `yaml:"output_path"`
}

// ConduitConfig describes a single conduit to include in the generated agent.
type ConduitConfig struct {
	Module  string         `yaml:"module"`
	Options map[string]any `yaml:"options,omitempty"`
}

// ParseBlueprint reads and validates a forge blueprint from r.
func ParseBlueprint(r io.Reader) (*Blueprint, error) {
	var b Blueprint
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&b); err != nil {
		return nil, fmt.Errorf("decode blueprint: %w", err)
	}

	if b.Dist.Name == "" {
		return nil, fmt.Errorf("blueprint dist.name is required")
	}
	if b.Dist.OutputPath == "" {
		return nil, fmt.Errorf("blueprint dist.output_path is required")
	}
	if len(b.Conduits) == 0 {
		return nil, fmt.Errorf("blueprint conduits must contain at least one entry")
	}
	for i, c := range b.Conduits {
		if c.Module == "" {
			return nil, fmt.Errorf("blueprint conduits[%d].module is required", i)
		}
	}

	return &b, nil
}
