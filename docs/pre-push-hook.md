# The pre-push hook

The detail behind [The pre-push hook](../README.md#the-pre-push-hook) in the README.

One rule here has a mechanical half, and a clone runs it only once it has opted in: nothing
leaves this repository except its git identity. `.githooks/pre-push` refuses a push whose
commits carry an identity this clone has not declared, and it refuses a push whose new
commits add a line, or carry one in their message or header, that matches a shape that
personal data takes — an address of the kind mail uses, a phone number, a bank account
number, a postal address, each only in the format its pattern spells out, so a format it was
not written for passes. It reads every new commit rather than the tip alone, because a push
publishes them all, and a line removed in a later commit stays reachable by its hash for
anyone who clones. A commit is new unless the remote's old tip for that ref reaches it, or
one of this clone's tracking refs for the destination remote does. Of a new commit it reads
only the lines it adds: every other line of its tree was added by another new commit or is
already on the remote, checked on its way there. A merge adds a line only when the line is
new against every parent: each commit is diffed once per parent, as text even where a file
holds a NUL byte or is marked binary, and a line counts only when every one of those diffs
adds it. A committer may also be GitHub's web-flow identity, the one a merge through the web
UI records; as an author it is still undeclared.

Each new commit's object is read as well, as stored, the way a tag object is: the message,
since a message is as public as a blob and trailers are where addresses ride in, and every
line of the header. `git log` shows neither a header line git does not know nor anything
after a NUL byte in the message, and a push publishes both.

| A hand-made commit object holds … | local bare repository, `receive.fsckObjects` off (git's default) | on |
|---|---|---|
| an extra header line after the committer | accepted | accepted |
| an extra header line between the author and the committer | accepted | refused |
| a NUL byte in the message | accepted | refused |
| an extra header line before the tree | refused by the sending git itself | refused |

(*measured with git 2.55.0, 2026-10-02; what GitHub's receiving end accepts was not
checked.*) Left out of the object are what the identity check reads, and a signature's base64
lines (below). The identity is the author's or the committer's `Name <address>` as git splits
it — up to the first `<` and on to the first `>` after it — on the one header line of that
role, before any NUL. Whatever follows on that line is read, a second address included. `git
log` converts the header from the encoding it names as well, and in UTF-7 that turned one
author line as stored into two, the declared one last (*measured with git 2.55.0,
2026-10-02*): in a commit whose header names an encoding other than UTF-8, the identity is
left out only when it is declared as stored, or when git shows a declared one — a name stored
in Latin-1 or Shift_JIS. For an encoding outside the list below, only when that one differs
from it in nothing but characters outside ASCII.

Every shape is matched twice, under your own locale and under `C`, and a line either match
finds is a finding. The patterns hold characters outside ASCII, which takes your locale; but
a byte that is not valid in your locale's encoding matches no bracket expression there, not
even `[^0-9]`, and such bytes do arrive: a file is stored as it was written, a commit made by
a tool other than git's own commands can keep them in its message, and a tag object is raw
bytes. A Latin-1 no-break space right before a phone number hid it from a UTF-8 locale and
not from `C` (*measured with GNU grep 3.12, 2026-10-01*). The author and committer identities
are read with `git log --encoding=UTF-8`. Without that flag git re-encodes what it prints
into `i18n.logOutputEncoding`, or into `i18n.commitEncoding` when that is unset; set to
Latin-1, either one made a declared name outside ASCII read as undeclared, and hid a postcode
before a place that starts with an umlaut in a message read through `git log` from both
matches (*measured with git 2.55.0, 2026-10-01*). The message is read from the object, where
neither setting reaches. git converts a commit from the encoding its header names, though,
and in Latin-1 a place with an umlaut matches no pattern under a UTF-8 locale or `C`, so a
commit whose header names an encoding other than UTF-8 is read whole as `git log --pretty=raw
--encoding=UTF-8` converts and prints it, too — header and message, since in UTF-7 a header
line or a signature line that holds no shape as stored can hold one to git (*measured with
git 2.55.0, 2026-10-02*). An encoding line that names nothing counts as such a header: git
takes the locale's character set for it, and under a Latin-1 locale it turned a UTF-8 place
into characters no pattern holds (*measured with git 2.55.0, 2026-10-02*), which the object
as stored still has as written. The header can also be wrong: with `i18n.commitEncoding` set
to Latin-1 and UTF-8 typed in, `git commit` writes UTF-8 under a Latin-1 header, the
conversion turns the same umlaut into two characters that neither match finds, and `git log
--format` converts even under `--encoding=none`. The object as stored holds the umlaut as
typed, and the author or committer of such a commit counts as declared when either reading
does — the stored one only when the header holds a single line for that role, since git shows
the last of several (*measured with git 2.55.0, 2026-10-01*). For an encoding outside the
list below, also only when the identity git shows differs from the stored one in nothing but
characters outside ASCII: a conversion that changes more is not the same name read two ways,
and in UTF-7 an extra header line became an author line of its own, which git showed, while
the stored header held one declared author (*measured with git 2.55.0, 2026-10-02*). The
encodings on the list make no line of their own, and Shift_JIS, Big5 and GBK keep bytes in
the ASCII range inside a character, so there the comparison is not made. A tag object is read
byte for byte throughout, so a tagger whose name is stored in Latin-1 is read as written.

| The header's encoding line names … | read as stored | read as git converts it | finding of its own |
|---|---|---|---|
| nothing (no line), UTF-8 or utf8 | yes | — | — |
| an empty value, US-ASCII or ASCII, ISO-8859-*, latinN, windows-125* or CP125*, KOI8-R/U, EUC-JP/KR/CN/TW, GB2312, GBK, GB18030, Big5, Big5-HKSCS, Shift_JIS, SJIS, CP932, Windows-31J | yes | yes | — |
| any other encoding: UTF-7, UTF-16, UTF-32, EBCDIC (IBM037 …), ISO-2022-* … | yes | yes | `unread encoding` |

Reading both ways covers what git shows only where a conversion makes an ASCII character from
that same byte alone and never makes a newline or a NUL. In UTF-7 neither holds: an extra
header line became an author line once converted, and an escaped NUL ended git's converted
text before a shape that is only encoded as stored (*measured with git 2.55.0, 2026-10-02*).
So a commit whose header names an encoding outside the list is a finding, as a ref to a blob
or a tree is; `IMPRINT_PUSH_ANYWAY` with a reason is the way past. Two more cases are the
same finding, for a listed encoding too, since a Latin-1 place matches nothing as stored: a
commit that holds a NUL byte, where `git log` stops converting, and a conversion git cannot
finish — one byte the encoding leaves undefined is enough, and git prints the commit as
stored, encoding line and all (*both measured with git 2.55.0, 2026-10-02*). `git commit`
writes no NUL, but it does store a name as UTF-8 under whatever encoding
`i18n.commitEncoding` names: with windows-1252, a `user.name` whose UTF-8 bytes include one
that windows-1252 leaves undefined — 0x81, 0x8D, 0x8F, 0x90 or 0x9D, as in `Ł`, `Á` or `Í` —
makes every commit such a finding, while `ł` or `ö` do not, and with US-ASCII any name
outside ASCII does (*measured with git 2.55.0, 2026-10-02*). UTF-8 as the commit encoding
avoids it. This is the hook's choice of list, made 2026-10-02, and a name can be added once
its conversion is shown to keep to that rule.

An annotated tag is read as well, since it carries what a commit carries. Its tagger has to
be a declared identity, as an author has to, and a tag that names no tagger is a finding.
Everything else in the tag object is checked against the same shapes: the message, a
signature block but for its base64 lines (below), the tag's name, any other header line, and
whatever follows the tagger's identity on its line. That includes a tag whose header never
ends in an empty line, from which git itself reads no message although its bytes are
published. A pushed ref is followed through every tag object it leads to, so the tags inside
a nested tag are read as well as the outer one; where the chain ends in a commit, that
commit's new history is checked as for any branch. A new tag on a commit the remote already
has adds no commit, and its tag object is still read. Tags have no tracking refs, so a tag
object the remote already holds is read again when it is pushed under another name or inside
another tag.

| Where a signature sits | Its base64 lines | Everything else in it |
|---|---|---|
| `gpgsig` or `gpgsig-sha256` header of a commit | not matched, inside the last signature armour | matched: armour lines, `Comment:` and other armour headers |
| `mergetag` header of a merge (the merged tag) | not matched, inside the last signature armour | matched: tag name, message, any other armour, armour lines; tagger held to the declared identities |
| a tag object pushed as such (the tag check) | not matched, inside the last signature armour | matched: the message before it, armour lines and headers, any other armour, lines after its END line |

A signed commit carries its signature in a `gpgsig` header (`gpgsig-sha256` in a SHA-256
repository), and a merge of a signed tag carries the tag, tagger and signature, in a
`mergetag` header; git writes one only for a tag whose message holds a line that starts with
a signature marker — signed, or typed into an unsigned tag's message (*measured with git
2.55.0, 2026-10-02*). A signature's base64 lines are encoded bytes in which no shape can be
read, yet a run of their letters and digits takes the IBAN pattern's form now and then: in
random bytes the size of a signature, base64 encoded and wrapped as git stores them, about
one OpenPGP Ed25519 signature in 2,400, one SSH Ed25519 signature in 1,450, one OpenPGP
RSA-4096 signature in 425 and one 3 KB X.509 signature in 85 (*simulated, 200,000 runs each,
400,000 for SSH at the 232 characters of a real one and 50,000 for X.509, 2026-10-02; none of
the signed commits in this repository's history held such a line that day*). So in those
headers a signature's base64 lines are not matched: from the last line that opens a
signature, as git finds one — a line that starts with `-----BEGIN PGP SIGNATURE-----`, `PGP
MESSAGE`, `SSH SIGNATURE` or `SIGNED MESSAGE` — to the `-----END …-----` line after it — an
armour only when that line is there — a line made only of base64 characters is skipped. An
armour of another name, or one before the last, is matched like any text. The merged tag's
tagger is held to the declared identities as a pushed tag's tagger is, and left out of the
shapes; a tagger line with no `>` in it is a check that could not run. In a merge read as
converted, a tagger git shows counts as declared when the one stored in its place is — for an
encoding outside the list above, only when the two differ in nothing but characters outside
ASCII, as for an author. A merged tag that names no tagger brings no identity along and is no
finding, unlike a pushed tag with none. A signed tag pushed as a tag object is read the same
way: from the last line in it that opens a signature, as git finds one in a tag, to its END
line — wherever in the object that is, and only when there is one — a line made only of
base64 characters is not matched, and everything else is (the owner's call, 2026-10-02).

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
inside a bracket, because under `C` a bracket reads the umlauts' bytes one by one: there, five
digits before a word that starts with a lowercase umlaut or `ß` were a postcode and place, and
a place that starts with a capital umlaut was missed. Spelled out, `C` reads each umlaut as a
UTF-8 locale does (*measured with GNU grep 3.12, 2026-10-02*). And the hook diffs lines, with
no copy detection, so lines copied or moved into another file are reported again even though
they are already published; `IMPRINT_PUSH_ANYWAY` with a reason is the way past that.

Shapes are matched line by line, in a commit object as in a file or a tag, so one broken
across a line end passes; that includes the first paragraph of a message, whose lines `git
log`'s subject would join. The signature rule above leaves a hole of its own: a line made
only of base64 characters can hold an IBAN written without spaces, or a phone number with a
slash, and between a signature's first and END line it passes. In a commit's `gpgsig` header
git puts nothing there but the signature, and only a commit object made by hand can. A tag's
message is a person's text: `git tag -a` writes such an armour into it as typed, and `git
merge -m` of that tag copies it into the merge's `mergetag` header, unsigned or not
(*measured with git 2.55.0, 2026-10-02*). A tag signed in a SHA-256 repository with a SHA-1
compatibility hash carries its signature twice, the second time in a `gpgsig` header of the
tag, and the base64 lines there are matched like any text (*measured in review with git
2.47.3, 2026-10-02*). A merge of a tag signed by someone this clone has not declared is
refused for its tagger: `IMPRINT_PUSH_ANYWAY` with a reason is the way past, or a merge of
the commit the tag points at rather than of the tag. And `git merge --no-edit` of a signed
tag writes the tag's signature and the report on verifying it into the merge's message, as
lines that start with `#`; they are matched as the rest of the message is, base64 included.
For an SSH key git knows through `gpg.ssh.allowedSignersFile`, that report is `Good "git"
signature for <principal> …`, and a principal that is an address is a finding (*both measured
with git 2.55.0, 2026-10-02*). A message given with `-m` leaves them out.

`git push --no-verify` skips every hook silently, and the script cannot see that it happened.
`IMPRINT_PUSH_ANYWAY='reason' git push` is the loud alternative — the findings are printed in
full, the reason is echoed back, and the push proceeds.
