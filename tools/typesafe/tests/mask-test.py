#!/usr/bin/env python3
"""Secret values must never survive ts_common.mask (regression: value after key= leaked, found in PR #29 review)."""
import atexit, os, sys, tempfile
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
import ts_common as tc
# Own temporary names file with synthetic names only: this test never reads or
# writes the real, private ~/.config/typesafe/names.txt.
_fd, names_file = tempfile.mkstemp(prefix="typesafe-mask-test-names-", suffix=".txt")
with os.fdopen(_fd, "w", encoding="utf-8") as f:
    f.write("# synthetic names for the mask test\nMax Mustermann\nErika Musterfrau\n")
atexit.register(os.remove, names_file)
os.environ["TYPESAFE_NAMES_FILE"] = names_file

CASES = {  # text -> secret that must be gone (None = nothing may be masked)
    "api_key=SECRETVALUE99 mail a@b.example": "SECRETVALUE99",
    "password=hunter2 end": "hunter2",
    "export API_KEY=abcd1234 x": "abcd1234",
    "Authorization: Bearer sk-abc123 def": "sk-abc123",
    '{"token": "t0k3n", "x": 1}': "t0k3n",
    "{'secret':'s3cr3t'}": "s3cr3t",
    "db_pass" + "word: geheim123;": "geheim123",  # built from parts: no password in this file
    "mail an max.muster@mail.example": "max.muster@mail.example",
    "pytest -q tests/test_api.py": None,
    'git commit -m "fix token refresh"': None,
    # Names and addresses
    "Treffen mit Max Mustermann in Berlin": ("Max", "Mustermann"),
    "Post an Erika in der Hauptstraße 12": ("Erika", "Hauptstraße 12"),
    "Wohnort: 80331 München": "80331 München",
}
fail = 0
for text, secret in CASES.items():
    masked, hits = tc.mask(text)
    if secret is None:
        ok = (masked == text)
    elif isinstance(secret, (tuple, list)):
        ok = all(s not in masked for s in secret)
    else:
        ok = (secret not in masked)
    fail += not ok
    print(("ok   " if ok else "FAIL ") + repr(masked))
print(f"{len(CASES) - fail}/{len(CASES)}")
sys.exit(1 if fail else 0)

