package config

import "testing"

func TestValidateYAMLSubsetRejectsAnchors(t *testing.T) {
	t.Parallel()
	input := []byte("sources:\n  - &base {name: work, path: ./work}\n")
	if err := ValidateYAMLSubset(input); err == nil {
		t.Fatalf("expected anchor rejection")
	}
}

func TestValidateYAMLSubsetRejectsMergeKeys(t *testing.T) {
	t.Parallel()
	input := []byte("root:\n  <<: {name: work}\n")
	if err := ValidateYAMLSubset(input); err == nil {
		t.Fatalf("expected merge key rejection")
	}
}

func TestValidateYAMLSubsetRejectsCustomTags(t *testing.T) {
	t.Parallel()
	input := []byte("value: !custom tagged\n")
	if err := ValidateYAMLSubset(input); err == nil {
		t.Fatalf("expected custom tag rejection")
	}
}
