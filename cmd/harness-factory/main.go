package main

import (
	"fmt"
	"os"
)

var version = "0.1.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--version" {
		fmt.Println(version)
		os.Exit(0)
	}
	// TODO: ACP stdio JSON-RPC loop
	fmt.Fprintln(os.Stderr, "harness-factory", version, "— waiting for ACP JSON-RPC on stdin")
}
