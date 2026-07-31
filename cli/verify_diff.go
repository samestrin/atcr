package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/samestrin/atcr/internal/gitexec"
	"github.com/samestrin/atcr/internal/verify"
	"github.com/spf13/cobra"
)

// newVerifyDiffCmd builds `atcr verify diff` (epic 35.10): the standalone
// surface over the diff-smell analyzer. Epic 35.3 ported that analyzer into
// internal/verify/diffsmell.go but wired it ONLY into the in-process
// fix-selection gate, explicitly scoping out "any subprocess or CLI shell-out" —
// so the detection logic could not reach a consumer outside this binary, and a
// consumer's only alternatives were a second dependency or a third copy of the
// analyzer. This command adds no analysis: only input resolution, rendering, and
// an opt-in exit-code gate.
func newVerifyDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Scan a unified diff for over-simplification (reward-hack) fingerprints",
		Long: "Scan a unified diff for the mechanical fingerprints of an over-simplified " +
			"(\"reward-hacked\") patch: a change that makes a test pass by deleting the test, " +
			"skipping it, renaming it out of the test namespace, or weakening its assertions, " +
			"or that resolves a lint by suppressing it.\n\n" +
			"Deterministic and model-free — no agent, no provider, no API key, no network.\n\n" +
			"Diff sources, one at a time: --diff <path> (or --diff - for stdin), --staged, " +
			"--range <a>..<b>, or --rev <rev> (the default, HEAD). Reports a verdict of clean, " +
			"soft_only, or hard, and exits 0 regardless of verdict unless --fail-on is set.\n\n" +
			"Note: a diff touching only test files raises the HARD test_only smell here. The " +
			"in-process fix gate suppresses that case for findings whose own file is a test, but " +
			"a standalone caller has no finding to key that exemption on.",
		Args: usageArgs(cobra.NoArgs),
		RunE: runVerifyDiff,
	}
	cmd.Flags().String("diff", "", "scan a unified diff read from this file, or from stdin when set to \"-\"")
	cmd.Flags().Bool("staged", false, "scan the staged changes (git diff --cached) in --repo")
	cmd.Flags().String("range", "", "scan a git commit range in --repo, e.g. main..HEAD")
	cmd.Flags().String("rev", "HEAD", "scan a single commit in --repo (the default source)")
	cmd.Flags().String("repo", ".", "repo root for --staged / --range / --rev (default: current directory)")
	cmd.Flags().Bool("json", false, "emit the scan as JSON on stdout instead of the text summary")
	cmd.Flags().String("fail-on", "", "exit 1 when the verdict reaches this level: hard, soft, or none (default: never fail)")
	return cmd
}

func runVerifyDiff(cmd *cobra.Command, _ []string) error {
	// Validate the gate BEFORE any I/O so a bad value fails fast as a usage error
	// rather than after a git subprocess (AC3), mirroring runVerify's
	// --min-severity ordering.
	gate, err := smellGate(cmd)
	if err != nil {
		return err
	}

	diff, source, err := readDiffInput(cmd)
	if err != nil {
		return err
	}

	// Two shapes are reported CLEAN rather than refused, both with a note on
	// stderr so the pass is visible instead of silent:
	//
	//   - Empty input. An empty staged set is a normal git state in a pre-commit
	//     hook, unlike an empty range for `atcr review` (docs/ci-integration.md),
	//     where it means misconfiguration.
	//   - Content that is not a unified diff. This preserves drop-in parity with
	//     upstream `llm-support diff-smell`, which never exits nonzero on content,
	//     and mirrors the in-process gate's treatment of an unparseable fix.
	//
	// stdout stays payload-only either way, per the AXI guarantee
	// (docs/agentic-consumption.md).
	note := ""
	switch {
	case strings.TrimSpace(diff) == "":
		note = fmt.Sprintf("note: %s produced an empty diff; nothing to scan", source)
	case !verify.LooksLikeUnifiedDiff(diff):
		note = fmt.Sprintf("note: %s is not a unified diff; reported clean without scanning", source)
		diff = ""
	}
	if note != "" {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), note)
	}

	res := verify.AnalyzeDiff(diff)

	// Render BEFORE the gate so a tripped gate still tells the caller WHY in the
	// same invocation, instead of forcing a second ungated run.
	if asJSON, _ := cmd.Flags().GetBool("json"); asJSON {
		if err := renderSmellJSON(cmd.OutOrStdout(), res); err != nil {
			return err
		}
	} else {
		renderSmellText(cmd.OutOrStdout(), res)
	}

	return smellGateError(res, gate)
}

// smellGate reads and validates --fail-on, returning the canonical level or ""
// when gating is off. An ABSENT flag is unset (opt-in no-op, matching
// `atcr review --fail-on` and upstream's always-zero exit); an explicit "none"
// is the same thing spelled out, so a scripted consumer can always pass the flag
// and vary only its value. Any other value — including an explicitly empty one —
// is a usage error (exit 2).
//
// The values are verdicts (hard/soft), not the severities `atcr review` and
// `atcr reconcile` take (CRITICAL/HIGH/MEDIUM/LOW). Same concept, disjoint value
// sets, so a severity passed here is a usage error rather than a silent misread.
func smellGate(cmd *cobra.Command) (string, error) {
	if !cmd.Flags().Changed("fail-on") {
		return "", nil
	}
	raw, _ := cmd.Flags().GetString("fail-on")
	switch v := strings.ToLower(strings.TrimSpace(raw)); v {
	case "none":
		return "", nil
	case "hard", "soft":
		return v, nil
	default:
		return "", usageError(fmt.Errorf("invalid --fail-on %q: must be one of hard, soft, none "+
			"(this command gates on diff-smell verdicts, not on finding severities)", raw))
	}
}

// smellGateError returns a plain error (exit 1) when the verdict reaches the
// gate level, else nil. Mirrors gateFindings' contract: "" is a no-op, and the
// message names both the counts and the level so CI logs are self-explaining.
func smellGateError(res *verify.SmellResult, gate string) error {
	trip := false
	switch gate {
	case "hard":
		trip = res.Summary.Verdict == verify.VerdictHard
	case "soft":
		trip = res.Summary.Verdict != verify.VerdictClean
	}
	if !trip {
		return nil
	}
	return fmt.Errorf("diff-smell verdict %s: %d hard, %d soft smell(s) at or above --fail-on=%s",
		res.Summary.Verdict, res.Summary.Hard, res.Summary.Soft, gate)
}

// diffSource names the four mutually exclusive sources, in flag-name form so a
// conflict message can name both offenders (AC4).
var diffSourceFlags = []string{"diff", "staged", "range", "rev"}

// readDiffInput resolves the single diff source and returns its text plus a
// human label for the stderr notes. At most one source may be NAMED; naming none
// falls through to --rev's HEAD default, so a bare `atcr verify diff` scans the
// last commit (upstream `diff-smell`'s own default shape).
func readDiffInput(cmd *cobra.Command) (text, source string, err error) {
	// --staged is a bool: cobra marks it Changed on ANY assignment, so
	// `--staged=false` (a scripted `--staged=$FLAG` with FLAG off) would read as
	// naming the source. The parsed value, not Changed, decides both the
	// mutual-exclusion roll call and the source switch below.
	staged, _ := cmd.Flags().GetBool("staged")
	var named []string
	for _, f := range diffSourceFlags {
		if !cmd.Flags().Changed(f) {
			continue
		}
		if f == "staged" && !staged {
			continue
		}
		named = append(named, "--"+f)
	}
	if len(named) > 1 {
		return "", "", usageError(fmt.Errorf("%s and %s are mutually exclusive: name exactly one diff source",
			named[0], named[1]))
	}

	switch {
	case cmd.Flags().Changed("diff"):
		path, _ := cmd.Flags().GetString("diff")
		if path == "-" {
			b, rerr := io.ReadAll(cmd.InOrStdin())
			if rerr != nil {
				return "", "", usageError(fmt.Errorf("read diff from stdin: %w", rerr))
			}
			return string(b), "stdin", nil
		}
		if strings.TrimSpace(path) == "" {
			return "", "", usageError(errors.New("--diff must name a file, or \"-\" for stdin"))
		}
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return "", "", usageError(fmt.Errorf("read diff file: %w", rerr))
		}
		return string(b), path, nil

	case staged:
		out, gerr := gitText(cmd, "diff", "--cached")
		return out, "the staged changes", gerr

	case cmd.Flags().Changed("range"):
		spec, rerr := revArg(cmd, "range")
		if rerr != nil {
			return "", "", rerr
		}
		out, gerr := gitText(cmd, "diff", spec)
		return out, spec, gerr

	default:
		spec, rerr := revArg(cmd, "rev")
		if rerr != nil {
			return "", "", rerr
		}
		// --format= suppresses the commit header so only the diff body reaches the
		// parser; without it the subject and author lines would be scanned as
		// content.
		out, gerr := gitText(cmd, "show", "--format=", spec)
		return out, spec, gerr
	}
}

// revArg reads a revision-shaped flag and refuses a value git would read as an
// OPTION. Argv-only exec means no shell injection, but a leading "-" is still
// live: `--range --output=<path>` alone is enough to make the scan write a file
// of the caller's choosing.
func revArg(cmd *cobra.Command, name string) (string, error) {
	raw, _ := cmd.Flags().GetString(name)
	spec := strings.TrimSpace(raw)
	if spec == "" {
		return "", usageError(fmt.Errorf("--%s must not be empty", name))
	}
	if strings.HasPrefix(spec, "-") {
		return "", usageError(fmt.Errorf("invalid --%s %q: a revision must not start with '-'", name, raw))
	}
	return spec, nil
}

// gitText runs a hardened `git -C <repo> <sub> --no-ext-diff --no-color ...` and
// returns stdout. Both flags are passed at the call site rather than by gitexec
// because they are diff-command-specific: --no-ext-diff neutralizes a poisoned
// repo-local diff.external (see internal/gitexec's package doc), and --no-color
// defeats a `color.ui = always` config that would otherwise feed ANSI escapes to
// a parser expecting a plain unified diff. Any git failure is a usage error
// (exit 2), so exit 1 keeps meaning "the gate tripped" and nothing else — with
// one exception: a cancelled context propagates as a plain error, because an
// interrupt is not a bad invocation.
func gitText(cmd *cobra.Command, sub string, extra ...string) (string, error) {
	// Shared with `atcr verify` and `atcr reconcile` so the three commands
	// normalize --repo identically — including the existence check, which turns
	// a nonexistent root into the shared usage-error wording instead of a raw
	// git message.
	repo, err := normalizeRepoFlag(cmd)
	if err != nil {
		return "", err
	}
	argv := append([]string{"-C", repo, sub, "--no-ext-diff", "--no-color"}, extra...)

	var stdout, stderr bytes.Buffer
	gc := gitexec.CommandContextFn(cmd.Context(), argv...)
	gc.Stdout = &stdout
	gc.Stderr = &stderr
	if err := gc.Run(); err != nil {
		// A cancelled context (SIGINT/SIGTERM via runMain's handler) killed the
		// git child: propagate the interruption as a plain error, never a usage
		// error — exit 2 must keep meaning "bad invocation", and a CI wrapper
		// keying on it must not report a cancelled job as one.
		if ctxErr := cmd.Context().Err(); ctxErr != nil {
			return "", ctxErr
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", usageError(fmt.Errorf("git %s in %s: %s", sub, repo, msg))
		}
		return "", usageError(fmt.Errorf("git %s in %s: %w", sub, repo, err))
	}
	return stdout.String(), nil
}

// renderSmellJSON writes the scan as the sole content of stdout, so a consumer
// can pipe the stream straight into a parser. Diagnostics and the clean-input
// notes stay on stderr, per atcr's payload-only stdout guarantee.
func renderSmellJSON(w io.Writer, res *verify.SmellResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		return fmt.Errorf("encode diff-smell result: %w", err)
	}
	return nil
}

// renderSmellText writes the human summary: a verdict line every caller can
// grep, then one line per smell, citing file:line where the smell has one.
func renderSmellText(w io.Writer, res *verify.SmellResult) {
	s := res.Summary
	if len(res.Smells) == 0 {
		_, _ = fmt.Fprintf(w, "verdict: %s — no over-simplification smells; %d test file(s), %d impl file(s)\n",
			s.Verdict, s.TestFiles, s.ImplFiles)
		return
	}
	_, _ = fmt.Fprintf(w, "verdict: %s — %d hard, %d soft smell(s); %d test file(s), %d impl file(s)\n",
		s.Verdict, s.Hard, s.Soft, s.TestFiles, s.ImplFiles)
	for _, sm := range res.Smells {
		loc := sm.File
		if sm.Line > 0 {
			loc = fmt.Sprintf("%s:%d", sm.File, sm.Line)
		}
		_, _ = fmt.Fprintf(w, "  %-4s %-18s %s: %s\n",
			strings.ToUpper(sm.Severity), sm.Type, loc, sm.Evidence)
	}
}
