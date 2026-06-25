// Package personas ships the embedded default reviewer personas (six
// generalists plus three domain bonus personas) and the shared base template.
// `atcr init` installs editable copies; the prompt resolution chain falls back
// to these embedded versions when no file overrides them.
package personas

import (
	"embed"
	"fmt"
)

//go:embed *.md
var files embed.FS

//go:embed testdata/*.patch
var testfiles embed.FS

// canonical order: generalists first, then the three domain bonus personas
// (security, performance, Go idioms), with the style reviewer last.
var names = []string{"bruce", "greta", "kai", "mira", "dax", "sentinel", "tracer", "idiomatic", "otto"}

// expectedEmbeddedFiles returns the set of .md files that must be present in the
// embedded persona directory: one per registered persona plus the shared base.
func expectedEmbeddedFiles() map[string]struct{} {
	want := make(map[string]struct{}, len(names)+1)
	for _, n := range names {
		want[n+".md"] = struct{}{}
	}
	want["_base.md"] = struct{}{}
	return want
}

func init() {
	want := expectedEmbeddedFiles()
	entries, err := files.ReadDir(".")
	if err != nil {
		panic(fmt.Sprintf("personas: failed to read embedded files: %v", err))
	}
	got := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		got[e.Name()] = struct{}{}
	}
	if len(got) != len(want) {
		panic(fmt.Sprintf("personas: embedded .md files %v do not match registered personas plus _base.md %v", got, want))
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			panic(fmt.Sprintf("personas: embedded .md files %v do not match registered personas plus _base.md %v", got, want))
		}
	}
}

// Names returns the embedded persona names in canonical order.
func Names() []string {
	out := make([]string, len(names))
	copy(out, names)
	return out
}

// isRegistered reports whether name is one of the canonical persona names.
func isRegistered(name string) bool {
	for _, n := range names {
		if n == name {
			return true
		}
	}
	return false
}

// Get returns the embedded persona template for name. Only the canonical
// registered personas resolve; the shared base template is served by Base, not
// Get, so the registry stays the single source of truth for persona identity.
func Get(name string) (string, error) {
	if !isRegistered(name) {
		return "", fmt.Errorf("unknown persona %q: not a registered persona", name)
	}
	data, err := files.ReadFile(name + ".md")
	if err != nil {
		return "", fmt.Errorf("internal error: embedded persona %s not found: %w", name, err)
	}
	return string(data), nil
}

// Fixture returns the embedded patch fixture for name. Only the three bonus
// personas (idiomatic, sentinel, tracer) ship fixtures; other built-ins return
// an error — callers treat that as HasFixture: false.
func Fixture(name string) (string, error) {
	data, err := testfiles.ReadFile("testdata/" + name + "_fixture.patch")
	if err != nil {
		return "", fmt.Errorf("no embedded fixture for persona %q", name)
	}
	return string(data), nil
}

// Base returns the shared base persona template (_base.md).
func Base() (string, error) {
	data, err := files.ReadFile("_base.md")
	if err != nil {
		return "", fmt.Errorf("internal error: embedded persona _base not found: %w", err)
	}
	return string(data), nil
}
