package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// dvcPointer mirrors the shape of a .dvc file. The yaml tags map the lowercase
// YAML keys onto exported Go fields. Outs is a list because DVC can track
// several outputs per pointer; we read the first.
type dvcPointer struct {
	Outs []struct {
		MD5 string `yaml:"md5"`
	} `yaml:"outs"`
}

// datasetHash reads the md5 DVC recorded for the tracked file. This md5, not a
// re-hash of the CSV, is the source of truth for which dataset a run trains on.
func datasetHash(pointerPath string) (string, error) {
	data, err := os.ReadFile(pointerPath)
	if err != nil {
		return "", fmt.Errorf("read pointer %s: %w", pointerPath, err)
	}

	var p dvcPointer
	if err := yaml.Unmarshal(data, &p); err != nil {
		return "", fmt.Errorf("parse pointer %s: %w", pointerPath, err)
	}
	if len(p.Outs) == 0 {
		return "", fmt.Errorf("pointer %s has no outs", pointerPath)
	}
	return p.Outs[0].MD5, nil
}
