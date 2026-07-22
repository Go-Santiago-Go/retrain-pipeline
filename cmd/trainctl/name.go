package main

import "fmt"

// jobName derives a SageMaker training job name from the dataset hash and
// git SHA so identical inputs always produce the same name. That sameness is
// the idempotency mechanism: SageMaker rejects a duplicate name, so a re-run
// on unchanged data is refused by the service rather than billed again.
//
// Callers must pass a full-length md5 dataset hash and a 40-char git SHA;
// both come from DVC and `git rev-parse`, so the slice bounds are safe.
func jobName(datasetHash, gitSHA string) string {
	return fmt.Sprintf("retrain-pipeline-%s-%s", datasetHash[:8], gitSHA[:7])
}
