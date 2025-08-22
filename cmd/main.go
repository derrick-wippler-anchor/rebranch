package main

import (
	"fmt"
	"os"

	"rebranch"
)

func main() {
	if err := rebranch.RunCmd(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}