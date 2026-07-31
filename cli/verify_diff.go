package cli

import (
	"bytes"
	"context"
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

// newVerifyDiffCmd builds `atcr verify diff`: the standalone surface over the
// diff-smell analyzer. Epic 35.3 ported that analyzer into
// internal/verify/diffsmell.go but wired it ONLY into the in-process
// fix-selection gate, explicitly scoping out "any subprocess or CLI shell-out" —
// so the detection logic had no way to reach a consumer outside this binary.
// This command is that surface: it adds no analysis, only input resolution,
// rendering, and an opt-in exit-code gate.
func newVerifyDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff [file]",
		Short: "Scan a unified diff for over-simplification (reward-hack) fingerprints",
		Long: "Scan a unified diff for the mechanical fingerprints of an over-simplified " +
			"(\"reward-hacked\") patch: a change that makes a test pass by deleting the test, " +
			"skipping it, renaming it out of the test namespace, or weakening its assertions, " +
			"or that resolves a lint by suppressing it.\n\n" +
			"Reads the diff from a file argument, from stdin (the default, or an explicit \"-\"), " +
			"from the git index (--staged), or from a commit range (--range). Reports a verdict " +
			"of clean, soft_only, or hard. Exits 0 regardless of verdict unless --fail-on is set.\n\n" +
			"Note: a diff touching only test files raises the HARD test_only smell here. The " +
			"in-process fix gate suppresses that case for findings whose own file is a test, but " +
			"a standalone caller has no finding to key that exemption on.",
		Args: usageArgs(cobra.MaximumNArgs(1)),
		RunE: runVerifyDiff,
	}
	cmd.Flags().Bool("staged", false, "scan the staged changes (git diff --cached) in --repo")
	cmd.Flags().String("range", "", "scan a git commit range in --repo, e.g. main..HEAD")
	cmd.Flags().String("repo", ".", "repo root for --staged / --range (default: current directory)")
	cmd.Flags().Bool("json", false, "emit the scan as JSON on stdout instead of the text summary")
	cmd.Flags().String("fail-on", "", "exit 1 when the verdict reaches this level: hard, soft, or none (default: never fail)")
	return cmd
}

func runVerifyDiff(cmd *cobra.Command, args []string) error {
	// Validate the gate BEFORE any I/O so a bad value fails fast as a usage
	// error rather than after a git subprocess, mirroring runVerify's
	// --min-severity ordering.
	gate, err := smellGate(cmd)
	if err != nil {
		return err
	}

	diff, err := readDiffInput(cmd, args)
	if err != nil {
		return err
	}

	// Non-diff content is a usage error here, DIVERGING from the in-process fix
	// gate (which classifies unparseable content clean and passes it through).
	// That gate reads Finding.Fix, which is free-form by construction; a caller
	// of this command asserted "here is a diff", so reporting clean for input
	// that was never actually scanned would hand them a false pass. Empty input
	// is exempt: nothing staged is legitimately clean.
	if strings.TrimSpace(diff) != "" && !verify.LooksLikeUnifiedDiff(diff) {
		return usageError(errors.New("input is not a unified diff: expected a `diff --git`, `--- `/`+++ ` header pair, or an `@@` hunk header"))
	}

	res := verify.AnalyzeDiff(diff)

	// Render BEFORE the gate so a tripped gate still tells the caller WHY in the
	// same invocation, instead of forcing a second ungated run.
	asJSON, _ := cmd.Flags().GetBool("json")
	if asJSON {
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
// `atcr review --fail-on`); an explicit "none" is the same thing spelled out, so
// a scripted consumer can always pass the flag and vary only its value. Any
// other value — including an explicitly empty one — is a usage error (exit 2).
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
		return "", usageError(fmt.Errorf("invalid --fail-on %q: must be one of hard, soft, none", raw))
	}
}

// smellGateError returns a plain error (exit 1) when the verdict reaches the
// gate level, else nil. Mirrors gateFindings' contract: "" is a no-op, and the
// message names both the counts and the level so CI logs are self-explaining.
func smellGateError(res *verify.DiffSmellResult, gate string) error {
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

// readDiffInput resolves the single diff source and returns its text. Exactly
// one source may be named: a file argument, an explicit "-" (stdin), --staged,
// or --range. Naming none reads stdin, so the command composes in a pipe.
func readDiffInput(cmd *cobra.Command, args []string) (string, error) {
	staged, _ := cmd.Flags().GetBool("staged")
	rangeSpec, _ := cmd.Flags().GetString("range")
	hasRange := cmd.Flags().Changed("range")

	fileArg := ""
	stdinArg := false
	if len(args) == 1 {
		if args[0] == "-" {
			stdinArg = true
		} else {
			fileArg = args[0]
		}
	}

	// Counted rather than checked pairwise so adding a source cannot leave a
	// combination unguarded. cobra's MarkFlagsMutuallyExclusive is deliberately
	// NOT used: its violation surfaces as a plain error (exit 1), and every
	// other input mistake on this command is exit 2.
	named := 0
	for _, set := range []bool{staged, hasRange, fileArg != "", stdinArg} {
		if set {
			named++
		}
	}
	if named > 1 {
		return "", usageError(errors.New("choose exactly one diff source: a file argument, \"-\", --staged, or --range"))
	}

	switch {
	case staged:
		return gitDiffText(cmd.Context(), cmd, "--cached")
	case hasRange:
		spec := strings.TrimSpace(rangeSpec)
		if spec == "" {
			return "", usageError(errors.New("--range must not be empty: pass a revision range, e.g. main..HEAD"))
		}
		// The range is interpolated into git's argv. Argv-only exec means no
		// shell injection, but a leading "-" would still be read by git as an
		// OPTION rather than a revision — `--output=<path>` alone is enough to
		// make the scan write a file of the caller's choosing.
		if strings.HasPrefix(spec, "-") {
			return "", usageError(fmt.Errorf("invalid --range %q: a revision range must not start with '-'", rangeSpec))
		}
		return gitDiffText(cmd.Context(), cmd, spec)
	case fileArg != "":
		b, err := os.ReadFile(fileArg)
		if err != nil {
			return "", usageError(fmt.Errorf("read diff file: %w", err))
		}
		return string(b), nil
	default:
		b, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return "", usageError(fmt.Errorf("read diff from stdin: %w", err))
		}
		return string(b), nil
	}
}

// gitDiffText runs `git -C <repo> diff --no-ext-diff <extra...>` and returns
// stdout. --no-ext-diff is passed here at the call site, not by gitexec, because
// it is diff-command-specific: without it a poisoned repo-local diff.external
// entry would execute in place of git's own differ (see internal/gitexec's
// package doc). Any git failure is a usage error (exit 2) so exit 1 keeps
// meaning "the gate tripped" and nothing else.
func gitDiffText(ctx context.Context, cmd *cobra.Command, extra ...string) (string, error) {
	repo, _ := cmd.Flags().GetString("repo")
	if strings.TrimSpace(repo) == "" {
		repo = "."
	}
	argv := append([]string{"-C", repo, "diff", "--no-ext-diff"}, extra...)

	var stdout, stderr bytes.Buffer
	gc := gitexec.CommandContextFn(ctx, argv...)
	gc.Stdout = &stdout
	gc.Stderr = &stderr
	if err := gc.Run(); err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return "", usageError(fmt.Errorf("git diff in %s: %s", repo, msg))
		}
		return "", usageError(fmt.Errorf("git diff in %s: %w", repo, err))
	}
	return stdout.String(), nil
}

// renderSmellJSON writes the scan as the sole content of stdout, so a consumer
// can pipe the stream straight into a parser. Diagnostics stay on stderr, per
// atcr's convention (see the --axi note in main.go).
func renderSmellJSON(w io.Writer, res *verify.DiffSmellResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		return fmt.Errorf("encode diff-smell result: %w", err)
	}
	return nil
}

// renderSmellText writes the human summary: a verdict line every caller can
// grep, then one line per smell.
func renderSmellText(w io.Writer, res *verify.DiffSmellResult) {
	s := res.Summary
	if len(res.Smells) == 0 {
		_, _ = fmt.Fprintf(w, "verdict: %s — no over-simplification smells; %d test file(s), %d impl file(s)\n",
			s.Verdict, s.TestFiles, s.ImplFiles)
		return
	}
	_, _ = fmt.Fprintf(w, "verdict: %s — %d hard, %d soft smell(s); %d test file(s), %d impl file(s)\n",
		s.Verdict, s.Hard, s.Soft, s.TestFiles, s.ImplFiles)
	for _, sm := range res.Smells {
		_, _ = fmt.Fprintf(w, "  %-4s %-18s %s: %s\n",
			strings.ToUpper(sm.Severity), sm.Type, sm.File, sm.Evidence)
	}
}
