We have a large diff adding many new files. We need to find API contract changes. The diff is only adding new files; no modifications to existing files. Thus no API changes. But we must consider that adding new files could introduce new exported symbols that may break compatibility? No, new functions are new, not breaking existing API. No changes to existing signatures.

But we need to check if any changes to existing files are present. The diff shows only new files; no modifications. So no findings. However, we must consider that the new files might introduce exported constants or functions that clash with existing ones? But since it's new files, not changes, no breaking change.

Thus output nothing.

Check if any new files add to internal/report/render.go? Wait, the plan refers to modifications to internal/report/render.go. But the diff didn't show modifications to that file. It's only adding new files. So the diff doesn't include the actual changes to internal/report/render.go. So we can't detect breaking changes. The instruction: "Review only the changed regions. The payload shows you the change in context, but a finding whose FILE:LINE falls outside the changed lines will be discarded". Since no changed files, no findings.

Thus output nothing.