## Git Strategy & Workflow

#### Core Philosophy
We follow a **GitHub Flow** / **Trunk-Based Development** approach.
- **main**: The single source of truth. Always deployable.
- **feature/**: Short-lived branches for all changes.

#### Workflow

1.  **Branch**: Create `feature/short-description` from `main`.
2.  **Commit**: Small, atomic commits with [Conventional Commits](https://www.conventionalcommits.org/) messages.
    - `feat: add login`
    - `fix: resolve auth bug`
3.  **Pull Request**:
    - Open PR against `main`.
    - Fill out the PR Template (if available) or describe: *What*, *Why*, and *How*.
    - Ensure all CI checks pass (Tests, Lint).
4.  **Review**: At least one approval required.
5.  **Merge**: **Squash and Merge** to maintain a linear history on `main`.
6.  **Cleanup**: Delete the feature branch.

#### Rules

###### Commit Messages
- **Format**: `type(scope): description`
- **Types**: `feat`, `fix`, `docs`, `style`, `refactor`, `test`, `chore`.
- **Example**: `feat(auth): implement google login`

###### Pull Requests
- **Scope**: One logical change per PR.
- **Size**: Keep PRs small (< 400 lines ideally) for better reviews.
- **Verification**: Include instructions on how the change was tested locally (e.g., test commands executed, MCP inspector logs).

###### CI/CD
- **Gating**: PRs cannot be merged if the GitHub Actions `Go CI` workflow fails (which checks formatting, vetting, linting, and runs unit tests).
- **Deploy**: Merging to `main` builds production release binaries and packages the MCP server.
