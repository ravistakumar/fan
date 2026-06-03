package main

import (
	"fmt"
	"os"

	"github.com/ravistakumar/fan/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "fan:", err)
		os.Exit(1)
	}
}
