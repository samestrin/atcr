# changelog.d

One file per change not yet released. The rules are at the top of
[`CHANGELOG.md`](../CHANGELOG.md); in short:

- Name the file `<category>-<short-slug>.md`, where `<category>` is `added`,
  `changed`, `deprecated`, `removed`, `fixed` or `security`, and the slug is
  unique, for example `added-preview-url.md`.
- Put one or more `- ` bullet lines in it, written for a user of `atcr` and
  ending with the source in brackets, for example `(epic 85.0)`.

When a release is cut, `go run ./cmd/changelog assemble --version X.Y.Z --date
YYYY-MM-DD` moves every fragment into `CHANGELOG.md` and deletes it (see
[docs/release-process.md](../docs/release-process.md#cutting-a-release)). This
README is never assembled.
