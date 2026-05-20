package main

import (
	"fmt"
	"io"
	"strings"

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
// Optional fields:
//   - handlers: artifact handler modules to wire into each stream's loop.Step
//
// Required fields:
//   - dist.name: binary name used in go.mod and as the default output file name
//   - dist.output_path: destination path for the compiled binary (relative paths
//     are resolved against the current working directory)
//   - conduits: must contain at least one entry with a non-empty module path
type Blueprint struct {
	Dist     Dist            `yaml:"dist"`
	Conduits   []ConduitConfig   `yaml:"conduits"`
	Handlers   []HandlerConfig   `yaml:"handlers,omitempty"`
	Transforms []TransformConfig `yaml:"transforms,omitempty"`
}

// Dist describes the distribution (compiled binary) to produce.
type Dist struct {
	Name       string `yaml:"name"`
	OutputPath string `yaml:"output_path"`
}

// ConduitConfig describes a single conduit to include in the generated agent.
type ConduitConfig struct {
	Name    string         `yaml:"name,omitempty"`
	Module  string         `yaml:"module"`
	Options map[string]any `yaml:"options,omitempty"`
}

// HandlerConfig describes a single artifact handler to include in the
// generated agent. Handlers are instantiated per-stream and wired into
// loop.Step via loop.WithHandlers.
type HandlerConfig struct {
	Name    string         `yaml:"name,omitempty"`
	Module  string         `yaml:"module"`
	Options map[string]any `yaml:"options,omitempty"`
}

// TransformConfig describes a single inference assembly transform to include
// in the generated agent. Transforms are instantiated per-stream and wired
// into loop.Step via loop.WithTransforms.
type TransformConfig struct {
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
	for i, h := range b.Handlers {
		if h.Module == "" {
			return nil, fmt.Errorf("blueprint handlers[%d].module is required", i)
		}
	}
	for i, tr := range b.Transforms {
		if tr.Module == "" {
			return nil, fmt.Errorf("blueprint transforms[%d].module is required", i)
		}
	}

	if err := deriveAndValidateNames(&b); err != nil {
		return nil, err
	}

	return &b, nil
}

// deriveAndValidateNames fills in empty Name fields from the last path element
// of Module and validates that all names are unique across conduits and
// handlers.
func deriveAndValidateNames(b *Blueprint) error {
	used := make(map[string]struct{})

	// First, reserve all explicit names and check for duplicates among them.
	for _, c := range b.Conduits {
		if c.Name != "" {
			if _, ok := used[c.Name]; ok {
				return fmt.Errorf("duplicate conduit/handler name: %s", c.Name)
			}
			used[c.Name] = struct{}{}
		}
	}
	for _, h := range b.Handlers {
		if h.Name != "" {
			if _, ok := used[h.Name]; ok {
				return fmt.Errorf("duplicate conduit/handler name: %s", h.Name)
			}
			used[h.Name] = struct{}{}
		}
	}

	// Then derive names for empty entries, avoiding collisions with reserved names.
	for i := range b.Conduits {
		if b.Conduits[i].Name == "" {
			b.Conduits[i].Name = deriveName(b.Conduits[i].Module, used)
			used[b.Conduits[i].Name] = struct{}{}
		}
	}
	for i := range b.Handlers {
		if b.Handlers[i].Name == "" {
			b.Handlers[i].Name = deriveName(b.Handlers[i].Module, used)
			used[b.Handlers[i].Name] = struct{}{}
		}
	}

	return nil
}

// deriveName returns a unique name derived from the last path element of module.
// If the base name is already in used, a numeric suffix is appended.
func deriveName(module string, used map[string]struct{}) string {
	parts := strings.Split(module, "/")
	name := parts[len(parts)-1]

	base := name
	for i := 1; ; i++ {
		if _, ok := used[name]; !ok {
			return name
		}
		name = fmt.Sprintf("%s%d", base, i)
	}
}
