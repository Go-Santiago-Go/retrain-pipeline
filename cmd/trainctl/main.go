package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 { // nothing after the binary name
		fmt.Fprintln(os.Stderr, "usage: trainctl <submit|register>")
		os.Exit(2)
	}

	switch os.Args[1] {
	case "submit":
		runSubmit(os.Args[2:])
	case "register":
		fmt.Println("register")
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		os.Exit(2)
	}
}

func runSubmit(args []string) {
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	execRole := fs.String("execution-role", "", "SageMaker execution role ARN")
	fs.Parse(args)

	hash, err := datasetHash("data/train.csv.dvc")
	if err != nil {
		fmt.Fprintln(os.Stderr, "submit:", err)
		os.Exit(1)
	}

	sha, err := gitSHA()
	if err != nil {
		fmt.Fprintln(os.Stderr, "submit:", err)
		os.Exit(1)
	}
	name := jobName(hash, sha)
	fmt.Println("derived job name:", name)
	fmt.Println("submit, execution-role=", *execRole)
}
