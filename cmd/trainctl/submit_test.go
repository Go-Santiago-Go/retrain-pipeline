package main

import (
	"reflect"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sagemaker/types"
)

func TestBuildTags(t *testing.T) {
	got := buildTags("dhash", "gsha", "hhash")

	// Order and keys are the contract: the AWS side queries by these exact keys.
	want := []types.Tag{
		{Key: aws.String("dataset_hash"), Value: aws.String("dhash")},
		{Key: aws.String("git_sha"), Value: aws.String("gsha")},
		{Key: aws.String("holdout_hash"), Value: aws.String("hhash")},
	}

	// DeepEqual dereferences the *string fields; == would compare addresses.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("buildTags mismatch\n got: %+v\nwant: %+v", got, want)
	}
}
