package history

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeRecord(t *testing.T, b *strings.Builder, r Record) {
	t.Helper()
	data, err := json.Marshal(r)
	require.NoError(t, err)
	b.Write(data)
	b.WriteByte('\n')
}

func TestLoad_SkipsOversizedLine(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "findings-history.jsonl")

	rec1 := Record{Timestamp: time.Now(), Package: "pkg/a", Severity: "HIGH", ID: "id1", File: "a.go", Category: "correctness"}
	rec2 := Record{Timestamp: time.Now(), Package: "pkg/b", Severity: "LOW", ID: "id2", File: "b.go", Category: "style"}

	b := &strings.Builder{}
	writeRecord(t, b, rec1)
	b.WriteString(strings.Repeat("x", 2*1024*1024) + "\n")
	writeRecord(t, b, rec2)

	require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))

	got, err := Load(path)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, rec1.ID, got[0].ID)
	assert.Equal(t, rec2.ID, got[1].ID)
}

func TestLoad_OnlyOversizedLineReturnsEmpty(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "findings-history.jsonl")

	content := strings.Repeat("x", 2*1024*1024) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	got, err := Load(path)
	require.NoError(t, err)
	assert.Empty(t, got, "file with only oversized line must return empty slice")
}
