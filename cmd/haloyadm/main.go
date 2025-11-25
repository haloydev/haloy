package main

import (
	"os"

	"github.com/haloydev/haloy/internal/haloyadm"
)

func main() {
	os.Exit(haloyadm.Execute())
}
