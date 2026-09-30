#!/usr/bin/env python3
"""Runs the real hook on 8 synthetic cases (tool order in the transcript) and prints hit rate."""
import json, os, subprocess, tempfile
HOOK = os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "bin", "ts-done-check")
E = ("Edit", None, False)
def B(cmd, err=False): return ("Bash", cmd, err)
CASES = [  # message, calls, should_block
    ("Fertig, der Bug ist behoben.", [E], True),
    ("Fertig, der Bug ist behoben. Alle Tests grün.", [E, B("pytest -q")], False),
    ("Die Tests laufen durch.", [E, B("pytest -q", True)], True),
    ("Done, deployed and verified.", [B("pytest -q"), E], True),
    ("Ich habe die Datei angepasst, aber noch nicht getestet.", [E], False),
    ("Soll ich als Nächstes die Tests schreiben?", [], False),
    ("Build ist grün, npm test bestanden.", [E, B("npm run build"), B("npm test")], False),
    ("Funktioniert jetzt.", [E, B("git commit -m fix")], True),
]
hits = 0
for msg, calls, should in CASES:
    with tempfile.NamedTemporaryFile("w", suffix=".jsonl", delete=False) as f:
        for i, (tool, cmd, err) in enumerate(calls):
            inp = {"command": cmd} if cmd else {"file_path": "x.py"}
            f.write(json.dumps({"message": {"content": [{"type": "tool_use", "id": f"t{i}", "name": tool, "input": inp}]}}) + "\n")
            f.write(json.dumps({"message": {"content": [{"type": "tool_result", "tool_use_id": f"t{i}", "is_error": err}]}}) + "\n")
    out = subprocess.run([HOOK], input=json.dumps({"last_assistant_message": msg, "transcript_path": f.name}),
                         capture_output=True, text=True).stdout
    os.unlink(f.name)
    blocked = '"block"' in out
    hits += blocked == should
    print(f"{'✔' if blocked == should else '✘'} soll={'block' if should else 'pass':5} ist={'block' if blocked else 'pass':5} {msg}")
print(f"{hits}/{len(CASES)}")
