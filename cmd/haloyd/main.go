package main

import (
	"os"

	"github.com/haloydev/haloy/internal/haloydcli"
)

func main() {
	os.Exit(haloydcli.Execute())
}
