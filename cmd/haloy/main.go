package main

import (
	"fmt"
	"os"

	"github.com/haloydev/haloy/internal/haloy"
)

func main() {
	rootCmd := haloy.NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
