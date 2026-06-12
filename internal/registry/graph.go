package registry

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Sentinel errors so callers can discriminate fallback-validation failures
// without string matching.
var (
	ErrDanglingFallback = errors.New("dangling fallback reference")
	ErrFallbackCycle    = errors.New("fallback cycle")
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
			return agentSentinelErr(name, ErrDanglingFallback,
				fmt.Sprintf("%s: agent '%s' fallback references unknown agent '%s'", ErrDanglingFallback, name, fb))
		}
	}

	color := map[string]nodeColor{}
	for _, name := range names {
		if color[name] != white {
			continue
		}
		if path, found := r.walkFallbacks(name, color); found {
			// Prefer a project-tier node for attribution so errors name
			// .atcr/registry.yaml when the cycle spans tiers.
			attributed := path[0]
			for _, n := range path {
				if r.AgentTier(n) == SourceProject {
					attributed = n
					break
				}
			}
			return agentSentinelErr(attributed, ErrFallbackCycle,
				fmt.Sprintf("%s detected: %s", ErrFallbackCycle, strings.Join(path, " -> ")))
		}
	}
	return nil
}

// walkFallbacks follows the (single) fallback edge from start, coloring
// nodes. It reports the full cycle path when one is found.
func (r *Registry) walkFallbacks(start string, color map[string]nodeColor) ([]string, bool) {
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
			// path starts at the repeated node. Because ValidateFallbacks only
			// walks white roots and colors nodes gray on the current path, next
			// is always in path — the loop below cannot complete without matching.
			for i, n := range path {
				if n == next {
					return append(path[i:], next), true
				}
			}
			// Unreachable: next is gray, hence already on the current path.
			panic(fmt.Sprintf("walkFallbacks: invariant violation — gray node %q not found on current path %v", next, path))
		}
		node = next
	}
	for _, n := range path {
		color[n] = black
	}
	return nil, false
}
