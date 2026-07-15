Here's a thinking process:

1.  **Analyze User Input:**
   - **Role:** greta, algorithmic correctness reviewer
   - **Focus:** Boundary conditions, loop correctness, numeric issues, data-structure misuse, complexity traps
   - **Scope:** Review only changed regions in the diff. FILE:LINE must fall within changed lines. If flagging pre-existing issues, use CATEGORY `out-of-scope`. Constrain to sprint plan scope (SARIF Output Integration), but still report critical issues.
   - **Severity Rubric:** CRITICAL, HIGH, MEDIUM, LOW
   - **Output Format:** `SEVERITY|FILE:LINE|PROBLEM|FIX|CATEGORY|EST_MINUTES|EVIDENCE` (pipe-delimited, exactly 7 columns, replace `|` with `/`, no prose, one per line)
   - **Payload:** A diff showing 15 new/modified files, all related to planning/documentation for Sprint 25.0: SARIF Output Integration. The files are markdown, yaml, and text files in `.planning/sprints/active/25.0_sarif_output_integration/`.
   - **Key Observation:** The diff contains *only* planning/documentation files (markdown, yaml). There is *no actual code* in the diff. The diff adds `README.md`, `metadata.md`, `plan/README.md`, `plan/acceptance-criteria/*.md`, `plan/documentation/*.md`, `plan/metadata.md`, `plan/package-recommendations.md`, `plan/test-planning-matrix.md`, `sprint-knowledge.yaml`.

2.  **Evaluate Against Constraints:**
   - The prompt says: "Review only the changed regions... If nothing is wrong, emit nothing."
   - The diff contains only planning documents, markdown, and YAML configuration. There are no algorithms, loops, math, data structures, or boundary conditions in the code sense to review for algorithmic correctness.
   - The scope constraint says: "Suppress findings for unrelated changes that merely happen to appear in the diff — for example dependency bumps, formatter or whitespace-only reformatting, and mechanical refactors not described in the plan." These files *are* the plan files, so they are in scope, but they are documentation/planning artifacts, not executable code.
   - As an "algorithmic correctness reviewer", my job is to verify loops, math, data structures, boundary conditions in *code*. Since there is no code in this diff, there are no algorithmic correctness findings to report.
   - I should verify if any YAML or markdown contains algorithmic/logical flaws that fit the rubric. The YAML is just metadata/sprint tracking. The markdown describes plans, acceptance criteria, and documentation references. No executable logic.
   - Therefore, the correct output is to emit nothing, as per the rule: "If nothing is wrong, emit nothing."

3.  **Double-Check:**
   - Could there be a hidden trap? The prompt says "Reviewing 15 changed file(s)... payload mode: blocks." The diff is entirely new files (all `+++` with `index 00000000..`).
   - The prompt explicitly says: "If nothing is wrong, emit nothing."
   - I will strictly follow this. No code = no algorithmic issues to flag.
   - I will output an empty response.

   Wait, let's carefully read the prompt to ensure I'm not missing a subtle instruction. "Emit ONLY findings, one per line... If nothing is wrong, emit nothing."
   I will output nothing.

   However, sometimes these prompts expect a specific acknowledgment if empty, but the instruction is strict: "If nothing is wrong, emit nothing." I will just output an empty string.

   Let's verify if there's any logical flaw in the planning documents that could be construed as an algorithmic issue under the rubric. For example, the test matrix says "Total ACs: 9" but lists ACs 01-01, 01-02, 01-03, 01-04, 02-01, 03-01, 03-02, 04-01, 04-02. That's 9. Matches.
   The timeline says 1.5 + 1.5 + 1 + 1 = 5 days. Matches.
   Everything is consistent documentation.

   Conclusion: No findings. Output is empty.
   I will produce exactly an empty response.
   Wait, the prompt says "Emit ONLY findings... If nothing is wrong, emit nothing."
   I'll just output nothing.
   To be safe, I'll make sure there's absolutely no extra text.
   Proceed. 
   Self-Correction/Refinement during thought: Sometimes the system expects a specific format even if empty, but the instruction is explicit. I will output an empty string. If the platform requires *something*, I might just leave it blank. I'll stick to the instruction.
   Actually, I'll double check the diff for any code snippets that might contain bugs. There are code snippets in the markdown (e.g., `renderJSON` function, `jsonschema` usage). But these are quoted examples/documentation, not changed code in the diff. The diff is purely new files. So no algorithmic review needed.
   Output: (empty)
   I will generate an empty response.✅
   Wait, let's consider if I should output a single newline or literally nothing. I'll output nothing.
   All good. 
   Proceeds. 
   [Output Generation] -> (empty)
   Actually, to be strictly compliant with "If nothing is wrong, emit nothing.", I will just output an empty string.
   Final check of the prompt: "Emit ONLY findings, one per line, exactly 7 pipe-delimited columns... If nothing is wrong, emit nothing."
   I will output nothing. 
   Done. 
   *Self-Correction/Verification during generation prep*: 
   I should ensure I don't accidentally output markdown or conversational text. I will just output an empty response. If the system requires a non-empty response, it's a platform constraint, but I'll follow the prompt exactly.
   I will produce exactly an empty string.
   Wait, is there any chance the prompt implies I should review the *planning logic*? "You are greta, the panel's algorithm specialist. You verify that loops, math, data structures, and boundary conditions are actually correct..." This is strictly about code. Planning docs don't have loops/math.
   So, empty output is correct.
   I will output nothing.✅
   [Done]