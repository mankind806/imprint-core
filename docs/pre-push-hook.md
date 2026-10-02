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

Every shape is matched twice, under your own locale and under `C`, and a line either match
finds is a finding. The patterns hold characters outside ASCII, which takes your locale; but
a byte that is not valid in your locale's encoding matches no bracket expression there, not
even `[^0-9]`, and such bytes do arrive: a file is stored as it was written, a commit made by
a tool other than git's own commands can keep them in its message, and a tag object is raw
bytes. A Latin-1 no-break space right before a phone number hid it from a UTF-8 locale and not
from `C` (*measured with GNU grep 3.12, 2026-10-01*). Commit messages and the author and
committer identities are read with `git log --encoding=UTF-8`. Without that flag git
re-encodes what it prints into `i18n.logOutputEncoding`, or into `i18n.commitEncoding` when
that is unset. Set to Latin-1, either one hid a postcode before a place that starts with an
umlaut from both matches, and made a declared name outside ASCII read as undeclared
(*measured with git 2.55.0, 2026-10-01*). The flag has a cost of its own: git converts from
whatever encoding a commit's header names, and the header can be wrong. With
`i18n.commitEncoding` set to Latin-1 and UTF-8 typed in, `git commit` writes UTF-8 under a
Latin-1 header, and the conversion turns the same umlaut into two characters that neither
match finds. `git log --format` converts even under `--encoding=none`, so a commit whose header
names an encoding other than UTF-8 is also read straight from its object: its message is
matched as stored too, and its author or committer counts as declared when either reading
does — the stored one only when the header holds a single line for that role, since git shows
the last of several (*measured with git 2.55.0, 2026-10-01*). A tag object is read byte for
byte throughout, so a tagger whose name is stored in Latin-1 is read as written.

An annotated tag is read as well, since it carries what a commit carries. Its tagger has to be
a declared identity, as an author has to, and a tag that names no tagger is a finding.
Everything else in the tag object is checked against the same shapes: the message, a signature
block, the tag's name, any other header line, and whatever follows the tagger's identity on its
line. That includes a tag whose header never ends in an empty line, from which git itself reads
no message although its bytes are published. A pushed ref is followed
through every tag object it leads to, so the tags inside a nested tag are read as well as the
outer one; where the chain ends in a commit, that commit's new history is checked as for any
branch. A new tag on a commit the remote already has adds no commit, and its tag object is
still read. Tags have no tracking refs, so a tag object the remote already holds is read again
when it is pushed under another name or inside another tag.

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
this clone never set, a temporary directory it cannot create, a line of its own report it
cannot write down on a full disk — ends in a refused push, because a gate that waves you
through when it breaks is indistinguishable from one that checked. One empty case is not
such a failure and took a refused push to find: git runs the hook even when the remote is
already up to date, and pipes in an empty ref list. Nothing is
published in that run, so there is nothing to check, and the hook says so and lets it
through — but only when git is the one calling, which is a hook invoked with a remote name
and location and handed a pipe rather than a terminal. An empty list from anything else is
still refused. A check that could not run is reported apart from a finding, and no override
covers it: "I could not look" and "I looked and found nothing" must never share an exit code.

**What it takes the remote to hold is this clone's word for it.** Only the destination's own
tracking refs count — `refs/remotes/<remote>/*` for the remote git names — so a commit that
only a private mirror's tracking refs hold is still checked on its way to a public remote.
They count only while no other remote stores refs under that prefix: when another remote's
name starts with `<remote>/`, or one of its fetch refspecs points there (a mirror's
`+refs/*:refs/*` included, and `remotes/<remote>/…` without the `refs/`, which git
completes), none of them counts, and everything the remote's old tip does not reach is
checked. Two places define remotes that `git remote` here does not show, and the hook reads
neither: a file in the git directory's legacy `remotes/` folder, whose `Pull:` lines git
still fetches by, and, with `extensions.worktreeConfig` on, another worktree's
`config.worktree`, where a remote of its own or a `url.<base>.insteadOf` can make that
worktree's fetch store another repository's commits under `refs/remotes/<remote>/`. While
either is in use, the tracking refs are not trusted at all. A conditional include
(`includeIf`, on `onbranch:` or `gitdir:`) can do the same between worktrees without the
extension, and nothing here covers it. A push by name to a remote whose fetch refspec stores
into `remotes/<x>/…` leaves a plain file in the legacy folder as well, which
`git remote remove` does not delete; to clear such a leftover, look at what the `remotes/`
folder under `git rev-parse --git-common-dir` holds first. `git clone --sparse` and
`git sparse-checkout set` turn `extensions.worktreeConfig` on, and `git sparse-checkout
disable` leaves it on, so such a clone has everything the remote's old tip does not reach
checked: push with `IMPRINT_PUSH_ANYWAY` and a reason, or, after `git sparse-checkout
disable`, unset `extensions.worktreeConfig` when no worktree's `config.worktree` holds
anything but the keys `disable` set to false. *Measured with git 2.55.0, 2026-10-01.
Re-check by 2027-01-01.* A
push to a location rather than a configured remote has no tracking refs to trust, and then
everything the remote's old tip does not reach is checked. And tracking refs are what this
clone last fetched, not what the remote holds now: after `git remote set-url` points a remote
at a different repository, the old tracking refs still exclude commits the new one never
received. Run `git fetch --prune` first. The tracking refs count only when the push goes to
the URL they were fetched from: with a `pushurl`, a `pushInsteadOf` or a second `url` on the
remote, git hands the hook another location, and then everything the remote's old tip does
not reach is checked, as for a push to a location.

A ref that points straight at a blob or a tree, or at a tag that ends in one, is refused
unread, as a finding. Git takes such a ref: a local bare repository accepted a blob under
`refs/tags/` and a tree under a namespace of its own, and refused a blob under `refs/heads/`
(*measured with git 2.55.0, 2026-10-01; what a hosted remote accepts was not checked*). A blob
or a tree carries no history that would tell its new lines from published ones.
`IMPRINT_PUSH_ANYWAY` with a reason is the way to push one on purpose (the owner's call,
2026-10-01). The names of refs are not read at all: a branch name, or the name of a tag without
a tag object, passes whatever it holds; an annotated tag's name is read only as a line of its
tag object. Matching under `C` as well closes the gap for a byte next to a shape, not inside
one: the patterns are written in UTF-8, so a place name spelled in Latin-1 still passes, and
so does a phone number whose separator is a no-break space, in either encoding, since the
pattern takes only a space, a slash or a hyphen there (*measured with GNU grep 3.12,
2026-10-01*). The postcode pattern spells each umlaut out as an alternative rather than
inside a bracket, because under `C` a bracket reads the umlauts' bytes one by one: five digits
before a word that starts with a lowercase umlaut were then a postcode and place, and a place
that starts with a capital one was missed. Spelled out, `C` reads each umlaut as a UTF-8 locale
does (*measured with GNU grep 3.12, 2026-10-02*). And the hook diffs lines, with no copy
detection, so lines copied or moved into another file are reported again even though they are
already published; `IMPRINT_PUSH_ANYWAY` with a reason is the way past that.

`git push --no-verify` skips every hook silently, and the script cannot see that it happened.
`IMPRINT_PUSH_ANYWAY='reason' git push` is the loud alternative — the findings are printed in
full, the reason is echoed back, and the push proceeds.
