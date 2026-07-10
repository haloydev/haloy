package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/haloyproxy"
	"github.com/haloydev/haloy/internal/proxywire"
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
		if len(os.Args) > 2 {
			if len(os.Args) != 3 || os.Args[2] != "--json" {
				fmt.Fprintln(os.Stderr, "haloy-proxy: usage: haloy-proxy version [--json]")
				os.Exit(2)
			}
			if err := json.NewEncoder(os.Stdout).Encode(struct {
				Version       string `json:"version"`
				Generation    int    `json:"proxy_generation"`
				SchemaVersion int    `json:"proxy_schema_version"`
			}{
				Version:       constants.Version,
				Generation:    proxywire.ProxyGeneration,
				SchemaVersion: proxywire.SchemaVersion,
			}); err != nil {
				fmt.Fprintf(os.Stderr, "haloy-proxy: encode version metadata: %v\n", err)
				os.Exit(1)
			}
			return
		}
		fmt.Println(constants.Version)
	default:
		fmt.Fprintf(os.Stderr, "haloy-proxy: unknown command %q (available: serve, version)\n", cmd)
		os.Exit(2)
	}
}
