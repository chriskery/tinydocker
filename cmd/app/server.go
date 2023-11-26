package app

import (
	"fmt"
	"github.com/chriskery/tinydocker/cmd/app/cmds"
	"github.com/spf13/cobra"
)

// NewTinyDockerCommand creates a *cobra.Command object with default parameters
func NewTinyDockerCommand() *cobra.Command {
	cmd := &cobra.Command{
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				cmd.SetOut(cmd.ErrOrStderr())
				cmd.HelpFunc()(cmd, args)
				return nil
			}
			return fmt.Errorf("tinydocker: '%s' is not a tinydocker command.\nSee 'tinydocker --help'", args[0])
		},
		SilenceUsage:          true,
		DisableFlagsInUseLine: true,
		TraverseChildren:      true,
	}
	cmd.AddCommand(cmds.NewTinyDockerRunCommand())
	cmd.AddCommand(cmds.NewTinyDockerInitCommand())
	return cmd
}
