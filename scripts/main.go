package main

import (
	"os"

	"github.com/spf13/cobra"
)

func main() {
	command := &cobra.Command{
		Use:          "scripts",
		Short:        "Build panascais/ci-rust images and update their configuration",
		SilenceUsage: true,
	}
	command.AddCommand(buildCommand(), mergeCommand(), updateCommand())
	command.CompletionOptions.DisableDefaultCmd = true

	if command.Execute() != nil {
		os.Exit(1)
	}
}
