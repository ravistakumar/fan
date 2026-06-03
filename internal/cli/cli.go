// Package cli defines the fan command-line interface.
package cli

import (
	"time"

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
	root.AddCommand(newRunCmd())
	root.AddCommand(newPlanCmd())
	return root.Execute()
}

// timestamp is the slug used for integration branch names.
func timestamp() string {
	return time.Now().Format("2006-01-02-1504")
}
