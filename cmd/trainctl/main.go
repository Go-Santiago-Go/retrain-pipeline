package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker/types"
)

func main() {
	if len(os.Args) < 2 { // nothing after the binary name
		fmt.Fprintln(os.Stderr, "usage: trainctl <submit|register>")
		os.Exit(2)
	}

	// Subcommands return errors rather than exiting themselves; main owns the
	// process exit so error handling lives in one place and stays testable.
	var err error
	switch os.Args[1] {
	case "submit":
		err = runSubmit(os.Args[2:])
	case "register":
		fmt.Println("register") // stubbed until Phase 6 (Model Registry)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", os.Args[1])
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "trainctl:", err)
		os.Exit(1)
	}
}

func runSubmit(args []string) error {
	// A FlagSet per subcommand so submit and register keep independent flags.
	// The S3 paths and role ARN carry no defaults: they come from Terraform
	// outputs in CI and must be passed explicitly. Only instance-type defaults,
	// and it defaults small (CPU) so a bare submit can never pick a billing surprise.
	fs := flag.NewFlagSet("submit", flag.ExitOnError)
	execRole := fs.String("execution-role", "", "SageMaker execution role ARN")
	image := fs.String("image", "", "managed sklearn framework image URI")
	bucket := fs.String("bucket", "", "model-artifacts bucket name, no scheme; code/, input/, output/ prefixes derive from it")
	instanceType := fs.String("instance-type", "ml.m5.large", "CPU instance type; no GPU")
	fs.Parse(args)

	// The pointer paths are fixed: this pipeline trains on one dataset against
	// one frozen holdout, and their DVC pointers are what git versions.
	hash, err := datasetHash("data/train.csv.dvc")
	if err != nil {
		return err
	}
	holdoutHash, err := datasetHash("data/holdout.csv.dvc")
	if err != nil {
		return err
	}

	sha, err := gitSHA()
	if err != nil {
		return err
	}
	name := jobName(hash, sha)

	// The three S3 paths are not independent inputs: they are one bucket plus a
	// fixed layout the CLI owns, so CI wires up a single --bucket. Folding the
	// dataset hash into the input prefix here is the payoff: the bucket value
	// stays stable across dataset versions, and no one hand-edits a path when the
	// data changes. hash is the full pointer md5, the same value jobName truncates.
	sourceS3 := fmt.Sprintf("s3://%s/code/sourcedir.tar.gz", *bucket)
	inputS3 := fmt.Sprintf("s3://%s/input/%s/", *bucket, hash)
	outputS3 := fmt.Sprintf("s3://%s/output/", *bucket)

	// Submitting the job is a fast control-plane call, so a short timeout is
	// right here. Waiting for training to finish is a separate poll loop with
	// its own, much longer budget.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// LoadDefaultConfig walks the credential chain: SSO locally, the OIDC
	// web-identity role in CI. The same binary authenticates in both.
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load aws config: %w", err)
	}
	client := sagemaker.NewFromConfig(cfg)

	p := submitParams{
		image:        *image,
		sourceS3:     sourceS3,
		inputS3:      inputS3,
		outputS3:     outputS3,
		instanceType: *instanceType,
		datasetHash:  hash,
		gitSHA:       sha,
		holdoutHash:  holdoutHash,
		jobName:      name,
		roleARN:      *execRole,
	}

	// The pure builder assembles the request; the client submits it. This is a
	// fast control-plane call that returns as soon as the job is accepted, not
	// when training finishes. A duplicate name (unchanged data + code) is
	// rejected here by the service, which is the idempotency guarantee.
	out, err := client.CreateTrainingJob(ctx, buildTrainingInput(p))
	if err != nil {
		return fmt.Errorf("create training job %q: %w", name, err)
	}
	fmt.Println("submitted:", *out.TrainingJobArn)

	// Watching to completion is a separate, much longer budget than the submit.
	// The job's StoppingCondition caps it at 20 minutes, so a 30-minute client
	// deadline stays looser than that server-side ceiling: SageMaker owns the
	// authoritative stop, and the client never abandons a still-billing job.
	pollCtx, pollCancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer pollCancel()

	// This error means the watch itself failed (deadline, or a Describe call
	// broke), not that training failed. A failed job returns a successful poll
	// carrying a Failed status, handled just below.
	desc, err := pollTrainingJob(pollCtx, client, name)
	if err != nil {
		return fmt.Errorf("poll training job %q: %w", name, err)
	}

	// Policy lives with the caller: anything but Completed is a failure. Return
	// an error rather than calling os.Exit, so main owns the single nonzero exit
	// and CI goes red. FailureReason is nil on a Stopped job; ToString is the
	// nil-safe read.
	if desc.TrainingJobStatus != types.TrainingJobStatusCompleted {
		fmt.Fprintf(os.Stderr, "training job %q ended %s: %s\n",
			name, desc.TrainingJobStatus, aws.ToString(desc.FailureReason))
		fmt.Fprintln(os.Stderr, "logs:", sagemakerJobURL(cfg.Region, name))
		return fmt.Errorf("training job %q did not complete", name)
	}
	fmt.Println("completed:", name)
	return nil
}

// pollTrainingJob blocks until the job reaches a terminal state or ctx expires.
// It returns the full terminal Describe output so the caller owns policy (what
// counts as failure, what to print, what exit code); the poller only owns the
// mechanism of watching to a terminal state.
func pollTrainingJob(ctx context.Context, client *sagemaker.Client, name string) (*sagemaker.DescribeTrainingJobOutput, error) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		out, err := client.DescribeTrainingJob(ctx, &sagemaker.DescribeTrainingJobInput{
			TrainingJobName: aws.String(name),
		})
		if err != nil {
			return nil, fmt.Errorf("describe training job %q: %w", name, err)
		}

		switch out.TrainingJobStatus {
		case types.TrainingJobStatusCompleted,
			types.TrainingJobStatusFailed,
			types.TrainingJobStatusStopped:
			return out, nil // terminal, stop polling
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-ticker.C:
			// tick, loop, describe again
		}
	}
}

// sagemakerJobURL links to the training job's console page, which carries a
// native "View logs" link. Preferred over hand-building a CloudWatch deep link:
// this route is stable, where the console's log-fragment encoding is not.
func sagemakerJobURL(region, jobName string) string {
	return fmt.Sprintf(
		"https://%s.console.aws.amazon.com/sagemaker/home?region=%s#/jobs/%s",
		region, region, jobName,
	)
}
