package registry

import (
	"fmt"
	"sort"
	"strings"
)

// Fallback-graph node colors for DFS cycle detection. A gray→gray edge is a
// cycle; gray→black is a diamond (shared fallback target), which is legal.
type nodeColor int

const (
	white nodeColor = iota // unvisited
	gray                   // on the current DFS path
	black                  // fully explored
)

// ValidateFallbacks checks every agent's fallback chain at load time:
// dangling references and cycles (including self-references) are hard
// errors. Runs in O(V + E); each agent has at most one outgoing edge.
func (r *Registry) ValidateFallbacks() error {
	// Deterministic iteration so error messages are stable.
	names := make([]string, 0, len(r.Agents))
	for name := range r.Agents {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		fb := r.Agents[name].Fallback
		if fb == "" {
			continue
		}
		if _, ok := r.Agents[fb]; !ok {
			return fmt.Errorf("agent '%s' fallback references unknown agent '%s'", name, fb)
		}
	}

	color := map[string]nodeColor{}
	for _, name := range names {
		if color[name] != white {
			continue
		}
		if path := walkFallbacks(r, name, color); path != nil {
			return fmt.Errorf("fallback cycle detected: %s", strings.Join(path, " -> "))
		}
	}
	return nil
}

// walkFallbacks follows the (single) fallback edge from start, coloring
// nodes. It returns the full cycle path when one is found, nil otherwise.
func walkFallbacks(r *Registry, start string, color map[string]nodeColor) []string {
	var path []string
	node := start
	for {
		color[node] = gray
		path = append(path, node)

		next := r.Agents[node].Fallback
		if next == "" || color[next] == black {
			break
		}
		if color[next] == gray {
			// Close the loop for the error message: trim the lead-in so the
			// path starts at the repeated node.
			for i, n := range path {
				if n == next {
					return append(path[i:], next)
				}
			}
		}
		node = next
	}
	for _, n := range path {
		color[n] = black
	}
	return nil
}
