package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// gitSHA returns the full commit SHA of HEAD by shelling out to git. The repo
// is a real git checkout both locally and in the Actions runner, so the same
// command answer in both. jobName truncates this to its short form
func gitSHA() (string, error) {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD: %w", err)
	}
	// Output() keeps the trailing newline git prints; trim it so the SHA is clean.
	return strings.TrimSpace(string(out)), nil
}
