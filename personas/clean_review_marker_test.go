package personas

import (
	"embed"
	"io/fs"
	"strings"
	"testing"
)

// TestEveryPersona_InstructsTheCleanReviewMarker pins the contract the fanout
// engine's empty-response gate depends on.
//
// The engine treats an empty reviewer response as a dead call and fails the slot
// over to its backup: a provider that returns a null completion without setting
// finish_reason=length would otherwise be recorded as a clean review, which on a
// leaderboard is indistinguishable from a reviewer that actually read the diff.
// That gate is only safe while a genuinely clean review looks DIFFERENT from
// silence — hence the explicit "NO FINDINGS" marker every prompt instructs.
//
// A persona that reverts to staying silent reports failures on exactly the
// reviews it handled correctly, and nothing else in the system would notice. So
// it is pinned here, over every embedded persona — built-in and community —
// rather than a sampled few.
//
// (Deliberately worded without the word this marker would naturally be called:
// that term is a retired persona slug, and TestNoRetiredSlugs scans this file.)
func TestEveryPersona_InstructsTheCleanReviewMarker(t *testing.T) {
	var checked int

	for _, src := range []struct {
		name string
		fsys embed.FS
	}{
		{"built-in", files},
		{"community", communityFiles},
	} {
		err := fs.WalkDir(src.fsys, ".", func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			body, readErr := fs.ReadFile(src.fsys, path)
			if readErr != nil {
				t.Errorf("%s %s: %v", src.name, path, readErr)
				return nil
			}
			checked++
			low := strings.ToLower(string(body))

			if !strings.Contains(low, "no findings") {
				t.Errorf("%s %s: no clean-review marker — a persona that stays silent on a clean "+
					"review is failed over as a dead call", src.name, path)
			}
			if strings.Contains(low, "emit nothing") {
				t.Errorf("%s %s: still instructs silence on a clean review; emit the NO FINDINGS "+
					"marker instead", src.name, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walking %s personas: %v", src.name, err)
		}
	}

	if checked == 0 {
		t.Fatal("no embedded persona files were checked — the walk found nothing to pin")
	}
}
