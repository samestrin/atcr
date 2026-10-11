# JSON repair fixture corpus

Damaged model replies used to test the last-try JSON repair (epic 35.16.11.2.2.10). The `jsonrepair` tests, the findings parser tests (`internal/stream`) and the skeptic verdict parser tests (`internal/verify`) all read this one corpus.

Every fixture is a synthetic reconstruction written by hand on made-up content: a shop cart, a billing script, a web page, a readme. None is a verbatim reply from any run, so the corpus publishes no one's code. `corpus_test.go` fails if a fixture mentions atcr.

## Layout

- `corpus.json` lists every fixture: its `name`, `lane`, defect `class`, a `note` on what is damaged, and for the findings lane where the damage sits (`position`). `cut_off: true` marks a reply that stops before its end.
- `findings/<name>.txt` is the text inside a ```` ```json ```` fence of a reviewer reply, which is what `decodeJSONValue` reads. Wrap it in a fence to build the reply. Leave the closing fence off a `cut_off` fixture, or keep it: the JSON ends early either way.
- `findings/<name>.want.json` is the expected decode: a JSON array of the finding objects (the 7 model keys) the parser should return, in order.
- `verdict/<name>.txt` is a whole skeptic reply, prose and fence included, which is what `parseVerdictCore` reads.
- `verdict/<name>.want.json` is the expected verdict object (`verdict` and `reasoning`), or `null` when no verdict should be taken.

Compare decoded values, not bytes: whitespace and key order in the input are not part of the expected value.

## Defect classes

| Class | Findings lane | Verdict lane |
|---|---|---|
| `valid` (controls; repair must leave them unchanged) | yes | yes |
| `trailing_comma` | yes | yes |
| `single_quotes` | yes | yes |
| `line_comment` | yes | yes |
| `block_comment` | yes | yes |
| `unquoted_keys` | yes | yes |
| `mismatched_closer` | yes | no |
| `extra_closer` | yes | no |
| `missing_closer` | yes | no |
| `mixed` | yes | no |
| `cut_off` (must stay dropped) | yes | yes |

The verdict lane has no closer classes: it repairs only the balanced objects `extractJSONObject` already finds, so a missing or mismatched brace never reaches repair there.

On the findings lane, each element-level class has a fixture where the damaged finding sits between two intact ones (`position: between_intact`): the expected decode keeps all three, not only the one before the damage. The `cut_off` fixtures keep every complete finding and drop the final one, because repair never closes an open string or an element cut off by end of input.

## Adding a fixture

Write the `.txt` and `.want.json` pair by hand on neutral content, add an entry to `corpus.json`, and run `go test ./internal/jsonrepair/`. Never paste a reply from a real run.
