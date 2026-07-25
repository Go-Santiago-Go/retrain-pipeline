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

// buildTags turns the lineage values into SageMaker tags. Tags make lineage
// queryable from the AWS side (search jobs by dataset_hash) without opening the
// job. The same values also go in as hyperparameters later; that redundancy is
// intentional. One side answers "which job trained on this data", the other
// answers "what did this job train on".
func buildTags(datasetHash, gitSHA, holdoutHash string) []types.Tag {
	return []types.Tag{
		{Key: aws.String("dataset_hash"), Value: aws.String(datasetHash)},
		{Key: aws.String("git_sha"), Value: aws.String(gitSHA)},
		{Key: aws.String("holdout_hash"), Value: aws.String(holdoutHash)},
	}
}

// submitParams collects the inputs CreateTrainingJob needs that are not fixed
// constants. In CI these come from Terraform outputs; locally, from flags.
// Bundling them keeps buildTrainingInput a pure function of one value, which is
// what makes it straightforward to table-test.
type submitParams struct {
	image        string // managed sklearn framework image URI
	sourceS3     string // s3:// path to sourcedir.tar.gz (injected code)
	inputS3      string // s3:// prefix of the training data channel
	outputS3     string // s3:// prefix for model.tar.gz and metrics.json
	instanceType string // e.g. ml.m5.large; never GPU on this budget
	datasetHash  string
	gitSHA       string
	holdoutHash  string
	jobName      string // derived from dataset hash + short SHA; the idempotency key
	roleARN      string // SageMaker execution role the job assumes
}

// buildTrainingInput assembles the CreateTrainingJob request. It is pure: no
// AWS calls, no clock, no environment, so a table test can assert the wiring.
func buildTrainingInput(p submitParams) *sagemaker.CreateTrainingJobInput {
	return &sagemaker.CreateTrainingJobInput{
		// Identity first: the derived name is what SageMaker rejects on a
		// duplicate, and RoleArn is the execution role the job runs as, distinct
		// from the CI role trainctl itself runs under.
		TrainingJobName: aws.String(p.jobName),
		RoleArn:         aws.String(p.roleARN),

		// AlgorithmSpecification names the stock sklearn image; the code that
		// actually runs is injected via the sagemaker_* hyperparameters below,
		// which is why no custom image or ECR repo is needed.
		AlgorithmSpecification: &types.AlgorithmSpecification{
			TrainingImage:     aws.String(p.image),
			TrainingInputMode: types.TrainingInputModeFile,
		},
		// Two kinds of keys: the sagemaker_* pair tells the container what code
		// to fetch and run (script mode), and the lineage keys put dataset and
		// commit inside the job's own record, the inside-proof half of the
		// double-carry whose outside half is buildTags.
		HyperParameters: map[string]string{
			"sagemaker_program":          "train.py",
			"sagemaker_submit_directory": p.sourceS3,
			"dataset_hash":               p.datasetHash,
			"git_sha":                    p.gitSHA,
			"holdout_hash":               p.holdoutHash,
		},
		// One channel named "train"; SageMaker exposes it to the container as
		// SM_CHANNEL_TRAIN, the exact env var train.py already reads.
		InputDataConfig: []types.Channel{
			{
				ChannelName: aws.String("train"),
				DataSource: &types.DataSource{
					S3DataSource: &types.S3DataSource{
						S3DataType:             types.S3DataTypeS3Prefix,
						S3Uri:                  aws.String(p.inputS3),
						S3DataDistributionType: types.S3DataDistributionFullyReplicated,
					},
				},
				ContentType: aws.String("text/csv"),
			},
		},
		// SageMaker appends the job name and output/ under this prefix, so a
		// bare bucket path is all it wants.
		OutputDataConfig: &types.OutputDataConfig{
			S3OutputPath: aws.String(p.outputS3),
		},
		// Smallest viable footprint, since this is the one phase that bills.
		// One CPU instance (type is a flag so CI can pin the cheapest current
		// SKU), and a 10 GB volume, which is ample for a few-thousand-row CSV.
		ResourceConfig: &types.ResourceConfig{
			InstanceType:   types.TrainingInstanceType(p.instanceType),
			InstanceCount:  aws.Int32(1),
			VolumeSizeInGB: aws.Int32(10),
		},
		// Hard wall-clock ceiling so a hung job cannot bill open-ended. The fit
		// finishes in seconds, so 20 minutes never trips in normal operation;
		// it exists only to cap the blast radius of a pathological hang.
		StoppingCondition: &types.StoppingCondition{
			MaxRuntimeInSeconds: aws.Int32(1200),
		},

		// The outside half of the double-carry: same lineage as the
		// hyperparameters, but queryable from the AWS side without opening the job.
		Tags: buildTags(p.datasetHash, p.gitSHA, p.holdoutHash),
	}
}
