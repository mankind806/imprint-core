# The pre-push hook

The detail behind [The pre-push hook](../README.md#the-pre-push-hook) in the README.

One rule here has a mechanical half, and a clone runs it only once it has opted in: nothing
leaves this repository except its git identity. `.githooks/pre-push` refuses a push whose
commits carry an identity this clone has not declared, and it refuses a push whose new
commits add a line, or carry a message, that matches a shape that personal data takes — an
address of the kind mail uses, a phone number, a bank account number, a postal address, each
only in the format its pattern spells out, so a format it was not written for passes. It
reads every new commit rather than the tip alone, because a push publishes them all, and a
line removed in a later commit stays reachable by its hash for anyone who clones. A commit is
new unless the remote's old tip for that ref reaches it, or one of this clone's tracking refs
for the destination remote does. Of a new commit it reads only the lines it adds: every other
line of its tree was added by another new commit or is already on the remote, checked on its
way there. A merge adds a line only when the line is new against every parent: each commit
is diffed once per parent, as text even where a file holds a NUL byte or is marked binary,
and a line counts only when every one of those diffs adds it. Commit
messages are checked alongside the added lines, since a message is as public as a blob and
trailers are where addresses ride in. A committer may also be GitHub's web-flow identity, the
one a merge through the web UI records; as an author it is still undeclared.

**It does not arrive with a clone.** Git runs hooks out of `.git/hooks` unless it is told
otherwise, and nothing in a checkout can tell it for you. Each clone needs one line:

```
git config core.hooksPath .githooks
```

Nothing turns it on by default. The check workflow neither sets `core.hooksPath` nor calls
the script, and, absent that line, `.git/hooks` has no copy of it either. *Measured
2026-09-27: `git config --get core.hooksPath` returned nothing and `.git/hooks/pre-push` did
not exist in the maintainer's own clone, and the workflow was read. Re-check by 2026-12-27.*

A co-author or a fork declares a second identity with
`git config --add imprint.allowedIdentity 'Name <address>'`. Your own `user.name` and
`user.email` count as declared without being listed.

**What it does not enforce is the larger half.** The rule asks for abstraction: a worked case
told generically, with no organisation, no product, no ticket number, no path off anybody's
machine. Whether a passage is abstract is a question of meaning, and no pattern answers it. A
page naming a real employer in plain words passes this hook exactly as a properly abstracted
one does, and a blocklist of real names would not change that — it would only look as though
it had. In the four states this repository sorts every rule into: the shapes and the
identity are **enforced**; reading for abstraction stays a **behaviour rule** with nothing
behind it. The hook says so itself, in every report it prints.

It fails closed. Every way it can fail to finish — a git command that errors, an identity
this clone never set, a temporary directory it cannot create — ends in a refused push,
because a gate that waves you through when it breaks is indistinguishable from one that
checked. One empty case is not such a failure and took a refused push to find: git runs the
hook even when the remote is already up to date, and pipes in an empty ref list. Nothing is
published in that run, so there is nothing to check, and the hook says so and lets it
through — but only when git is the one calling, which is a hook invoked with a remote name
and location and handed a pipe rather than a terminal. An empty list from anything else is
still refused. A check that could not run is reported apart from a finding, and no override
covers it: "I could not look" and "I looked and found nothing" must never share an exit code.

**What it takes the remote to hold is this clone's word for it.** Only the destination's own
tracking refs count — `refs/remotes/<remote>/*` for the remote git names — so a commit that
only a private mirror's tracking refs hold is still checked on its way to a public remote. A
push to a location rather than a configured remote has no tracking refs to trust, and then
everything the remote's old tip does not reach is checked. And tracking refs are what this
clone last fetched, not what the remote holds now: after `git remote set-url` points a remote
at a different repository, the old tracking refs still exclude commits the new one never
received. Run `git fetch --prune` first. The tracking refs count only when the push goes to
the URL they were fetched from: with a `pushurl`, a `pushInsteadOf` or a second `url` on the
remote, git hands the hook another location, and then everything the remote's old tip does
not reach is checked, as for a push to a location.

Some of what a push publishes is not read at all: the message and tagger of an annotated tag,
and a ref that points straight at a blob or a tree. And the hook diffs lines, with no copy
detection, so lines copied or moved into another file are reported again even though they are
already published; `IMPRINT_PUSH_ANYWAY` with a reason is the way past that.

`git push --no-verify` skips every hook silently, and the script cannot see that it happened.
`IMPRINT_PUSH_ANYWAY='reason' git push` is the loud alternative — the findings are printed in
full, the reason is echoed back, and the push proceeds.
