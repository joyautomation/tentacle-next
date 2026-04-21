Run API performance tests against the tentacle-dist container.

Run from the tentacle-next repo root:

```
bash scripts/api-perf.sh
```

To use a custom threshold (in milliseconds, default 500):

```
bash scripts/api-perf.sh 200
```

Report the full output to the user. If any endpoints are SLOW or ERR, investigate and explain what is wrong.
