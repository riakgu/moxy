package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/riakgu/moxy/internal/delivery/cli"
	"github.com/riakgu/moxy/web"
)

var rootCmd = &cobra.Command{
	Use:   "moxy",
	Short: "Multi-IP proxy server using network namespace slots",
}

func init() {
	rootCmd.AddCommand(cli.NewServeCommand(web.StaticFS))
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
