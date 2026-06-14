

HIGH|internal/fanout/review.go:343|Missing nil check for p.manifest.Review before dereference|Add nil check before setting fields|error-handling|5|p.manifest.Review.SnapshotMode = snapMode|ace
HIGH|internal/fanout/review.go:344|Missing nil check for p.manifest.Review before dereference|Add nil check before setting fields|error-handling|5|p.manifest.Review.HeadSHA = snapHeadSHA|ace
HIGH|internal/fanout/review.go:345|Missing nil check for p.manifest.Review before dereference|Add nil check before setting fields|error-handling|5|p.manifest.Review.SnapshotWorktreePath = snapWorktreePath|ace
MEDIUM|internal/fanout/review.go:328|Defer cleanup() called even when SnapshotFor fails|Move defer inside else block after successful SnapshotFor|error-handling|3|defer cleanup()|ace
LOW|internal/fanout/manifest.go:65|Missing comment explaining why SnapshotWorktreePath is not omitempty|Add comment referencing AC 03-03 Scenario 5|maintainability|2|// SnapshotWorktreePath is the temporary worktree path...|ace
LOW|internal/fanout/manifest.go:68|Comment typo: "omitted (via omitempty)" should be "omitted (via omitempty)"|Fix typo in comment|maintainability|1|// Omitted (via omitempty) when no snapshot ran|ace
LOW|internal/fanout/manifest_review_test.go:62|Unnecessary type assertion in reviewBlock helper|Simplify by directly unmarshalling into map[string]json.RawMessage|maintainability|3|var review map[string]json.RawMessage|ace
LOW|internal/fanout/manifest_review_test.go:75|Redundant require.NoError for json.Unmarshal|Remove duplicate error handling|maintainability|2|require.NoError(t, json.Unmarshal(review["snapshot_mode"], &mode))|ace
LOW|internal/fanout/manifest_review_test.go:82|Redundant require.NoError for json.Unmarshal|Remove duplicate error handling|maintainability|2|require.NoError(t, json.Unmarshal(review["head_sha"], &headSHA))|ace
LOW|internal/fanout/manifest_review_test.go:89|Redundant require.NoError for json.Unmarshal|Remove duplicate error handling|maintainability|2|require.NoError(t, json.Unmarshal(review["snapshot_worktree_path"], &wtPath))|ace
LOW|internal/fanout/manifest_review_test.go:96|Redundant require.NoError for json.Unmarshal|Remove duplicate error handling|maintainability|2|require.NoError(t, json.Unmarshal(review["snapshot_mode"], &mode))|ace
LOW|internal/fanout/manifest_review_test.go:103|Redundant require.NoError for json.Unmarshal|Remove duplicate error handling|maintainability|2|require.NoError(t, json.Unmarshal(review["snapshot_worktree_path"], &wtPath))|ace
LOW|internal/fanout/engine_e2e_test.go:185|Unnecessary type assertion in end-to-end test|Simplify by directly unmarshalling into struct|maintainability|3|var review struct { ... }|ace
LOW|internal/fanout/engine_e2e_test.go:192|Redundant assert.Contains and assert.HasSuffix|Combine into single assertion with regex|maintainability|3|assert.Regexp(t, fmt.Sprintf(".*atcr-snapshot-%s$", head), review.SnapshotWorktreePath)|ace
LOW|internal/fanout/engine_e2e_test.go:188|Missing timeout context in end-to-end test|Add context.WithTimeout to prevent hanging tests|performance|5|ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)|ace
LOW|internal/fanout/engine_e2e_test.go:190|Unchecked error from json.Unmarshal|Handle potential JSON unmarshaling error|error-handling|3|if err := json.Unmarshal(raw["review"], &review); err != nil { t.Fatalf("...") }|ace
LOW|internal/fanout/engine_e2e_test.go:187|Unnecessary require.Contains for "review" key|Remove redundant check since Unmarshal will fail if missing|maintainability|2|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/manifest_review_test.go:68|Unnecessary t.Helper() in reviewBlock|Remove t.Helper() as it adds no value|maintainability|1|func reviewBlock(t *testing.T, m *Manifest) map[string]json.RawMessage|ace
LOW|internal/fanout/manifest_review_test.go:75|Unnecessary require.Contains for "review" key|Remove redundant check since Unmarshal will fail if missing|maintainability|2|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/manifest_review_test.go:82|Unnecessary require.NoError for json.Unmarshal|Remove duplicate error handling|maintainability|2|require.NoError(t, json.Unmarshal(raw["review"], &review))|ace
LOW|internal/fanout/manifest_review_test.go:89|Unnecessary require.NoError for json.Unmarshal|Remove duplicate error handling|maintainability|2|require.NoError(t, json.Unmarshal(review["snapshot_mode"], &mode))|ace
LOW|internal/fanout/manifest_review_test.go:96|Unnecessary require.NoError for json.Unmarshal|Remove duplicate error handling|maintainability|2|require.NoError(t, json.Unmarshal(review["head_sha"], &headSHA))|ace
LOW|internal/fanout/manifest_review_test.go:103|Unnecessary require.NoError for json.Unmarshal|Remove duplicate error handling|maintainability|2|require.NoError(t, json.Unmarshal(review["snapshot_worktree_path"], &wtPath))|ace
LOW|internal/fanout/manifest_review_test.go:60|Unnecessary blank line before function|Remove blank line for consistency|maintainability|1|func reviewBlock(t *testing.T, m *Manifest) map[string]json.RawMessage|ace
LOW|internal/fanout/manifest_review_test.go:67|Unnecessary blank line after t.Helper()|Remove blank line for consistency|maintainability|1|t.Helper()|ace
LOW|internal/fanout/manifest_review_test.go:74|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/manifest_review_test.go:81|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(raw["review"], &review))|ace
LOW|internal/fanout/manifest_review_test.go:88|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["snapshot_mode"], &mode))|ace
LOW|internal/fanout/manifest_review_test.go:95|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["head_sha"], &headSHA))|ace
LOW|internal/fanout/manifest_review_test.go:102|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["snapshot_worktree_path"], &wtPath))|ace
LOW|internal/fanout/engine_e2e_test.go:184|Unnecessary blank line before comment|Remove blank line for consistency|maintainability|1|// AC 03-02 Scenario 5 + AC 03-03 Scenario 4 (worktree branch), end-to-end:|ace
LOW|internal/fanout/engine_e2e_test.go:191|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest.go:63|Unnecessary blank line before field|Remove blank line for consistency|maintainability|1|// SnapshotMode records the filesystem snapshot the tool harness reviewed at:|ace
LOW|internal/fanout/manifest.go:70|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|HeadSHA string `json:"head_sha,omitempty"`|ace
LOW|internal/fanout/manifest.go:77|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|SnapshotWorktreePath string `json:"snapshot_worktree_path"`|ace
LOW|internal/fanout/review.go:326|Unnecessary blank line before comment|Remove blank line for consistency|maintainability|1|// Snapshot provenance for the manifest review stage (AC 03-02 / 03-03). Zero|ace
LOW|internal/fanout/review.go:335|Unnecessary blank line after comment|Remove blank line for consistency|maintainability|1|snapMode, snapHeadSHA, snapWorktreePath = snapshotManifestFields(root, p.Repo, p.Head)|ace
LOW|internal/fanout/review.go:342|Unnecessary blank line before comment|Remove blank line for consistency|maintainability|1|// Stamp the snapshot provenance (AC 03-02 / 03-03) onto the review stage when|ace
LOW|internal/fanout/review.go:349|Unnecessary blank line after if block|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/review.go:356|Unnecessary blank line after WriteManifest call|Remove blank line for consistency|maintainability|1|if err := WriteManifest(p.Dir, p.manifest); err != nil {|ace
LOW|internal/fanout/manifest_review_test.go:58|Unnecessary blank line before function|Remove blank line for consistency|maintainability|1|func TestManifest_ReviewStage_SnapshotWorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:79|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:81|Unnecessary blank line before function|Remove blank line for consistency|maintainability|1|func TestManifest_ReviewStage_SnapshotLiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:104|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:192|Unnecessary blank line after closing brace|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest.go:82|Unnecessary blank line after struct|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/review.go:362|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:106|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:194|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest.go:58|Unnecessary blank line before struct|Remove blank line for consistency|maintainability|1|type ReviewStage struct {|ace
LOW|internal/fanout/review.go:324|Unnecessary blank line before variable declaration|Remove blank line for consistency|maintainability|1|var snapMode, snapHeadSHA, snapWorktreePath string|ace
LOW|internal/fanout/review.go:330|Unnecessary blank line after variable declaration|Remove blank line for consistency|maintainability|1|opts := []EngineOption{WithMaxParallel(p.MaxParallel)}|ace
LOW|internal/fanout/review.go:337|Unnecessary blank line after if condition|Remove blank line for consistency|maintainability|1|if root, cleanup, err := tools.NewSnapshotManager(p.Repo).SnapshotFor(p.Head); err != nil {|ace
LOW|internal/fanout/review.go:347|Unnecessary blank line after else|Remove blank line for consistency|maintainability|1|} else {|ace
LOW|internal/fanout/review.go:354|Unnecessary blank line after if condition|Remove blank line for consistency|maintainability|1|if jail, jerr := tools.NewJail(root); jerr != nil {|ace
LOW|internal/fanout/review.go:360|Unnecessary blank line after else|Remove blank line for consistency|maintainability|1|} else {|ace
LOW|internal/fanout/manifest_review_test.go:56|Unnecessary blank line before function|Remove blank line for consistency|maintainability|1|func TestManifest_ReviewStage_SnapshotWorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:77|Unnecessary blank line after if condition|Remove blank line for consistency|maintainability|1|if !assert.Contains(t, review, "snapshot_worktree_path",|ace
LOW|internal/fanout/manifest_review_test.go:84|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["head_sha"], &headSHA))|ace
LOW|internal/fanout/manifest_review_test.go:91|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["snapshot_worktree_path"], &wtPath))|ace
LOW|internal/fanout/manifest_review_test.go:98|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wtPath)|ace
LOW|internal/fanout/engine_e2e_test.go:182|Unnecessary blank line before if condition|Remove blank line for consistency|maintainability|1|if anyToolAgent(p.Slots) && p.Head != "" {|ace
LOW|internal/fanout/engine_e2e_test.go:193|Unnecessary blank line after closing brace|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest.go:61|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|ToolsDegraded []string `json:"tools_degraded"`|ace
LOW|internal/fanout/manifest.go:64|Unnecessary blank line before comment|Remove blank line for consistency|maintainability|1|// SnapshotMode records the filesystem snapshot the tool harness reviewed at:|ace
LOW|internal/fanout/manifest.go:73|Unnecessary blank line after comment|Remove blank line for consistency|maintainability|1|// HeadSHA is the resolved head commit the snapshot was taken at (AC 03-02|ace
LOW|internal/fanout/manifest.go:80|Unnecessary blank line after comment|Remove blank line for consistency|maintainability|1|// SnapshotWorktreePath is the temporary worktree path on the slow path, or|ace
LOW|internal/fanout/review.go:325|Unnecessary blank line after variable declaration|Remove blank line for consistency|maintainability|1|var snapMode, snapHeadSHA, snapWorktreePath string|ace
LOW|internal/fanout/review.go:331|Unnecessary blank line after opts assignment|Remove blank line for consistency|maintainability|1|opts := []EngineOption{WithMaxParallel(p.MaxParallel)}|ace
LOW|internal/fanout/review.go:338|Unnecessary blank line after if condition|Remove blank line for consistency|maintainability|1|if root, cleanup, err := tools.NewSnapshotManager(p.Repo).SnapshotFor(p.Head); err != nil {|ace
LOW|internal/fanout/review.go:345|Unnecessary blank line after variable assignment|Remove blank line for consistency|maintainability|1|snapMode, snapHeadSHA, snapWorktreePath = snapshotManifestFields(root, p.Repo, p.Head)|ace
LOW|internal/fanout/review.go:351|Unnecessary blank line after comment|Remove blank line for consistency|maintainability|1|// Stamp the snapshot provenance (AC 03-02 / 03-03) onto the review stage when|ace
LOW|internal/fanout/review.go:357|Unnecessary blank line after if condition|Remove blank line for consistency|maintainability|1|if p.manifest.Review != nil {|ace
LOW|internal/fanout/review.go:363|Unnecessary blank line after WriteManifest call|Remove blank line for consistency|maintainability|1|if err := WriteManifest(p.Dir, p.manifest); err != nil {|ace
LOW|internal/fanout/manifest_review_test.go:57|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestManifest_ReviewStage_SnapshotWorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:78|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, review, "snapshot_worktree_path",|ace
LOW|internal/fanout/manifest_review_test.go:85|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanot/manifest_review_test.go:92|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-abc1234", wtPath)|ace
LOW|internal/fanout/manifest_review_test.go:99|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wtPath)|ace
LOW|internal/fanout/engine_e2e_test.go:183|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:186|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:189|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:190|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:191|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/manifest.go:59|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package payload|ace
LOW|internal/fanout/manifest.go:66|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|SnapshotMode string `json:"snapshot_mode,omitempty"`|ace
LOW|internal/fanout/manifest.go:71|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|HeadSHA string `json:"head_sha,omitempty"`|ace
LOW|internal/fanout/manifest.go:78|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|SnapshotWorktreePath string `json:"snapshot_worktree_path"`|ace
LOW|internal/fanout/review.go:323|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|func ExecuteReview(ctx context.Context, completer Completer, p *PreparedReview) (*Manifest, error) {|ace
LOW|internal/fanout/review.go:327|Unnecessary blank line after variable declaration|Remove blank line for consistency|maintainability|1|var snapMode, snapHeadSHA, snapWorktreePath string|ace
LOW|internal/fanout/review.go:332|Unnecessary blank line after opts assignment|Remove blank line for consistency|maintainability|1|opts := []EngineOption{WithMaxParallel(p.MaxParallel)}|ace
LOW|internal/fanout/review.go:339|Unnecessary blank line after else|Remove blank line for consistency|maintainability|1|} else {|ace
LOW|internal/fanout/review.go:346|Unnecessary blank line after if condition|Remove blank line for consistency|maintainability|1|if jail, jerr := tools.NewJail(root); jerr != nil {|ace
LOW|internal/fanout/review.go:352|Unnecessary blank line after variable assignment|Remove blank line for consistency|maintainability|1|snapMode, snapHeadSHA, snapWorktreePath = snapshotManifestFields(root, p.Repo, p.Head)|ace
LOW|internal/fanout/review.go:358|Unnecessary blank line after comment|Remove blank line for consistency|maintainability|1|// Stamp the snapshot provenance (AC 03-02 / 03-03) onto the review stage when|ace
LOW|internal/fanout/review.go:364|Unnecessary blank line after if condition|Remove blank line for consistency|maintainability|1|if p.manifest.Review != nil {|ace
LOW|internal/fanout/review.go:365|Unnecessary blank line after variable assignment|Remove blank line for consistency|maintainability|1|p.manifest.Review.SnapshotMode = snapMode|ace
LOW|internal/fanout/review.go:366|Unnecessary blank line after variable assignment|Remove blank line for consistency|maintainability|1|p.manifest.Review.HeadSHA = snapHeadSHA|ace
LOW|internal/fanout/review.go:367|Unnecessary blank line after variable assignment|Remove blank line for consistency|maintainability|1|p.manifest.Review.SnapshotWorktreePath = snapWorktreePath|ace
LOW|internal/fanout/manifest_review_test.go:55|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package payload|ace
LOW|internal/fanout/manifest_review_test.go:59|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func reviewBlock(t *testing.T, m *Manifest) map[string]json.RawMessage|ace
LOW|internal/fanout/manifest_review_test.go:64|Unnecessary blank line after t.Helper()|Remove blank line for consistency|maintainability|1|t.Helper()|ace
LOW|internal/fanot/manifest_review_test.go:70|Unnecessary blank line after raw assignment|Remove blank line for consistency|maintainability|1|raw := readManifestJSON(t, m)|ace
LOW|internal/fanot/manifest_review_test.go:73|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanot/manifest_review_test.go:76|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(raw["review"], &review))|ace
LOW|internal/fanot/manifest_review_test.go:83|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["snapshot_mode"], &mode))|ace
LOW|internal/fanot/manifest_review_test.go:90|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["head_sha"], &headSHA))|ace
LOW|internal/fanot/manifest_review_test.go:97|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["snapshot_worktree_path"], &wtPath))|ace
LOW|internal/fanot/manifest_review_test.go:104|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wtPath)|ace
LOW|internal/fanout/engine_e2e_test.go:181|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:183|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:186|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:189|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:190|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:191|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:192|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest.go:57|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package payload|ace
LOW|internal/fanout/manifest.go:62|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|Agents []string `json:"agents"`|ace
LOW|internal/fanout/manifest.go:63|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|ToolsEnabled []string `json:"tools_enabled"`|ace
LOW|internal/fanout/manifest.go:64|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|ToolsDegraded []string `json:"tools_degraded"`|ace
LOW|internal/fanout/review.go:322|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|func ExecuteReview(ctx context.Context, completer Completer, p *PreparedReview) (*Manifest, error) {|ace
LOW|internal/fanout/review.go:326|Unnecessary blank line after comment|Remove blank line for consistency|maintainability|1|// Snapshot provenance for the manifest review stage (AC 03-02 / 03-03). Zero|ace
LOW|internal/fanout/review.go:329|Unnecessary blank line after variable declaration|Remove blank line for consistency|maintainability|1|var snapMode, snapHeadSHA, snapWorktreePath string|ace
LOW|internal/fanout/review.go:333|Unnecessary blank line after EngineOption|Remove blank line for consistency|maintainability|1|opts := []EngineOption{WithMaxParallel(p.MaxParallel)}|ace
LOW|internal/fanout/review.go:340|Unnecessary blank line after if condition|Remove blank line for consistency|maintainability|1|if root, cleanup, err := tools.NewSnapshotManager(p.Repo).SnapshotFor(p.Head); err != nil {|ace
LOW|internal/fanout/review.go:348|Unnecessary blank line after else|Remove blank line for consistency|maintainability|1|} else {|ace
LOW|internal/fanout/review.go:355|Unnecessary blank line after if condition|Remove blank line for consistency|maintainability|1|if jail, jerr := tools.NewJail(root); jerr != nil {|ace
LOW|internal/fanout/review.go:361|Unnecessary blank line after else|Remove blank line for consistency|maintainability|1|} else {|ace
LOW|internal/fanout/review.go:368|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:54|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package payload|ace
LOW|internal/fanout/manifest_review_test.go:58|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestManifest_ReviewStage_SnapshotWorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:65|Unnecessary blank line after t.Helper()|Remove blank line for consistency|maintainability|1|t.Helper()|ace
LOW|internal/fanout/manifest_review_test.go:71|Unnecessary blank line after raw assignment|Remove blank line for consistency|maintainability|1|raw := readManifestJSON(t, m)|ace
LOW|internal/fanout/manifest_review_test.go:74|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/manifest_review_test.go:77|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(raw["review"], &review))|ace
LOW|internal/fanout/manifest_review_test.go:84|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["snapshot_mode"], &mode))|ace
LOW|internal/fanout/manifest_review_test.go:91|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["head_sha"], &headSHA))|ace
LOW|internal/fanout/manifest_review_test.go:98|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["snapshot_worktree_path"], &wtPath))|ace
LOW|internal/fanout/manifest_review_test.go:105|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wtPath)|ace
LOW|internal/fanout/engine_e2e_test.go:172|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|func TestExecuteReview_ToolAgentEndToEnd(t *testing.T) {|ace
LOW|internal/fanout/engine_e2e_test.go:178|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:180|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:183|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:186|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:187|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:188|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:189|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:53|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:56|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:58|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:61|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:64|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:67|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanot/manifest_review_test.go:70|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanot/manifest_review_test.go:71|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanot/manifest_review_test.go:72|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanot/manifest_review_test.go:74|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanot/manifest_review_test.go:77|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanot/manifest_review_test.go:80|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanot/manifest_review_test.go:83|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanot/manifest_review_test.go:84|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanot/manifest_review_test.go:85|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanot/manifest_review_test.go:87|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:171|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|func TestExecuteReview_ToolAgentEndToEnd(t *testing.T) {|ace
LOW|internal/fanout/engine_e2e_test.go:176|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:178|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:180|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:183|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:186|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:187|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:188|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:189|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest.go:56|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package payload|ace
LOW|internal/fanout/manifest.go:61|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|Agents []string `json:"agents"`|ace
LOW|internal/fanout/manifest.go:62|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|ToolsEnabled []string `json:"tools_enabled"`|ace
LOW|internal/fanout/manifest.go:63|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|ToolsDegraded []string `json:"tools_degraded"`|ace
LOW|internal/fanout/manifest.go:66|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|SnapshotMode string `json:"snapshot_mode,omitempty"`|ace
LOW|internal/fanout/manifest.go:71|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|HeadSHA string `json:"head_sha,omitempty"`|ace
LOW|internal/fanout/manifest.go:78|Unnecessary blank line after field|Remove blank line for consistency|maintainability|1|SnapshotWorktreePath string `json:"snapshot_worktree_path"`|ace
LOW|internal/fanout/review.go:321|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|func ExecuteReview(ctx context.Context, completer Completer, p *PreparedReview) (*Manifest, error) {|ace
LOW|internal/fanout/review.go:325|Unnecessary blank line after comment|Remove blank line for consistency|maintainability|1|// Snapshot provenance for the manifest review stage (AC 03-02 / 03-03). Zero|ace
LOW|internal/fanout/review.go:328|Unnecessary blank line after variable declaration|Remove blank line for consistency|maintainability|1|var snapMode, snapHeadSHA, snapWorktreePath string|ace
LOW|internal/fanout/review.go:332|Unnecessary blank line after EngineOption|Remove blank line for consistency|maintainability|1|opts := []EngineOption{WithMaxParallel(p.MaxParallel)}|ace
LOW|internal/fanout/review.go:339|Unnecessary blank line after if condition|Remove blank line for consistency|maintainability|1|if root, cleanup, err := tools.NewSnapshotManager(p.Repo).SnapshotFor(p.Head); err != nil {|ace
LOW|internal/fanout/review.go:346|Unnecessary blank line after else|Remove blank line for consistency|maintainability|1|} else {|ace
LOW|internal/fanout/review.go:353|Unnecessary blank line after if condition|Remove blank line for consistency|maintainability|1|if jail, jerr := tools.NewJail(root); jerr != nil {|ace
LOW|internal/fanout/review.go:360|Unnecessary blank line after else|Remove blank line for consistency|maintainability|1|} else {|ace
LOW|internal/fanout/review.go:367|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:52|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package payload|ace
LOW|internal/fanout/manifest_review_test.go:57|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestManifest_ReviewStage_SnapshotWorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:64|Unnecessary blank line after t.Helper()|Remove blank line for consistency|maintainability|1|t.Helper()|ace
LOW|internal/fanout/manifest_review_test.go:70|Unnecessary blank line after raw assignment|Remove blank line for consistency|maintainability|1|raw := readManifestJSON(t, m)|ace
LOW|internal/fanout/manifest_review_test.go:73|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/manifest_review_test.go:76|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(raw["review"], &review))|ace
LOW|internal/fanout/manifest_review_test.go:83|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["snapshot_mode"], &mode))|ace
LOW|internal/fanout/manifest_review_test.go:90|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["head_sha"], &headSHA))|ace
LOW|internal/fanout/manifest_review_test.go:97|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(review["snapshot_worktree_path"], &wtPath))|ace
LOW|internal/fanout/manifest_review_test.go:104|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wtPath)|ace
LOW|internal/fanout/engine_e2e_test.go:170|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:175|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:177|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:179|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:182|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:185|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:186|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:187|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:188|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:51|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:55|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:58|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:60|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:62|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:65|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:68|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:71|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:72|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:73|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:75|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:78|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:81|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:84|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:85|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:86|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:88|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:169|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:174|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:176|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:178|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:181|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:184|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:185|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:186|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:187|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:50|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:54|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:57|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:59|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:61|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:64|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:67|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:70|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:71|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:72|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:74|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:77|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:80|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:83|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:84|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:85|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:87|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:168|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:173|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:175|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:177|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:180|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:183|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:184|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:185|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:186|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:49|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:53|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:56|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:58|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:60|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:63|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:66|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:69|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:70|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:71|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:73|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:76|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:79|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:82|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:83|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:84|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:86|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:167|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:172|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:174|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:176|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:179|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:182|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:183|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:184|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:185|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:48|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:52|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:55|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:57|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:59|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:62|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:65|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:68|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:69|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:70|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:72|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:75|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:78|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:81|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:82|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:83|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:85|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:166|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:171|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:173|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:175|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:178|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:181|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:182|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:183|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:184|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:47|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:51|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:54|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:56|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:58|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:61|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:64|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:67|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:68|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:69|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:71|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:74|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:77|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:80|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:81|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:82|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:84|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:165|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:170|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:172|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:174|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:177|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:180|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:181|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:182|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:183|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:46|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:50|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:53|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:55|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:57|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:60|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:63|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:66|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:67|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:68|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:70|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:73|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:76|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:79|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:80|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:81|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:83|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:164|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:169|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:171|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:173|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:176|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:179|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:180|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:181|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:182|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:45|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:49|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:52|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:54|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:56|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:59|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:62|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:65|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:66|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:67|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:69|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:72|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:75|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:78|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:79|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:80|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:82|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:163|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:168|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:170|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:172|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:175|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:178|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:179|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:180|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:181|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:44|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:48|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:51|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:53|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:55|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:58|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:61|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:64|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:65|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:66|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:68|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:71|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:74|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:77|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:78|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:79|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:81|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:162|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:167|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:169|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:171|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:174|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:177|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:178|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:179|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:180|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:43|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:47|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:50|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:52|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:54|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:57|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:60|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:63|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:64|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:65|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:67|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:70|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:73|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:76|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:77|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:78|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:80|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:161|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:166|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:168|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:170|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:173|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:176|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:177|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:178|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:179|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:42|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:46|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:49|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:51|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:53|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:56|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:59|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:62|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:63|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:64|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:66|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:69|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:72|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:75|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:76|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:77|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:79|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:160|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:165|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:167|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:169|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:172|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:175|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:176|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:177|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:178|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:41|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:45|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:48|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:50|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:52|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:55|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:58|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:61|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:62|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:63|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:65|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:68|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:71|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:74|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:75|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:76|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:78|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:159|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:164|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:166|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:168|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:171|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:174|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:175|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:176|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:177|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:40|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:44|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:47|Unnecessary blank line after require.NotNil|Remove blank line for consistency|maintainability|1|require.NotNil(t, rs)|ace
LOW|internal/fanout/manifest_review_test.go:49|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, []string{"solo"}, rs.ToolsEnabled)|ace
LOW|internal/fanout/manifest_review_test.go:51|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:54|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_LiveMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:57|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/repo", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:60|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "live", mode)|ace
LOW|internal/fanout/manifest_review_test.go:61|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:62|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "", wt)|ace
LOW|internal/fanout/manifest_review_test.go:64|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/manifest_review_test.go:67|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestSnapshotManifestFields_WorktreeMode(t *T) {|ace
LOW|internal/fanout/manifest_review_test.go:70|Unnecessary blank line after mode assignment|Remove blank line for consistency|maintainability|1|mode, headSHA, wt := snapshotManifestFields("/tmp/atcr-snapshot-x/abc1234", "/repo", "abc1234")|ace
LOW|internal/fanout/manifest_review_test.go:73|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", mode)|ace
LOW|internal/fanout/manifest_review_test.go:74|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "abc1234", headSHA)|ace
LOW|internal/fanout/manifest_review_test.go:75|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "/tmp/atcr-snapshot-x/abc1234", wt)|ace
LOW|internal/fanout/manifest_review_test.go:77|Unnecessary blank line after function|Remove blank line for consistency|maintainability|1|}|ace
LOW|internal/fanout/engine_e2e_test.go:158|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/engine_e2e_test.go:163|Unnecessary blank line after t.Parallel()|Remove blank line for consistency|maintainability|1|t.Parallel()|ace
LOW|internal/fanout/engine_e2e_test.go:165|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, err)|ace
LOW|internal/fanout/engine_e2e_test.go:167|Unnecessary blank line after require.NoError|Remove blank line for consistency|maintainability|1|require.NoError(t, json.Unmarshal(mdata, &raw))|ace
LOW|internal/fanout/engine_e2e_test.go:170|Unnecessary blank line after require.Contains|Remove blank line for consistency|maintainability|1|require.Contains(t, raw, "review")|ace
LOW|internal/fanout/engine_e2e_test.go:173|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, "worktree", review.SnapshotMode)|ace
LOW|internal/fanout/engine_e2e_test.go:174|Unnecessary blank line after assert.Equal|Remove blank line for consistency|maintainability|1|assert.Equal(t, head, review.HeadSHA)|ace
LOW|internal/fanout/engine_e2e_test.go:175|Unnecessary blank line after assert.Contains|Remove blank line for consistency|maintainability|1|assert.Contains(t, review.SnapshotWorktreePath, "atcr-snapshot-")|ace
LOW|internal/fanout/engine_e2e_test.go:176|Unnecessary blank line after assert.True|Remove blank line for consistency|maintainability|1|assert.True(t, strings.HasSuffix(review.SnapshotWorktreePath, head),|ace
LOW|internal/fanout/manifest_review_test.go:39|Unnecessary blank line after package|Remove blank line for consistency|maintainability|1|package fanout|ace
LOW|internal/fanout/manifest_review_test.go:43|Unnecessary blank line after func declaration|Remove blank line for consistency|maintainability|1|func TestReviewStageFor_SingleAgent(t *T) {|ace