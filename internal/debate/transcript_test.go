package debate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTranscript_RecordsTurnsAndRuling(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	tr := OpenTranscript(path)
	tr.RecordTurn(TurnEvent{Role: LabelProposer, Agent: "alice", Model: "m-a", Turn: 1, Statement: "defends"})
	tr.RecordRuling(RulingEvent{Outcome: "uphold", SettledSeverity: "HIGH", Reasoning: "ok"})
	require.NoError(t, tr.Close())

	roles := transcriptRoles(t, path)
	assert.Equal(t, []string{LabelProposer}, roles)
}

func TestTranscript_DisabledWriterIsNoOp(t *testing.T) {
	// An unwritable path yields a disabled writer whose methods never panic.
	path := filepath.Join(t.TempDir(), "nope", "transcript.jsonl")
	tr := OpenTranscript(path)
	tr.RecordTurn(TurnEvent{Role: LabelJudge})
	tr.RecordRuling(RulingEvent{Outcome: "overturn"})
	assert.NoError(t, tr.Close())
	// Verify the file was NOT created (disabled writer should not write).
	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err), "disabled writer must not create the transcript file")
}

func TestTranscript_NilReceiverSafe(t *testing.T) {
	var tr *Transcript
	assert.NotPanics(t, func() {
		tr.RecordTurn(TurnEvent{})
		tr.RecordRuling(RulingEvent{})
		_ = tr.Close()
	})
}
