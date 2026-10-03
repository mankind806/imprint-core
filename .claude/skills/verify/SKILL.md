---
name: verify
description: "Runs this repository's own checks, the same steps as the CI check job, right before a commit. Skipped for commits touching only docs or only tests."
---

# Verify

This skill runs the repository's test and check suite locally right before a commit. In this repository, it stands in for Claude Code's bundled `/verify` because there is no application to build or launch. Its steps mirror the CI gate defined in `.github/workflows/check.yml`.

## Before the commit

Record the working tree state before running step 1. The before-file lives in the git directory so it does not itself change the working tree:

```sh
git status --porcelain --untracked-files=all >"$(git rev-parse --git-dir)/verify-before.txt"
```

1. `go test` in `tools/imprint-dev` (including the zero-tests guard N16):
```sh
(
  cd tools/imprint-dev
  n=$(go test -list '.*' ./... | grep -c '^Test' || true)
  echo "Tests found: $n"
  if [ "$n" -eq 0 ]; then
    echo "go test found zero tests; that counts as red (N16)"
    exit 1
  fi
  go test ./... && go test -count=1 -v -run '^TestPrePushToolchains$' .
)
```

2. Codex arrival test (offline, needs python3):
```sh
sh tools/test-arrival-codex.sh
```

3. Antigravity arrival test (offline, needs python3):
```sh
sh tools/test-arrival-agy.sh
```

4. rechte-anwenden.sh test:
```sh
bash setup/vorlagen/rechte-anwenden-test.sh
```

5. `imprint-dev check`:
```sh
go run ./tools/imprint-dev check --root .
```

6. typesafe Python tests (offline, no key). CI runs them on Python 3.14 (unicodedata 16.0.0, `actions/setup-python`); with another `python3`, the tests that need Unicode 16.0.0 skip and say so:
```sh
(
  names_dir=$(mktemp -d)
  trap 'rm -rf "$names_dir"' EXIT
  printf 'Max Mustermann\nErika Musterfrau\n' >"$names_dir/typesafe-names.txt"
  export PYTHONDONTWRITEBYTECODE=1
  export TYPESAFE_NAMES_FILE="$names_dir/typesafe-names.txt"
  python3 -m unittest discover -s tools/typesafe -v && python3 tools/typesafe/tests/mask-test.py
)
```

7. Working tree unchanged (red if it prints a difference):
```sh
git status --porcelain --untracked-files=all | diff "$(git rev-parse --git-dir)/verify-before.txt" -
rm -f "$(git rev-parse --git-dir)/verify-before.txt"
```

## After the commit, before the push

```sh
go run ./tools/imprint-dev commit-check --root . --range origin/main..HEAD
```

CI repeats this check with the pull-request flags on every pull request.

## Reporting

State each step as passed or failed with the failing output. A red step stops the commit. Never make a step green by weakening, skipping, or deleting a test or check.

## What enforces this

| Rule | Enforcement |
|---|---|
| Run these steps right before a commit | **Behaviour rule**: Claude Code only tells Claude to run this skill; nothing stops a commit without it. |
| The same steps pass before merge | **Enforced**: the `check` job in `.github/workflows/check.yml` runs on every pull request and every push to main. |
