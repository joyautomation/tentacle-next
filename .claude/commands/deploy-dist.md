Deploy the latest GitHub release to a fresh incus container for manual testing.

Run from the tentacle-next repo root:

```
bash scripts/deploy-dist.sh
```

To reuse an existing container (re-downloads the binary):

```
bash scripts/deploy-dist.sh --keep
```

Report the output to the user, including the container URL.
