package main

import (
	"flag"
	"os"

	"github.com/haloydev/haloy/internal/constants"
	"github.com/haloydev/haloy/internal/haloyd"
)

func main() {
	debugFlag := flag.Bool("debug", false, "Run in debug mode (don't actually send commands to HAProxy)")
	flag.Parse()

	debugEnv := os.Getenv(constants.EnvVarDebug) == "true"
	debug := *debugFlag || debugEnv

	haloyd.Run(debug)
}
