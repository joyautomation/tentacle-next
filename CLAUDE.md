# Tentacle Next

## Auto-deploy

After completing Go code changes (editing files in `internal/`, `cmd/`, or `types/`), automatically run `/deploy-dev` to build and deploy to the dev container. Do not run it after every individual edit — only once you believe the task is complete and the code is ready to test.

Web-only changes (`web/src/`) do NOT need a deploy — the vite dev server on the dev container picks them up via HMR automatically.

## Auto-commit

After completing each logical batch of work (a fix, a feature, a refactor), commit with a descriptive message. Do not wait for the user to ask — uncommitted work has been lost before. One commit per logical unit of work, not per individual edit.
