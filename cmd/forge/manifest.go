package main

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// Manifest is the top-level forge configuration read from a YAML file.
type Manifest struct {
	Dist    Dist    `yaml:"dist"`
	Conduit Conduit `yaml:"conduit"`
}

// Dist describes the distribution (compiled binary) to produce.
type Dist struct {
	Name       string `yaml:"name"`
	OutputPath string `yaml:"output_path"`
}

// Conduit describes which ore conduit the generated agent should use.
type Conduit struct {
	Type string `yaml:"type"`
}

// ParseManifest reads and validates a forge manifest from r.
func ParseManifest(r io.Reader) (*Manifest, error) {
	var m Manifest
	dec := yaml.NewDecoder(r)
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}

	if m.Dist.Name == "" {
		return nil, fmt.Errorf("manifest dist.name is required")
	}
	if m.Dist.OutputPath == "" {
		return nil, fmt.Errorf("manifest dist.output_path is required")
	}
	if m.Conduit.Type != "http" && m.Conduit.Type != "tui" {
		return nil, fmt.Errorf("manifest conduit.type must be \"http\" or \"tui\", got %q", m.Conduit.Type)
	}

	return &m, nil
}
