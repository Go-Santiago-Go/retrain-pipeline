package main

import (
	"fmt"
	"strings"
)

// parseS3URI splits an s3://bucket/key URI into its two parts. The s3:// form is
// a CLI and tooling convention, not an API concept: the SDK takes Bucket and Key
// as separate fields, so the split has to happen on this side.
//
// Results are named because both are strings, and the names are what stop a
// caller from silently transposing them.
func parseS3URI(uri string) (bucket, key string, err error) {
	rest, found := strings.CutPrefix(uri, "s3://")
	if !found {
		return "", "", fmt.Errorf("not an s3 uri: %q", uri)
	}

	// Cut splits on the first separator, so the key keeps every remaining slash.
	bucket, key, found = strings.Cut(rest, "/")
	if !found || bucket == "" || key == "" {
		return "", "", fmt.Errorf("malformed s3 uri: %q", uri)
	}
	return bucket, key, nil
}
