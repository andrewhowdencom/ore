package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// buildDependencyGraph reads every module's go.mod and records which ore
// modules it depends on.
func buildDependencyGraph(root string, modules []Module) (map[string][]string, error) {
	graph := make(map[string][]string)
	for _, m := range modules {
		gomodPath := filepath.Join(root, m.Dir, "go.mod")
		data, err := os.ReadFile(gomodPath)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", gomodPath, err)
		}
		f, err := modfile.Parse(gomodPath, data, nil)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", gomodPath, err)
		}
		for _, r := range f.Require {
			if strings.HasPrefix(r.Mod.Path, "github.com/andrewhowdencom/ore") {
				graph[m.Path] = append(graph[m.Path], r.Mod.Path)
			}
		}
	}
	return graph, nil
}

// topologicalSort returns the given modules in dependency order so that every
// dependency is released before the modules that consume it.  It uses Kahn's
// algorithm on the reversed dependency graph.
func topologicalSort(modules []Module, graph map[string][]string) ([]Module, error) {
	moduleSet := make(map[string]bool)
	for _, m := range modules {
		moduleSet[m.Path] = true
	}

	// Build reversed adjacency list: edge from dependency → dependent.
	adj := make(map[string][]string)
	for _, m := range modules {
		for _, dep := range graph[m.Path] {
			if moduleSet[dep] {
				adj[dep] = append(adj[dep], m.Path)
			}
		}
	}

	inDegree := make(map[string]int)
	for _, m := range modules {
		inDegree[m.Path] = 0
	}
	for _, successors := range adj {
		for _, succ := range successors {
			inDegree[succ]++
		}
	}

	var queue []string
	for _, m := range modules {
		if inDegree[m.Path] == 0 {
			queue = append(queue, m.Path)
		}
	}

	var sorted []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		sorted = append(sorted, node)

		for _, succ := range adj[node] {
			inDegree[succ]--
			if inDegree[succ] == 0 {
				queue = append(queue, succ)
			}
		}
	}

	if len(sorted) != len(modules) {
		return nil, fmt.Errorf("dependency cycle detected among %d modules", len(modules))
	}

	pathToModule := make(map[string]Module)
	for _, m := range modules {
		pathToModule[m.Path] = m
	}

	result := make([]Module, len(sorted))
	for i, path := range sorted {
		result[i] = pathToModule[path]
	}
	return result, nil
}
