package main

import (
	"os"

	"github.com/jonathanagustin/conduit/internal/highsshim"
)

func main() {
	if err := highsshim.Run(os.Stdin, os.Stdout, highsshim.NewEngine()); err != nil {
		// The shim has no diagnostic output channel: the digest-bound caller
		// observes only success/failure and the strict stdout envelope.
		os.Exit(1)
	}
}
