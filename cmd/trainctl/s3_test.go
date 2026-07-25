package main

import (
	"strings"
	"testing"
)

func TestParseS3URI(t *testing.T) {
	tests := []struct {
		name       string
		uri        string
		wantBucket string
		wantKey    string
		wantErr    string // substring the error must contain; empty means success
	}{
		{
			// The shape register actually receives back from DescribeTrainingJob.
			name:       "model artifact uri",
			uri:        "s3://retrain-pipeline-model-artifacts-646278323015/output/retrain-pipeline-0e703d3d-b307ebc/output/model.tar.gz",
			wantBucket: "retrain-pipeline-model-artifacts-646278323015",
			wantKey:    "output/retrain-pipeline-0e703d3d-b307ebc/output/model.tar.gz",
		},
		{
			name:       "single segment key",
			uri:        "s3://bucket/key.json",
			wantBucket: "bucket",
			wantKey:    "key.json",
		},
		{
			// A console URL or a bare path, the most likely operator mistake.
			name:    "missing scheme",
			uri:     "https://bucket.s3.amazonaws.com/key",
			wantErr: "not an s3 uri",
		},
		{
			// String concatenation against an unset variable produces this.
			name:    "empty bucket",
			uri:     "s3:///output/model.tar.gz",
			wantErr: "malformed s3 uri",
		},
		{
			name:    "no key",
			uri:     "s3://bucket",
			wantErr: "malformed s3 uri",
		},
		{
			// A prefix where an object was expected. Rejecting here beats a
			// confusing NoSuchKey from GetObject later.
			name:    "trailing slash, empty key",
			uri:     "s3://bucket/",
			wantErr: "malformed s3 uri",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bucket, key, err := parseS3URI(tc.uri)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("bucket = %q, key = %q, want an error containing %q", bucket, key, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("parseS3URI(%q): %v", tc.uri, err)
			}
			if bucket != tc.wantBucket {
				t.Errorf("bucket = %q, want %q", bucket, tc.wantBucket)
			}
			if key != tc.wantKey {
				t.Errorf("key = %q, want %q", key, tc.wantKey)
			}
		})
	}
}
