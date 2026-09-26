# The pre-push hook

The detail behind [The pre-push hook](../README.md#the-pre-push-hook) in the README.

One rule here has a mechanical half, and this repository now runs it: nothing leaves this
repository except its git identity. `.githooks/pre-push` refuses a push whose commits carry
an identity this clone has not declared, and it refuses a push whose tracked content or
commit messages match a shape that personal data takes — an address of the kind mail uses, a
phone number, a bank account number, a postal address, each only in the format its pattern
spells out, so a format it was not written for passes. It reads every
commit in the pushed range rather than the tip alone, because a push publishes the whole
range, and a file removed in a later commit stays reachable by its hash for anyone who
clones. Commit messages are checked alongside the trees, since a message is as public as a
blob and trailers are where addresses ride in.

**It does not arrive with a clone.** Git runs hooks out of `.git/hooks` unless it is told
otherwise, and nothing in a checkout can tell it for you. Each clone needs one line:

```
git config core.hooksPath .githooks
```

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

`git push --no-verify` skips every hook silently, and the script cannot see that it happened.
`IMPRINT_PUSH_ANYWAY='reason' git push` is the loud alternative — the findings are printed in
full, the reason is echoed back, and the push proceeds.
