package registry

import (
	"path/filepath"
	"sort"

	"github.com/samestrin/atcr/personas"
)

// basePersonaFile is the shared base template's file name, both on disk under
// `.atcr/personas/` and in the embedded set.
const basePersonaFile = "_base.md"

// StalePersona is one built-in persona copy under the project personas
// directory that has fallen behind its embedded counterpart (Epic 35.16.16).
type StalePersona struct {
	// File is the copy's base name ("bruce.md", "_base.md") — the stamp key and
	// the name an operator looks for under `.atcr/personas/`.
	File string
	// Path is the resolved source path, as ResolvePersona reported it.
	Path string
	// Agents are the roster agents whose persona resolves to this file, sorted.
	// More than one agent can share a file: every roster agent without its own
	// `<agent>.md` resolves to the project `_base.md`.
	Agents []string
	// Edited is true when the file also differs from what init wrote (its stamp
	// entry), so a refresh would overwrite a hand edit.
	Edited bool
	// Stamped is false when the file has no stamp entry and the verdict came from
	// the PredicateRuleGaps fallback; Edited is then unknown and reported false.
	Stamped bool
}

// StalePersonas resolves each roster agent's persona through the standard chain
// and returns, sorted by file name, every built-in persona copy under
// dirs.Project that has fallen behind the embedded content.
//
// Only a persona whose resolved source is a built-in's own file in the project
// directory is considered: `<agent>.md` for a registered built-in name, or
// `_base.md`. Community (Registry-tier) personas, explicit refs (a persona name
// other than the agent's), embedded-source personas and personas that fail to
// resolve have no installed copy to fall behind, and are never reported.
//
// With a stamp entry for the file, three hashes decide it — the embedded
// content's, the stamp's (what init wrote) and the file's own:
//
//   - stale when the embedded hash differs from both the stamp's and the file's
//     (the binary moved on, and the copy was not brought along);
//   - edited when the file's hash differs from the stamp's (an operator changed
//     it after init). An edited copy is reported only when it is also stale.
//
// With no stamp entry (a workspace initialized before the stamp existed, or a
// copy the operator added by hand) there is nothing to compare against, so the
// verdict defers to PredicateRuleGaps: the copy is stale when it lacks the
// panel-wide predicate rule. A copy identical to the embedded content is current
// either way. This keeps a stamp-less workspace from reporting every persona
// stale without cause.
func StalePersonas(agentToPersona map[string]string, dirs PersonaDirs) []StalePersona {
	if dirs.Project == "" {
		return nil
	}
	stamp, _ := ReadPersonaStamp(dirs.Project)

	var ruleGaps map[string]bool // lazily filled: only stamp-less copies need it
	byFile := map[string]*StalePersona{}
	for agent, personaRef := range agentToPersona {
		if personaRef != agent {
			continue // explicit ref: no built-in counterpart to compare against
		}
		p, err := ResolvePersona(agent, personaRef, nil, dirs)
		if err != nil {
			continue // PredicateRuleGaps reports it as unresolved
		}
		file, embedded, ok := builtinCopy(p.Source, agent, dirs.Project)
		if !ok {
			continue
		}
		fileHash := HashPersonaContent([]byte(p.Text))
		embeddedHash := HashPersonaContent([]byte(embedded))
		if fileHash == embeddedHash {
			continue
		}

		stampHash, stamped := stamp.Files[file]
		var edited bool
		if stamped {
			if embeddedHash == stampHash {
				continue // init wrote the current content; any drift is a hand edit
			}
			edited = fileHash != stampHash
		} else {
			if ruleGaps == nil {
				gaps, _ := PredicateRuleGaps(agentToPersona, dirs)
				ruleGaps = make(map[string]bool, len(gaps))
				for _, g := range gaps {
					ruleGaps[g] = true
				}
			}
			if !ruleGaps[agent] {
				continue
			}
		}

		sp := byFile[file]
		if sp == nil {
			sp = &StalePersona{File: file, Path: p.Source, Edited: edited, Stamped: stamped}
			byFile[file] = sp
		}
		sp.Agents = append(sp.Agents, agent)
	}

	out := make([]StalePersona, 0, len(byFile))
	for _, sp := range byFile {
		sort.Strings(sp.Agents)
		out = append(out, *sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].File < out[j].File })
	return out
}

// builtinCopy reports whether source, a resolved persona's source path, is a
// built-in's file directly under projectDir — `<agent>.md` for a registered
// built-in, or `_base.md` — and returns its base name and the embedded content
// it was copied from.
func builtinCopy(source, agent, projectDir string) (file, embedded string, ok bool) {
	dir := filepath.Clean(projectDir)
	switch filepath.Clean(source) {
	case filepath.Join(dir, agent+".md"):
		text, err := personas.Get(agent)
		if err != nil {
			return "", "", false // not a built-in name: a project-only persona
		}
		return agent + ".md", text, true
	case filepath.Join(dir, basePersonaFile):
		text, err := personas.Base()
		if err != nil {
			return "", "", false
		}
		return basePersonaFile, text, true
	}
	return "", "", false
}
