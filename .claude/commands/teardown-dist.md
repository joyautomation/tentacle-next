Tear down the tentacle-dist incus container.

Run from the tentacle-next repo root:

```
incus exec tentacle-dist -- tailscale logout 2>/dev/null || true
incus delete tentacle-dist --force
```

The `tailscale logout` frees the `tentacle-dist` hostname on the tailnet so the next deploy can reuse it instead of becoming `tentacle-dist-1`, `-2`, etc.

Report the output to the user.
