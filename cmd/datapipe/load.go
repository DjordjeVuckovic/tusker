package main

import "github.com/spf13/cobra"

func newLoadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "load",
		Short: "Load data into the configured storage backend",
	}
	cmd.AddCommand(
		newLoadArticlesCmd(),
		newLoadEmbeddingsCmd(),
	)
	return cmd
}
