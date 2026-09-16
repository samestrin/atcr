package benchmark

import (
	"context"
	"errors"
)

// errNotImplemented is the deliberate wrong answer this stub returns so the RED
// test compiles and fails for a real reason.
var errNotImplemented = errors.New("materialization not implemented")

// MaterializedCase is a repo-state case turned into a real git repository: the
// base tree as one commit, the change applied on top as a second commit carrying
// the case's commit message.
type MaterializedCase struct {
	Root    string
	BaseSHA string
	HeadSHA string
}

// MaterializeCase builds c's repository under dest.
func MaterializeCase(ctx context.Context, c RepoStateCase, dest string) (*MaterializedCase, error) {
	return nil, errNotImplemented
}
