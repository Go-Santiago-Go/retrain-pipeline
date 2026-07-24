package main

import "testing"

func TestJobName(t *testing.T) {
	tests := []struct {
		name        string
		datasetHash string
		gitSHA      string
		want        string
	}{
		{
			name:        "real dvc hash and full sha",
			datasetHash: "0e703d3d09d8b2a169961d290992e859", // md5 from train.csv.dvc
			gitSHA:      "7b8cc2b1e0b208c63f00abcdef012345678",
			want:        "retrain-pipeline-0e703d3d-7b8cc2b",
		},
		{
			name:        "different dataset, same commit, changes the name",
			datasetHash: "ffffffff09d8b2a169961d290992e859", // first 8 differ
			gitSHA:      "7b8cc2b1e0b208c63f00abcdef012345678",
			want:        "retrain-pipeline-ffffffff-7b8cc2b",
		},
		{
			name:        "same dataset, different commit, changes the name",
			datasetHash: "0e703d3d09d8b2a169961d290992e859",
			gitSHA:      "aaaaaaa1e0b208c63f00abcdef012345678", // first 7 differ
			want:        "retrain-pipeline-0e703d3d-aaaaaaa",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jobName(tt.datasetHash, tt.gitSHA)
			if got != tt.want {
				t.Errorf("jobName() = %q, want %q", got, tt.want)
			}
		})
	}
}
