package main

import (
	"fmt"
	"os"

	"github.com/haloydev/haloy/internal/haloyadm"
)

func main() {
	rootCmd := haloyadm.NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
