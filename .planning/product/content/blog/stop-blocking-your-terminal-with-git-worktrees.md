# Stop Blocking Your Terminal: The Ultimate Local AI Code Review Workflow

**Target Audience:** Open-source developers, local-CLI users, and engineers using ATCR with BYO-Keys.
**Objective:** Teach users how to use `git worktree` so that ATCR's `--auto-fix` execution doesn't block their primary development environment.

---

## 1. Introduction: The Price of Thoroughness
- Introduce ATCR's core differentiator: we don't just use a single LLM prompt. We use a multi-agent consensus panel (Reviewers + Skeptics), route to a Fixer, and execute patches inside a Docker validation sandbox.
- **The Catch:** This takes several minutes. 
- **The Problem:** If you run `atcr review --auto-fix` in your primary working directory, you are effectively blocked. If you switch branches or edit tracked files while ATCR is grinding in the background, you risk causing merge conflicts or breaking the `atomicfs` rollback mechanism when ATCR tries to apply the LLM's patch.

## 2. The Solution: Enter `git worktree`
- Briefly explain what `git worktree` is: a built-in Git feature that allows you to check out multiple branches at once in different directories, all sharing the same local `.git` database.
- Explain why it is the perfect pairing for a local AI agent: it gives the agent its own isolated filesystem and Git index to mutate, without touching your active workspace.

## 3. The Step-by-Step Workflow (Pro-Tip)
Walk the user through the exact terminal commands for this workflow:

1. **Create the Worktree:**
   When you are ready to review your feature branch (`my-feature-branch`), spin up a temporary worktree outside your main repo folder:
   ```bash
   git worktree add ../atcr-review-branch my-feature-branch
   ```
2. **Dispatch the AI Agent:**
   Navigate into that new directory and kick off the ATCR review:
   ```bash
   cd ../atcr-review-branch
   atcr review --auto-fix
   ```
3. **Keep Coding!**
   Go right back to your main project directory. Switch to `main`, start your next ticket, or grab a coffee. The Docker sandbox will mount the snapshot of the *worktree*, not your primary directory, so you have zero file-locking issues.
   ```bash
   cd ../atcr
   git checkout -b my-next-feature
   ```
4. **Merge and Cleanup:**
   Once ATCR completes and pushes the verified fixes to the branch (or opens a PR), simply clean up the worktree:
   ```bash
   git worktree remove ../atcr-review-branch
   ```

## 4. Conclusion
- Reinforce the ATCR philosophy: Enterprise-grade async review pipelines shouldn't require SaaS vendor lock-in. 
- By pairing a powerful local CLI tool with standard Git features, you get the exact same CI/CD experience entirely on your local machine, at a fraction of the API cost.
