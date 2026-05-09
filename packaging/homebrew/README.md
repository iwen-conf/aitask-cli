# Homebrew Packaging

`brew install iwen-conf/tap/aitask` installs the whole AITask CLI suite from a
single formula:

- `aitask`
- `aitask-watch`
- `aitask-worker`
- `aitask-agent-watch`

The old standalone `iwen-conf/tap/aitask-watch` formula is **retired**: it is
deleted from `iwen-conf/homebrew-tap` and its source repo
(`iwen-conf/aitask-watch`) is archived. The new formula keeps
`conflicts_with "aitask-watch"` for one release cycle so that any machine still
holding the old install gets a clear error message instead of a silent binary
collision.

## Release Flow

For each release:

1. Sync `cli/` to `github.com/iwen-conf/aitask-cli`. The published repo must
   contain the four top-level binary directories (`aitask/`, `aitask-watch/`,
   `aitask-worker/`, `aitask-agent-watch/`) at its root, plus shared
   `internal/` and `pkg/` packages. Remove any legacy `cmd/aitask` entry point
   so older formulas cannot resolve it.
2. Tag the published CLI repo (e.g. `v0.3.0`) and push the tag.
3. Compute the tarball SHA256:
   ```bash
   curl -sL https://github.com/iwen-conf/aitask-cli/archive/refs/tags/v0.3.0.tar.gz \
     | shasum -a 256
   ```
4. In `iwen-conf/homebrew-tap`:
   - Replace `Formula/aitask.rb` with the file in this directory.
   - Update its `url` and `sha256` to the new tag and digest.
   - Delete `Formula/aitask-watch.rb` from the tap on the same commit.
5. Commit + push the tap. End-user install:
   ```bash
   brew uninstall iwen-conf/tap/aitask-watch  # only if old formula was installed
   brew uninstall iwen-conf/tap/aitask        # only if pre-suite v0.2.x was installed
   brew update
   brew install iwen-conf/tap/aitask
   ```

## Local Formula Build Check

Run the same build commands the formula uses to validate before tagging:

```bash
cd cli
rm -rf dist/homebrew-check
mkdir -p dist/homebrew-check
for binary in aitask aitask-watch aitask-worker aitask-agent-watch; do
  go build -ldflags "-s -w -X main.version=v0.0.0" \
    -o "dist/homebrew-check/${binary}" "./${binary}"
  "dist/homebrew-check/${binary}" --version
done
```
