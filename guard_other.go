//go:build !darwin

package main

import (
	"fmt"
	"os"
)

func runGuard(args []string) {
	fmt.Fprintf(os.Stderr, "The guard command is only supported on macOS.\n")
	os.Exit(1)
}
