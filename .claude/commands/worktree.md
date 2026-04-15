Manage development worktrees and their isolated dev containers.

Pass the desired subcommand as arguments: create, destroy, list, deploy, start, stop, shell, logs.

## Examples

- `/worktree create plc` — new worktree + container for PLC module work
- `/worktree deploy profinet` — build and deploy to the profinet container
- `/worktree list` — show all worktrees and container status
- `/worktree destroy modbus` — tear down worktree + container

## Execution

Run from the tentacle-next repo root:

```
bash scripts/worktree-dev.sh $ARGUMENTS
```

Report the full output to the user. If creating, include the container IP and access URLs.
