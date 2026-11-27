package cli

import (
	"io"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	toon "github.com/toon-format/toon-go"

	goaxi "github.com/samestrin/go-axi"
)

// Structured errors on stdout (epic 165 T2, axi.md Principle 6). In AXI mode a
// failed run prints one `error: true` TOON document on stdout instead of the
// `atcr: <message>` line on stderr, so an agent reading one stream in one
// encoding learns that the command failed, why, and what it may call instead.
// Human mode keeps the stderr line.

// payloadWriter wraps the process stdout and records whether anything reached
// it. runMain writes the error document only when no payload was written: a gate
// exit (reconcile --fail-on) has already printed its result, and a second
// document after it would make stdout two documents.
type payloadWriter struct {
	w     io.Writer
	wrote bool
}

func (p *payloadWriter) Write(b []byte) (int, error) {
	if len(b) > 0 {
		p.wrote = true
	}
	return p.w.Write(b)
}

// Unwrap exposes the wrapped stdout, so stdoutFile still finds the terminal.
func (p *payloadWriter) Unwrap() io.Writer { return p.w }

// errorModeIsAXI reports whether the failed run renders its error in AXI mode.
// cmd is the command cobra was executing (root when the command was unknown).
//
// A command without an AXI mode keeps the stderr line, except a command group
// (`debt`, `config`): its failures are usage errors about its subcommands, so an
// agent calling `atcr debt nosuch` gets the document naming them. When the root
// PersistentPreRunE resolved the mode, that decision stands. When the run failed
// before it (an unknown flag or command) or inside it (a bad --log-format), the
// mode is resolved again from the raw args, the environment and stdout with the
// same rules (resolveAXIModeFlags): a usage error stops flag parsing at the
// first bad flag, so in `status --bogus --axi` cobra never saw --axi. The args
// are parsed against cmd's flags with unknown flags tolerated, so a flag's value
// and anything after "--" are not mistaken for a mode flag. A resolver error
// (--axi with --human) is AXI: --axi was asked for explicitly.
func errorModeIsAXI(cmd *cobra.Command, args []string, stdout io.Writer) bool {
	if cmd == nil || (!hasAXIMode(cmd) && !cmd.HasAvailableSubCommands()) {
		return false
	}
	if axi, ok := axiModeFromContext(cmd.Context()); ok {
		return axi
	}
	fs := pflag.NewFlagSet("mode", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.ParseErrorsWhitelist.UnknownFlags = true
	fs.AddFlagSet(cmd.Flags())
	fs.AddFlagSet(cmd.InheritedFlags())
	_ = fs.Parse(args)
	axi, err := resolveAXIModeFlags(fs, stdout, io.Discard)
	return err != nil || axi
}

// errorHelp is the error document's help[]: the commands an unknown command
// could have been, the flags an unknown flag could have been, and otherwise the
// failed command's --help.
func errorHelp(root, cmd *cobra.Command, args []string, err error) []string {
	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "unknown command "):
		var lines []string
		for _, c := range commandForArgs(root, args).Commands() {
			if c.IsAvailableCommand() {
				lines = append(lines, c.CommandPath())
			}
		}
		if len(lines) > 0 {
			return lines
		}
	case strings.HasPrefix(msg, "unknown flag: "), strings.HasPrefix(msg, "unknown shorthand flag: "):
		if lines := validFlags(cmd); len(lines) > 0 {
			return lines
		}
	}
	return []string{cmd.CommandPath() + " --help"}
}

// commandForArgs walks args' command words down from root and returns the
// deepest command they name: the parent an unknown command was looked up in.
// cobra hands back root, not that parent, when a lookup fails.
func commandForArgs(root *cobra.Command, args []string) *cobra.Command {
	cur := root
	for _, a := range args {
		if a == "--" {
			break
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		next := findChild(cur, a)
		if next == nil {
			break
		}
		cur = next
	}
	return cur
}

// findChild returns cur's subcommand named or aliased name, or nil.
func findChild(cur *cobra.Command, name string) *cobra.Command {
	for _, c := range cur.Commands() {
		if c.Name() == name || c.HasAlias(name) {
			return c
		}
	}
	return nil
}

// validFlags names every visible flag cmd accepts, its own and inherited, as
// `--name` or `-s/--name`.
func validFlags(cmd *cobra.Command) []string {
	seen := map[string]bool{}
	var lines []string
	add := func(f *pflag.Flag) {
		if f.Hidden || seen[f.Name] {
			return
		}
		seen[f.Name] = true
		name := "--" + f.Name
		if f.Shorthand != "" {
			name = "-" + f.Shorthand + "/" + name
		}
		lines = append(lines, name)
	}
	cmd.Flags().VisitAll(add)
	cmd.InheritedFlags().VisitAll(add)
	return lines
}

// writeErrorDocument prints the AXI error document for err on w: `error: true`,
// the redacted `message` (terminalErrorLine, the same scrubbing the stderr line
// gets), the numeric exit `code`, then help[].
func writeErrorDocument(w io.Writer, err error, code int, help []string) error {
	payload := toon.NewObject(
		toon.Field{Key: "error", Value: true},
		toon.Field{Key: "message", Value: goaxi.SanitizeString(terminalErrorLine(err))},
		toon.Field{Key: "code", Value: code},
	)
	return writeAXIHelp(w, payload, help)
}
