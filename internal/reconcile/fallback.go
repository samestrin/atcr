package reconcile

// Fallback-provenance de-weighting (Epic 19.10 F5). When a reviewer slot overflows
// its context window it can be routed to a litellm fallback model that may be
// shared across personas; two personas silently served by the same net model are
// NOT two independent voices. This file stamps that provenance onto the merged
// JSONFinding records (post-merge, mirroring validateFindingPaths) and collapses
// reviewers sharing a fallback target when counting distinct reviewers.
//
// The provenance never touches the extracted library types (reclib.Finding /
// reclib.Merged) — it lives only on ATCR-internal stream.Finding.FallbackModel
// (stamped at discovery from status.json) and JSONFinding.FallbackReviewers,
// exactly as PathValid/PathWarning do. The collapse key is the SERVED model, so
// two personas backed by the same net model collapse to one voice.

// stampFallbackProvenance records, per merged finding, which of its reviewers were
// served by a fallback model. It runs over the JSONFinding records (the extracted
// library Merged carries no ATCR-only provenance) and populates the FallbackReviewers
// side field, which the distinct-reviewer independence count reads.
//
// The per-reviewer provenance comes from the discovered sources, whose
// stream.Finding.FallbackModel was stamped at discovery time from each source's
// sibling status.json. Fallback is a whole-slot property, so every finding a
// reviewer produced shares the same FallbackModel; the reviewer→served-model map is
// built once. A reviewer that ran on its own configured model (empty FallbackModel)
// is left out of the map — fail-closed, counted as an independent voice.
func stampFallbackProvenance(findings []JSONFinding, sources []Source) {
	reviewerFallback := map[string]string{}
	for _, s := range sources {
		for _, f := range s.Findings {
			if f.FallbackModel != "" && f.Reviewer != "" {
				reviewerFallback[f.Reviewer] = f.FallbackModel
			}
		}
	}
	if len(reviewerFallback) == 0 {
		return
	}
	for i := range findings {
		var fr map[string]string
		for _, rev := range findings[i].Reviewers {
			if target := reviewerFallback[rev]; target != "" {
				if fr == nil {
					fr = make(map[string]string)
				}
				fr[rev] = target
			}
		}
		if fr != nil {
			findings[i].FallbackReviewers = fr
		}
	}
}

// distinctReviewerCount collapses reviewers served by the SAME fallback model into
// a single independent voice (Epic 19.10 F5). A reviewer with an empty (or absent)
// fallback target ran on its own configured model and always counts individually;
// two reviewers sharing a non-empty fallback target — one net model backing
// multiple personas — collapse to one voice, so the substitution does not inflate
// the distinct-reviewer independence score. With no fallback data the count is
// exactly len(reviewers), preserving pre-19.10 behavior.
func distinctReviewerCount(reviewers []string, fallback map[string]string) int {
	if len(fallback) == 0 {
		return len(reviewers)
	}
	distinct := 0
	seenTarget := map[string]bool{}
	for _, r := range reviewers {
		target := fallback[r]
		if target == "" {
			distinct++ // own configured model — an independent voice
			continue
		}
		if seenTarget[target] {
			continue // a persona already counted for this shared fallback model
		}
		seenTarget[target] = true
		distinct++
	}
	return distinct
}
