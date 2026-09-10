package main

import (
	_ "embed"

	"github.com/spf13/cobra"
)

// primeText is docs/prime.md, embedded; a test keeps the two identical.
//
//go:embed prime.md
var primeText string

func newPrimeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "prime",
		Short: "Print the rules an agent needs to use bn",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := cmd.OutOrStdout().Write([]byte(primeText))
			return err
		},
	}
}
