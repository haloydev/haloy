package main

import (
	"fmt"
	"os"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/haloyproxy"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}

	switch cmd {
	case "serve":
		debug := os.Getenv(constants.EnvVarDebug) == "true"
		if err := haloyproxy.Run(debug); err != nil {
			fmt.Fprintf(os.Stderr, "haloy-proxy: %v\n", err)
			os.Exit(1)
		}
	case "version":
		fmt.Println(constants.Version)
	default:
		fmt.Fprintf(os.Stderr, "haloy-proxy: unknown command %q (available: serve, version)\n", cmd)
		os.Exit(2)
	}
}
