package cli

import (
	"errors"

	"github.com/spf13/cobra"
)

// skillHarness holds the project-level and user-level install directories a
// single agent harness reads skills from.
type skillHarness struct {
	project string
	user    string
}

// skillHarnesses is the harness→path table. STUB: deliberately empty and wrong
// so the RED tests fail on content rather than on compilation.
var skillHarnesses = map[string]skillHarness{}

// resolveSkillDest resolves the export destination. STUB.
func resolveSkillDest(_ string, _ bool, _ string) (string, error) {
	return "", errors.New("not implemented")
}

// newSkillCmd builds `atcr skill`. STUB.
func newSkillCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "skill",
		Short: "Install the shipped atcr Agent Skill",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  func(cmd *cobra.Command, _ []string) error { return cmd.Help() },
	}
	export := &cobra.Command{
		Use:   "export",
		Short: "Write the embedded skill tree to disk",
		Args:  usageArgs(cobra.NoArgs),
		RunE:  func(_ *cobra.Command, _ []string) error { return errors.New("not implemented") },
	}
	export.Flags().String("harness", "claude", "target agent harness")
	export.Flags().Bool("user", false, "install for the user rather than the project")
	export.Flags().String("dir", "", "explicit destination directory")
	export.Flags().Bool("force", false, "overwrite a non-empty destination")
	cmd.AddCommand(export)
	return cmd
}
