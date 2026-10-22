package reconcile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/samestrin/atcr/internal/atomicfs"
)

// IgnoreFileName is the local feedback-loop rule file (Epic 40.0), read from
// the reviewed checkout's .atcr/ directory.
const IgnoreFileName = "ignore.yaml"

// IgnoreRule is one `.atcr/ignore.yaml` entry: a reviewed false positive the
// reconcile phase drops from the primary stream.
//
// No line number is stored, on purpose: a finding's line drifts with every
// edit above it (and its history.FindingID with it), so a rule keys on where
// the finding lives and what it says instead. Match runs against the Problem
// text, which already carries the "(symbol) " anchor stampSymbolAnchors
// prefixes, so a match quoted from one finding keeps hitting that symbol after
// the line moves.
type IgnoreRule struct {
	// File is a repo-relative, slash-separated path or glob.
	File string `yaml:"file,omitempty" json:"file,omitempty"`
	// Package is a history.PackageOf directory ("." for the repo root).
	Package string `yaml:"package,omitempty" json:"package,omitempty"`
	// Category, when set, narrows the rule to one finding category.
	Category string `yaml:"category,omitempty" json:"category,omitempty"`
	// Match, when set, is an RE2 regex tested against the Problem text.
	Match string `yaml:"match,omitempty" json:"match,omitempty"`
	// Reason is mandatory: report.md lists it beside the suppressed count, so
	// a rule nobody can justify cannot hide a finding silently.
	Reason string `yaml:"reason" json:"reason"`
}

// ignoreFile is the on-disk document shape.
type ignoreFile struct {
	Rules []IgnoreRule `yaml:"rules"`
}

// label names a rule in an error by its position and its most specific field,
// so a user with many rules can find the rejected one without bisecting.
func (r IgnoreRule) label(i int) string {
	for _, f := range []struct{ key, val string }{
		{"file", r.File}, {"package", r.Package}, {"match", r.Match}, {"category", r.Category},
	} {
		if strings.TrimSpace(f.val) != "" {
			return fmt.Sprintf("ignore rule %d (%s %q)", i+1, f.key, f.val)
		}
	}
	return fmt.Sprintf("ignore rule %d", i+1)
}

// validate enforces the AC6 guardrails on the rule at index i: a reason, a
// file or package scope, no escape from the repo via ".." or an absolute
// path, and a match that compiles as RE2 (Go's regexp, so a PCRE-only
// construct is rejected here rather than silently never matching).
func (r IgnoreRule) validate(i int) error {
	if strings.TrimSpace(r.Reason) == "" {
		return fmt.Errorf("%s: reason is required", r.label(i))
	}
	if strings.TrimSpace(r.File) == "" && strings.TrimSpace(r.Package) == "" {
		return fmt.Errorf("%s: needs a file or package scope", r.label(i))
	}
	for _, p := range []struct{ key, val string }{{"file", r.File}, {"package", r.Package}} {
		if err := checkRulePath(p.val); err != nil {
			return fmt.Errorf("%s: %s %w", r.label(i), p.key, err)
		}
	}
	if r.catchAllScope() && r.Match == "" && strings.TrimSpace(r.Category) == "" {
		return fmt.Errorf("%s: a catch-all scope needs a match or category to narrow it", r.label(i))
	}
	if r.Match != "" {
		if _, err := regexp.Compile(r.Match); err != nil {
			return fmt.Errorf("%s: match does not compile: %w", r.label(i), err)
		}
	}
	return nil
}

// catchAllScope reports whether every scope the rule sets names no particular
// file or package: a file glob made only of `*` and `**` segments (or "."),
// and a package of "." (the repo root). ignore.yaml is read from the reviewed
// checkout, so without this a pull request could add one such rule and drop
// every finding with the gate still green.
func (r IgnoreRule) catchAllScope() bool {
	if f := strings.TrimSpace(r.File); f != "" {
		for _, seg := range strings.Split(cleanIgnorePath(f), "/") {
			if seg != "*" && seg != "**" && seg != "." {
				return false
			}
		}
	}
	if p := strings.TrimSpace(r.Package); p != "" && cleanIgnorePath(p) != "." {
		return false
	}
	return true
}

// checkRulePath rejects a path that could reach outside the repository.
// Backslashes are folded to slashes first so `..\x.go` and `C:\x` are caught
// on every OS, not only on Windows.
func checkRulePath(p string) error {
	if p == "" {
		return nil
	}
	s := strings.ReplaceAll(p, `\`, "/")
	if path.IsAbs(s) || filepath.IsAbs(p) || (len(s) >= 2 && s[1] == ':') {
		return fmt.Errorf("must be repo-relative, not absolute")
	}
	for _, seg := range strings.Split(s, "/") {
		if seg == ".." {
			return fmt.Errorf(`must not contain a ".." segment`)
		}
	}
	return nil
}

// ParseIgnore decodes and validates an ignore.yaml document. Decoding is
// strict (KnownFields), so a typo'd or unsupported key such as `line:` is an
// error rather than a rule that silently matches more than its author meant.
// An empty document is no rules. Any invalid rule rejects the whole file,
// returning nil rules.
func ParseIgnore(data []byte) ([]IgnoreRule, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var doc ignoreFile
	if err := dec.Decode(&doc); err != nil {
		if errors.Is(err, io.EOF) {
			return nil, nil
		}
		return nil, fmt.Errorf("ignore yaml: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("ignore yaml: want one document, found more")
	}
	for i, r := range doc.Rules {
		if err := r.validate(i); err != nil {
			return nil, err
		}
	}
	return doc.Rules, nil
}

// MarshalIgnore validates rules and encodes them as an ignore.yaml document
// that ParseIgnore reads back to the same rules. It refuses an invalid rule,
// so the writer never puts on disk a file the next reconcile would reject.
func MarshalIgnore(rules []IgnoreRule) ([]byte, error) {
	for i, r := range rules {
		if err := r.validate(i); err != nil {
			return nil, err
		}
	}
	if rules == nil {
		rules = []IgnoreRule{}
	}
	return encodeIgnoreYAML(ignoreFile{Rules: rules})
}

func encodeIgnoreYAML(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ReadIgnoreFile reads and validates the rule file at path. A missing file is
// no rules and no error (the common case: most repos have no ignore file); any
// other failure is an error naming the file.
func ReadIgnoreFile(path string) ([]IgnoreRule, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	rules, err := ParseIgnore(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return rules, nil
}

// AppendIgnoreRule validates rule and appends it to the rule file at path,
// creating the file and its directory if needed. The existing file must parse
// cleanly first, so an append never launders a bad rule already on disk. The
// edit goes through the YAML node tree rather than re-marshalling []IgnoreRule,
// so comments a user wrote by hand survive `atcr ignore`. The write is atomic;
// on any error the file is left untouched.
func AppendIgnoreRule(path string, rule IgnoreRule) error {
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	existing, err := ParseIgnore(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := rule.validate(len(existing)); err != nil {
		return err
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	seq := rulesSequence(&root)
	var ruleNode yaml.Node
	if err := ruleNode.Encode(rule); err != nil {
		return err
	}
	seq.Style = 0 // a `rules: []` seed would otherwise stay in flow style
	seq.Content = append(seq.Content, &ruleNode)

	out, err := encodeIgnoreYAML(&root)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfs.WriteFileAtomic(path, out)
}

// rulesSequence returns the `rules:` sequence node of a decoded ignore.yaml,
// creating the document, mapping, key and sequence as needed. root has
// already passed ParseIgnore, so any `rules` value present is a sequence.
func rulesSequence(root *yaml.Node) *yaml.Node {
	if root.Kind != yaml.DocumentNode {
		*root = yaml.Node{Kind: yaml.DocumentNode}
	}
	if len(root.Content) == 0 {
		root.Content = []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}
	}
	m := root.Content[0]
	if m.Kind != yaml.MappingNode {
		// An explicit null document ("~"); replace it with an empty mapping.
		*m = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	m.Style = 0 // a `{}` seed would otherwise keep the whole file in flow style
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == "rules" {
			v := m.Content[i+1]
			if v.Kind != yaml.SequenceNode {
				// `rules:` with no value decodes as null.
				*v = yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
			}
			return v
		}
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	m.Content = append(m.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "rules"}, seq)
	return seq
}
