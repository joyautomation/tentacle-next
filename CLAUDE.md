# Tentacle Next

## Auto-deploy

After completing Go code changes (editing files in `internal/`, `cmd/`, or `types/`), automatically run `/deploy-dev` to build and deploy to the dev container. Do not run it after every individual edit — only once you believe the task is complete and the code is ready to test.

Web-only changes (`web/src/`) do NOT need a deploy — the vite dev server on the dev container picks them up via HMR automatically.

## Auto-commit and push

After completing each logical batch of work (a fix, a feature, a refactor), commit with a descriptive message AND push to origin. Do not wait for the user to ask — uncommitted/unpushed work has been lost before (disk failure, etc.). One commit per logical unit of work, not per individual edit. Push immediately after committing so WIP is backed up to GitHub. This applies to feature branches in worktrees too, not just `main`.
