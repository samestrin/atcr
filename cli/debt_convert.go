package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/samestrin/atcr/internal/localdebt"
)

// debtConvertFuncs carries the conversions runDebtConvert calls. A test
// injects a failing conversion through newDebtConvertCmdWith instead of
// swapping package-level variables.
type debtConvertFuncs struct {
	convert func(string, localdebt.ReadOpts) (localdebt.ConvertResult, error)
	plan    func(string, localdebt.ReadOpts) (localdebt.ConvertResult, error)
}

func newDebtConvertCmd() *cobra.Command {
	return newDebtConvertCmdWith(debtConvertFuncs{
		convert: localdebt.Convert,
		plan:    localdebt.ConvertPlan,
	})
}

func newDebtConvertCmdWith(fns debtConvertFuncs) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Convert the local TD store's month shards to the event layout (one way)",
		Long: "atcr debt convert moves every record of the store's month shards\n" +
			"(YYYY-MM.jsonl) into its own event file under items/ and removes the\n" +
			"shards. The store then writes an event file per add and resolution, so\n" +
			"two clones of a shared store merge with no git conflict.\n\n" +
			"No record is lost: the store is folded before and after, and no shard is\n" +
			"removed unless the two folds are identical; otherwise nothing changes.\n" +
			"A shard holding a line this atcr cannot carry into an event file is left\n" +
			"whole and reported. The store is opted in to the event layout even when\n" +
			"no shard converts. A store where two records of one item tie on timestamp\n" +
			"and status is refused; `atcr debt compact` first clears most such ties.\n" +
			"The conversion is one way; there is no command back.\n" +
			"Run it with --dry-run first to see what a real run would move.",
		Args: usageArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, _ []string) error { return runDebtConvert(cmd, fns) },
	}
	addDebtStoreFlag(cmd)
	cmd.Flags().Bool("dry-run", false, "report what would be converted without changing the store")
	return cmd
}

func runDebtConvert(cmd *cobra.Command, fns debtConvertFuncs) error {
	dir, err := debtStoreDir(cmd)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	opts := localdebt.ReadOpts{Writer: cmd.ErrOrStderr()}
	convert := fns.convert
	if dryRun {
		convert = fns.plan
	}
	res, err := convert(dir, opts)
	if err != nil {
		// A run that failed may still have finished an interrupted conversion or
		// opted the store in (localdebt.Convert reports both with the error), and
		// every later write is then an event file: say so before the error, on
		// stderr, so a script parsing stdout never reads a failed run as success.
		errOut := cmd.ErrOrStderr()
		if res.Resumed {
			_, _ = fmt.Fprintln(errOut, "Finished an interrupted conversion.")
		}
		if res.OptedIn && !dryRun {
			_, _ = fmt.Fprintln(errOut, optedInLine)
		}
		return fmt.Errorf("convert: %w", err)
	}
	out := cmd.OutOrStdout()

	if !res.StoreFound {
		_, _ = fmt.Fprintln(out, "No local TD store to convert.")
		return nil
	}
	if res.Resumed {
		_, _ = fmt.Fprintln(out, "Finished an interrupted conversion.")
	}
	switch {
	case len(res.Shards) > 0:
		verb := "Converted"
		if dryRun {
			verb = "Would convert"
		}
		_, _ = fmt.Fprintf(out, "%s %d record(s) from %d month shard(s) to event files (items/); %d item(s) fold the same before and after.\n",
			verb, res.Records, len(res.Shards), res.Items)
	case len(res.Kept) == 0:
		_, _ = fmt.Fprintf(out, "Nothing to convert: the store already uses the event layout (%d item(s)).\n", res.Items)
	default:
		_, _ = fmt.Fprintln(out, "Nothing to convert: no month shard can be carried into event files.")
	}
	if len(res.Kept) > 0 {
		_, _ = fmt.Fprintf(out, "Left %d file(s) in place; their records are still read:\n", len(res.Kept))
		for _, k := range res.Kept {
			_, _ = fmt.Fprintf(out, "  %s: %s\n", k.Name, k.Reason)
		}
	}
	if res.OptedIn {
		line := optedInLine
		if dryRun {
			line = "A real run opts the store in" + optedInTail
		}
		_, _ = fmt.Fprintln(out, line)
	}
	if dryRun {
		_, _ = fmt.Fprintln(out, "Dry run: the store was not modified.")
	}
	return nil
}

const (
	optedInTail = " to the event layout: every later add and resolution writes an event file (items/)."
	optedInLine = "The store is now opted in" + optedInTail
)
