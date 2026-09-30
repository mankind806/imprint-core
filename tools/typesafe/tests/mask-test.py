#!/usr/bin/env python3
"""Secret values must never survive ts_common.mask (regression: value after key= leaked, found in PR #29 review)."""
import os, sys
sys.path.insert(0, os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
import ts_common as tc
names_file = os.environ.get("TYPESAFE_NAMES_FILE", os.path.expanduser("~/.config/typesafe/names.txt"))
if not os.path.exists(names_file):
    os.makedirs(os.path.dirname(names_file), exist_ok=True)
    with open(names_file, "w", encoding="utf-8") as f:
        f.write("# TypeSafe known names for local masking\nMax Mustermann\nErika Musterfrau\n")

CASES = {  # text -> secret that must be gone (None = nothing may be masked)
    "api_key=SECRETVALUE99 mail a@b.de": "SECRETVALUE99",
    "password=hunter2 end": "hunter2",
    "export API_KEY=abcd1234 x": "abcd1234",
    "Authorization: Bearer sk-abc123 def": "sk-abc123",
    '{"token": "t0k3n", "x": 1}': "t0k3n",
    "{'secret':'s3cr3t'}": "s3cr3t",
    "db_password: geheim123;": "geheim123",
    "mail an max.muster@example.org": "max.muster@example.org",
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

