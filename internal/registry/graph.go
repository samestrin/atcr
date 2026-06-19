package registry

import (
	"errors"
	"fmt"
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
// dangling references and cycles (including self-references) are hard errors.
// Runs in O(V + E); each agent has at most one outgoing edge. It accumulates
// every dangling reference and every disjoint cycle and reports them together
// via errors.Join (Epic 4.2 / AC6) rather than returning the first found.
func (r *Registry) ValidateFallbacks() error {
	// Deterministic iteration so error messages are stable.
	names := sortedKeys(r.Agents)

	var errs []error

	for _, name := range names {
		fb := r.Agents[name].Fallback
		if fb == "" {
			continue
		}
		if _, ok := r.Agents[fb]; !ok {
			errs = append(errs, agentSentinelErr(name, ErrDanglingFallback,
				fmt.Sprintf("%s: agent '%s' fallback references unknown agent '%s'", ErrDanglingFallback, name, fb)))
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
			errs = append(errs, agentSentinelErr(attributed, ErrFallbackCycle,
				fmt.Sprintf("%s detected: %s", ErrFallbackCycle, strings.Join(path, " -> "))))
			// walkFallbacks blackens every node it visited (cycle + lead-in) on
			// detection, so the outer scan never re-walks a node left gray and
			// never trips the gray-not-on-path invariant. Nothing to do here.
		}
	}
	return errors.Join(errs...)
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
			// Trim the lead-in so the reported path starts at the repeated node.
			// Build the cycle into a fresh slice BEFORE blackening so it never
			// aliases path's backing array, then blacken EVERY node this walk
			// visited — lead-in nodes included, not just the trimmed cycle. Under
			// accumulation ValidateFallbacks keeps scanning, and a later root that
			// edges into a leftover-gray lead-in node would otherwise reach the
			// panic below. The single-outgoing-edge invariant makes this safe: a
			// lead-in node has one edge (into this cycle) and cannot start another
			// cycle, so marking it fully-explored loses no future detection.
			// Because ValidateFallbacks only walks white roots and colors nodes
			// gray on the current path, next is always in path — the loop cannot
			// complete without matching.
			for i, n := range path {
				if n == next {
					cycle := make([]string, 0, len(path)-i+1)
					cycle = append(cycle, path[i:]...)
					cycle = append(cycle, next)
					for _, visited := range path {
						color[visited] = black
					}
					return cycle, true
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
