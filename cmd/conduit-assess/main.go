package main

import (
	"fmt"
	"io"
	"os"
)

const maxInput = 128 * 1024

func run(in io.Reader, out io.Writer) error {
	data, err := io.ReadAll(io.LimitReader(in, maxInput+1))
	if err != nil {
		return err
	}
	if len(data) > maxInput {
		return fmt.Errorf("assessment exceeds 128 KiB")
	}
	return decodeAndReview(data, out)
}
func main() {
	var err error
	if len(os.Args) == 1 {
		err = run(os.Stdin, os.Stdout)
	} else if os.Args[1] == "discover" {
		err = discover(os.Args[2:], os.Stdout)
	} else if os.Args[1] == "commit-evidence" {
		err = committedEvidence(os.Args[2:], os.Stdout)
	} else if os.Args[1] == "evidence" {
		err = evidence(os.Args[2:], os.Stdout)
	} else {
		err = fmt.Errorf("unknown command")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
