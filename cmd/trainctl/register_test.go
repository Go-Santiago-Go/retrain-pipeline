package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker/types"
)

// makeTarGz builds a gzipped tar in memory, the mirror image of what
// extractFile unwraps. Building the fixture in code rather than committing a
// binary testdata file keeps each case's contents visible at the assertion.
func makeTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()

	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	archive := tar.NewWriter(gzipWriter)

	for name, body := range files {
		header := &tar.Header{Name: name, Mode: 0o600, Size: int64(len(body))}
		if err := archive.WriteHeader(header); err != nil {
			t.Fatalf("write header %s: %v", name, err)
		}
		if _, err := archive.Write([]byte(body)); err != nil {
			t.Fatalf("write body %s: %v", name, err)
		}
	}

	// Order matters and both are required: the tar writer flushes its footer,
	// then the gzip writer flushes its trailer. Skipping either produces a
	// truncated stream that fails to read back.
	if err := archive.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return compressed.Bytes()
}

func TestExtractFile(t *testing.T) {
	metrics := `{"accuracy":0.97,"f1":0.90}`

	tests := []struct {
		name    string
		files   map[string]string
		target  string
		want    string
		wantErr string // substring the error must contain; empty means success
	}{
		{
			name:   "flat entry name",
			files:  map[string]string{"metrics.json": metrics, "model.joblib": "binary"},
			target: "metrics.json",
			want:   metrics,
		},
		{
			// SageMaker's tar can carry a "./" prefix, which is why extractFile
			// compares base names instead of the raw header name.
			name:   "dot-slash prefixed entry",
			files:  map[string]string{"./metrics.json": metrics},
			target: "metrics.json",
			want:   metrics,
		},
		{
			name:    "target absent",
			files:   map[string]string{"model.joblib": "binary"},
			target:  "metrics.json",
			wantErr: "not found in archive",
		},
		{
			name:    "empty archive",
			files:   map[string]string{},
			target:  "metrics.json",
			wantErr: "not found in archive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractFile(bytes.NewReader(makeTarGz(t, tc.files)), tc.target)

			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("contents = %q, want an error containing %q", got, tc.wantErr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error = %q, want it to contain %q", err, tc.wantErr)
				}
				return
			}

			if err != nil {
				t.Fatalf("extractFile: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("contents = %q, want %q", got, tc.want)
			}
		})
	}
}

// testParams is the shape runRegister assembles at runtime, used as the base for
// the builder tests so each one varies only what it asserts on.
func testParams() registerParams {
	return registerParams{
		groupName:   "retrain-pipeline-models",
		image:       "683313688378.dkr.ecr.us-east-1.amazonaws.com/sagemaker-scikit-learn:1.4-2-cpu-py3",
		modelURI:    "s3://artifacts/output/retrain-pipeline-0e703d3d-b307ebc/output/model.tar.gz",
		metricsURI:  "s3://artifacts/metrics/retrain-pipeline-0e703d3d-b307ebc.json",
		datasetHash: "0e703d3d1f4a8c92b6d5e0a7c3f19b84",
		gitSHA:      "b307ebc9a1d4f2e8c6b5a3970d1e2f4c8a6b5d3e",
		jobName:     "retrain-pipeline-0e703d3d-b307ebc",
	}
}

// This is a policy assertion, not a plumbing one. A model package registered as
// Approved would be promotable with no human having read a metric, and nothing
// would fail or log; the guarantee would be gone silently. Anyone weakening it
// has to delete a test that says so.
func TestBuildModelPackageInputIsPendingManualApproval(t *testing.T) {
	got := buildModelPackageInput(testParams()).ModelApprovalStatus

	if got != types.ModelApprovalStatusPendingManualApproval {
		t.Fatalf("ModelApprovalStatus = %q, want %q: registering as anything else "+
			"removes the human from human-in-the-loop",
			got, types.ModelApprovalStatusPendingManualApproval)
	}
}

// Guards the traceability claim. If lineage silently stopped being attached,
// every model would still register cleanly and the gap would only surface the
// day someone needed to trace a bad model back to its rows.
func TestBuildModelPackageInputCarriesLineage(t *testing.T) {
	p := testParams()
	metadata := buildModelPackageInput(p).CustomerMetadataProperties

	want := map[string]string{
		"dataset_hash":      p.datasetHash,
		"git_sha":           p.gitSHA,
		"training_job_name": p.jobName,
	}
	for key, wantValue := range want {
		gotValue, ok := metadata[key]
		if !ok {
			t.Errorf("metadata is missing %q", key)
			continue
		}
		if gotValue != wantValue {
			t.Errorf("metadata[%q] = %q, want %q", key, gotValue, wantValue)
		}
	}
}

// The two S3 URIs are the ones a wrong nesting would misplace without any
// compile error, since both live several structs deep.
func TestBuildModelPackageInputWiresArtifactAndMetrics(t *testing.T) {
	p := testParams()
	input := buildModelPackageInput(p)

	if input.InferenceSpecification == nil || len(input.InferenceSpecification.Containers) != 1 {
		t.Fatalf("want exactly one inference container, got %#v", input.InferenceSpecification)
	}
	container := input.InferenceSpecification.Containers[0]
	if aws.ToString(container.ModelDataUrl) != p.modelURI {
		t.Errorf("ModelDataUrl = %q, want %q", aws.ToString(container.ModelDataUrl), p.modelURI)
	}
	if aws.ToString(container.Image) != p.image {
		t.Errorf("Image = %q, want %q", aws.ToString(container.Image), p.image)
	}

	// The console reads eval numbers from ModelQuality.Statistics, so a package
	// with the URI nested anywhere else renders as having no metrics at all.
	if input.ModelMetrics == nil || input.ModelMetrics.ModelQuality == nil ||
		input.ModelMetrics.ModelQuality.Statistics == nil {
		t.Fatalf("want metrics under ModelQuality.Statistics, got %#v", input.ModelMetrics)
	}
	if got := aws.ToString(input.ModelMetrics.ModelQuality.Statistics.S3Uri); got != p.metricsURI {
		t.Errorf("metrics S3Uri = %q, want %q", got, p.metricsURI)
	}

	// Version numbering comes from the group, so a package built without it would
	// be an unversioned model package and never appear in the registry group.
	if aws.ToString(input.ModelPackageGroupName) != p.groupName {
		t.Errorf("ModelPackageGroupName = %q, want %q",
			aws.ToString(input.ModelPackageGroupName), p.groupName)
	}
}

// A non-gzip stream must fail at the gzip layer rather than being mistaken for
// an empty archive, so the caller can tell "wrong bytes" from "missing file".
func TestExtractFileRejectsNonGzip(t *testing.T) {
	_, err := extractFile(strings.NewReader("not a gzip stream"), "metrics.json")
	if err == nil {
		t.Fatal("expected an error on a non-gzip stream, got nil")
	}
	if !strings.Contains(err.Error(), "gunzip") {
		t.Errorf("error = %q, want it to contain %q", err, "gunzip")
	}
}
