package main

import (
	"fmt"
	"os"
)

// main is dispatch only: it maps a subcommand name onto its implementation and
// owns the process exit. Each subcommand lives in its own file and returns an
// error rather than exiting, so error handling stays in one place and the
// implementations stay testable.
func main() {
	if len(os.Args) < 2 { // nothing after the binary name
		fmt.Fprintln(os.Stderr, "usage: trainctl <submit|register>")
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "submit":
		err = runSubmit(os.Args[2:])
	case "register":
		err = runRegister(os.Args[2:])
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "trainctl:", err)
		os.Exit(1)
	}
}
