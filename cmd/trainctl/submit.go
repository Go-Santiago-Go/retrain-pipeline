package main

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker/types"
)

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
