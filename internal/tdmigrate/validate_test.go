package tdmigrate

import "testing"

func TestValidateDir_MissingDirErrors(t *testing.T) {
	if _, err := ValidateDir("/no/such/dir/resolve-td-test"); err == nil {
		t.Error("expected error when validating a missing directory")
	}
}

func TestLoadShards_MissingDirErrors(t *testing.T) {
	if _, err := LoadShards("/no/such/dir/resolve-td-test"); err == nil {
		t.Error("expected error when loading shards from a missing directory")
	}
}
