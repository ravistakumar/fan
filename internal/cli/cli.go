// Package cli defines the fan command-line interface.
package cli

import (
	"github.com/spf13/cobra"
)

// version is overridden at build time via -ldflags.
var version = "dev"

// Execute runs the root command and returns its exit error.
func Execute() error {
	root := &cobra.Command{
		Use:           "fan",
		Short:         "Fan independent coding tasks across agents, in parallel",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	return root.Execute()
}
