package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker/types"
)

func runRegister(args []string) error {
	// Only two flags, and both are facts from outside the CLI: the group comes
	// from a Terraform output and the image is an AWS-published URI. Everything
	// else (job name, artifact location, metrics) is derived from what this
	// pipeline already owns, so there is nothing for CI to restate and get wrong.
	fs := flag.NewFlagSet("register", flag.ExitOnError)
	group := fs.String("group", "", "SageMaker model package group name")
	image := fs.String("image", "", "container image URI that can serve the artifact")
	fs.Parse(args)

	// Same two inputs submit reads, from the same committed pointer and commit,
	// so both subcommands resolve the identical job name without CI restating it.
	hash, err := datasetHash("data/train.csv.dvc")
	if err != nil {
		return err
	}
	sha, err := gitSHA()
	if err != nil {
		return err
	}
	name := jobName(hash, sha)

	// Describe is a fast control-plane read, so it gets submit's short budget
	// rather than the poller's long one.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	// Named by service because this function ends up holding two clients; in
	// submit, where there is only one, a bare "client" is unambiguous.
	sagemakerClient := sagemaker.NewFromConfig(cfg)

	// The service is authoritative for where the artifact landed. Asking it beats
	// rebuilding SageMaker's output path convention on this side, which would go
	// stale silently if AWS changed the layout.
	desc, err := sagemakerClient.DescribeTrainingJob(ctx, &sagemaker.DescribeTrainingJobInput{
		TrainingJobName: aws.String(name),
	})
	if err != nil {
		return fmt.Errorf("describe training job %q: %w", name, err)
	}

	// Registering a job that did not complete would put an unservable artifact in
	// front of a human reviewer, so refuse before the registry ever sees it.
	// This check also guarantees ModelArtifacts is populated: it is nil until a
	// job succeeds, so the deref below is safe only on this side of the guard.
	if desc.TrainingJobStatus != types.TrainingJobStatusCompleted {
		return fmt.Errorf("training job %q is %s, not Completed", name, desc.TrainingJobStatus)
	}

	modelURI := aws.ToString(desc.ModelArtifacts.S3ModelArtifacts)

	// Same cfg, different service: one credential chain, two service clients.
	// That pairing is the normal SDK v2 shape.
	s3Client := s3.NewFromConfig(cfg)

	bucket, key, err := parseS3URI(modelURI)
	if err != nil {
		return err
	}

	object, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("get %s: %w", modelURI, err)
	}

	// Body is a live HTTP response, not a buffer. Closing it returns the
	// connection to the pool; skipping it leaks one per call.
	defer object.Body.Close()

	// The payoff for extractFile taking an io.Reader: the response body goes
	// straight in. No temp file, no buffering the compressed bytes, and only the
	// one file we asked for is ever materialized.
	metricsJSON, err := extractFile(object.Body, "metrics.json")
	if err != nil {
		return fmt.Errorf("extract metrics from %s: %w", modelURI, err)
	}

	// Printing the candidate's scores puts them in the CI log next to the run
	// that produced them, so the approval decision is reviewable from the
	// workflow output and not only from the console.
	fmt.Printf("candidate metrics: %s\n", metricsJSON)

	// ModelMetrics takes an S3 URI, not numbers, so the copy sealed inside
	// model.tar.gz has to be republished as its own object. The tarball stays the
	// source of truth; this is a projection for the registry to render.
	//
	// Keyed by job name because metrics describe a run, not a dataset: two commits
	// training on identical data produce different scores, and a dataset-keyed key
	// would silently overwrite the earlier run's numbers under a package that had
	// already been registered against them. Same bucket the artifact came from, so
	// no second flag has to agree with the first.
	metricsKey := fmt.Sprintf("metrics/%s.json", name)
	_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(metricsKey),
		// Body is an io.Reader, so the bytes need a reader around them.
		Body: bytes.NewReader(metricsJSON),
		// Set explicitly so the console renders it inline rather than offering a
		// download of an octet-stream.
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		return fmt.Errorf("put metrics to s3://%s/%s: %w", bucket, metricsKey, err)
	}

	metricsURI := fmt.Sprintf("s3://%s/%s", bucket, metricsKey)
	fmt.Println("metrics published:", metricsURI)

	// The pure builder assembles the request; the client submits it. Same split
	// as submit, so the wiring is asserted by table tests and only the call
	// itself needs AWS.
	pkg, err := sagemakerClient.CreateModelPackage(ctx, buildModelPackageInput(registerParams{
		groupName:   *group,
		image:       *image,
		modelURI:    modelURI,
		metricsURI:  metricsURI,
		datasetHash: hash,
		gitSHA:      sha,
		jobName:     name,
	}))
	if err != nil {
		return fmt.Errorf("create model package in %q: %w", *group, err)
	}

	// The ARN ends in the version number SageMaker assigned, so this line is what
	// tells a reviewer which version to open. Printing it is the handoff from the
	// automated half of the pipeline to the human half.
	fmt.Println("registered (PendingManualApproval):", aws.ToString(pkg.ModelPackageArn))
	return nil
}

// registerParams collects what CreateModelPackage needs that is not a fixed
// constant. Bundling them keeps buildModelPackageInput a pure function of one
// value, which is what makes the request wiring table-testable without AWS.
type registerParams struct {
	groupName   string // model package group the version lands in
	image       string // container that can serve this artifact
	modelURI    string // s3:// path to model.tar.gz, from DescribeTrainingJob
	metricsURI  string // s3:// path to the republished metrics.json
	datasetHash string
	gitSHA      string
	jobName     string // the run that produced the artifact
}

// buildModelPackageInput assembles the CreateModelPackage request. It is pure:
// no AWS calls, no clock, no environment, so a table test can assert the wiring.
//
// Tags are deliberately absent. CreateModelPackage with tags requires
// sagemaker:AddTags, and the CI role grants that only on training-job ARNs, so
// tagging here would fail at runtime. Lineage rides in the metadata properties
// instead, which CreateModelPackage covers on its own.
func buildModelPackageInput(p registerParams) *sagemaker.CreateModelPackageInput {
	return &sagemaker.CreateModelPackageInput{
		// Naming the group rather than the package is what makes this a versioned
		// model: SageMaker assigns the next version number within the group.
		ModelPackageGroupName:   aws.String(p.groupName),
		ModelPackageDescription: aws.String("spam classifier from training job " + p.jobName),

		// The governance gate, and the reason this project exists. Registering as
		// Approved would let a model reach a promotable state with no human having
		// read a metric, and nothing would fail or log. Changing this constant
		// removes the human from human-in-the-loop.
		ModelApprovalStatus: types.ModelApprovalStatusPendingManualApproval,

		// Promotion has to be executable, not just recorded, so the package carries
		// the artifact plus a container that can serve it. The managed sklearn
		// image ships both training and serving entry points, which is why the same
		// URI submit trains with also appears here.
		InferenceSpecification: &types.InferenceSpecification{
			Containers: []types.ModelPackageContainerDefinition{{
				Image:        aws.String(p.image),
				ModelDataUrl: aws.String(p.modelURI),
			}},
			SupportedContentTypes:      []string{"text/csv"},
			SupportedResponseMIMETypes: []string{"text/csv"},
		},

		// ModelQuality.Statistics is where the console reads eval numbers from, so
		// this is what turns the registry entry into something a reviewer can judge
		// rather than a pointer to an opaque tarball.
		ModelMetrics: &types.ModelMetrics{
			ModelQuality: &types.ModelQuality{
				Statistics: &types.MetricsSource{
					ContentType: aws.String("application/json"),
					S3Uri:       aws.String(p.metricsURI),
				},
			},
		},

		// The traceability claim, made queryable. These three answer "which data,
		// which commit, which run" from the registry entry alone, with no need to
		// open the training job or unpack the artifact.
		CustomerMetadataProperties: map[string]string{
			"dataset_hash":      p.datasetHash,
			"git_sha":           p.gitSHA,
			"training_job_name": p.jobName,
		},
	}
}

// extractFile pulls one named file out of a gzipped tar stream, matching on the
// entry's base name. It takes an io.Reader rather than an S3 client so a table
// test can hand it an in-memory archive; the AWS call stays with the caller.
//
// A .tar.gz is two layers, so it unwraps in reverse: gzip comes off first and a
// tar archive is underneath. Composing readers keeps the whole thing streaming,
// with no temp file and no buffer of the compressed bytes.
func extractFile(compressed io.Reader, targetName string) ([]byte, error) {
	decompressed, err := gzip.NewReader(compressed)
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer decompressed.Close()

	// The tar reader is both the cursor over entries and the reader for the
	// current entry's bytes, which is why the body read below targets it too.
	archive := tar.NewReader(decompressed)
	for {
		entry, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("%s not found in archive", targetName)
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}

		// SageMaker tars the model dir flat, but entries can still carry a "./"
		// prefix, so compare base names rather than the raw header name.
		if path.Base(entry.Name) != targetName {
			continue
		}

		// Bounded read. The artifact is trusted, but a header claiming a huge
		// size should not get to allocate unbounded memory.
		return io.ReadAll(io.LimitReader(archive, 1<<20))
	}
}
