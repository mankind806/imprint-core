## What this changes

<!-- One or two sentences. -->

## Evidence

<!-- The real incident, told generically, or the test that shows the change is needed.
     A new rule or skill without evidence is not merged. -->

## Checklist

- [ ] `go run ./tools/imprint-dev check --root .` exits 0
- [ ] `go test ./...` in `tools/imprint-dev` passes
- [ ] No addresses, paths, host names or employer terms — in the files or the commit messages
- [ ] Examples use only `.example`, `.test` or `.invalid` domains
- [ ] I have the right to license this contribution under MIT, including with respect to any employer.

## Assisted-by

<!-- The AI tool or model that helped, without an address, or "none". Keep this the LAST
     line of the pull request text: a squash merge on this repository uses the pull request
     body as the commit message (`squash_merge_commit_message` is `PR_BODY`), so this line
     becomes the merge commit's trailer only if nothing follows it. No addresses or paths
     anywhere in this text either, for the same reason. -->
Assisted-by:
