#!/usr/bin/env python3
"""Acceptance measurements for bin/ts-done-check's supervisor design, with the
production budgets (8 s deadline, 2 s keyring cap) unless a criterion says otherwise.

Not a unittest module (the hyphenated name keeps `unittest discover` from loading it):
it runs for several minutes. Every run goes through tests/done_check_harness.py
(no TYPESAFE_API_KEY, fake secret-tools, log/HOME/TMPDIR in a temp dir, URL shim);
master's copy is extracted from git into the same temp dir and run with HOME there,
so its hard-coded ~/.local/state log lands in the temp dir too. Run it without
network access as an extra tripwire, e.g.
    unshare -rn sh -c 'ip link set lo up && exec python3 tests/done-check-acceptance.py'
Criterion 1's DNS case starts its own `unshare -rnm` (mount + network namespace).

    python3 tests/done-check-acceptance.py [c1 c2 c3 c4 c5 c6] [--sweep N] [--bodies N]
Exit status 0 iff every selected criterion passed.
"""
import argparse
import json
import os
import random
import signal
import statistics
import subprocess
import sys
import tempfile
import threading
import time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
import done_check_harness as h  # noqa: E402

LIMIT_S = 8.2
CLAIM = "Fertig, erledigt. Tests sind grün."
RESULTS = []


def report(criterion, name, ok, detail):
    RESULTS.append((criterion, name, ok))
    print(f"[{criterion}] {'PASS' if ok else 'FAIL'} {name}: {detail}", flush=True)


def one_error_line(sb, r, elapsed, info, stage, log_path=None, limit=LIMIT_S):
    path = log_path or sb.log_path
    lines = []
    if os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            lines = [json.loads(l) for l in f if l.strip()]
    left = h.leftovers(info)
    got_stage = lines[0].get("stage") if len(lines) == 1 else None
    ok = (r.returncode == 0 and r.stdout == "" and len(lines) == 1
          and lines[0].get("result") == "error" and got_stage == stage
          and elapsed < limit and not left)
    return ok, (f"rc={r.returncode} lines={len(lines)} stage={got_stage} "
                f"elapsed={elapsed:.3f}s leftovers={left or 'none'}")


# ---- 1: hanging / failing worker, production budgets ----

def c1(sb):
    tp = sb.write_transcript("c1.jsonl", [("Edit", None, False)])
    never = h.SlowServer(delay=None)
    trickle = h.TrickleServer(interval=0.5, n_bytes=40)  # ~20 s uncut
    cases = [
        ("POST server never answers", dict(keyring="key", url=never.url), CLAIM, "post"),
        ("tc.post blocks 60 s", dict(keyring="key", mode="hang_post"), CLAIM, "post"),
        ("trickling POST", dict(keyring="key", url=trickle.url), CLAIM, "post"),
        ("hanging keyring (sleep 30)", dict(keyring="sleep:30"), CLAIM, "keyring"),
        ("slow keyring 1.5 s, then hanging POST", dict(keyring="slowkey:1.5", url=never.url),
         CLAIM, "post"),
        ('quadratic RX_ADDRESS "Weg " x 12500 (50k chars)', {}, "Fertig. " + "Weg " * 12500,
         "startup"),
        ('quadratic RX_ADDRESS "Abc " x 12500 (50k chars)', {}, "Fertig. " + "Abc " * 12500,
         "startup"),
        ("worker crashes in the keyring step", dict(keyring="key", mode="crash_at_keyring"),
         CLAIM, "keyring"),
        ("worker prints garbage, then hangs", dict(mode="garbage"), CLAIM, "startup"),
        ("worker exits without a final record", dict(mode="exit_silently"), CLAIM, "startup"),
    ]
    for i, (name, kw, msg, stage) in enumerate(cases):
        log = os.path.join(sb.dir, f"c1-{i}.jsonl")
        r, elapsed, info = sb.run(sb.stdin(msg, transcript=tp), env={"TS_DONE_CHECK_LOG": log}, **kw)
        ok, detail = one_error_line(sb, r, elapsed, info, stage, log)
        report("1", name, ok, detail)
    never.close()
    trickle.close()
    c1_dns()


DNS_INNER = r'''
import json, os, socket, subprocess, sys, threading, time
sys.path.insert(0, {here!r})
import done_check_harness as h
s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
s.bind(("127.0.0.1", 53))
seen = []
def rx():
    while True:
        seen.append(s.recvfrom(4096)[0])
threading.Thread(target=rx, daemon=True).start()
out = {{}}
t = time.monotonic()
try:
    subprocess.run([sys.executable, "-c", "import socket; socket.getaddrinfo('dns-hang.example.test', 80)"],
                   timeout=10, capture_output=True)
    out["bare_getaddrinfo"] = "returned after %.2fs" % (time.monotonic() - t)
except subprocess.TimeoutExpired:
    out["bare_getaddrinfo"] = "still blocked after %.2fs" % (time.monotonic() - t)
sb = h.Sandbox()
tp = sb.write_transcript("dns.jsonl", [("Edit", None, False)])
r, elapsed, info = sb.run(sb.stdin("Fertig, erledigt.", transcript=tp), keyring="key",
                          url="http://dns-hang.example.test/v1/systemone")
out.update(rc=r.returncode, stdout=r.stdout, elapsed=elapsed, lines=sb.log_lines(),
           leftovers=h.leftovers(info), queries=len(seen))
sb.cleanup()
print(json.dumps(out))
'''


def c1_dns():
    with tempfile.TemporaryDirectory(prefix="typesafe-dns-") as d:
        with open(os.path.join(d, "resolv.conf"), "w") as f:
            f.write("nameserver 127.0.0.1\noptions timeout:10 attempts:3\n")
        with open(os.path.join(d, "nsswitch.conf"), "w") as f:
            f.write("passwd: files\ngroup: files\nhosts: files dns\n")
        with open(os.path.join(d, "inner.py"), "w") as f:
            f.write(DNS_INNER.format(here=HERE))
        script = (f"ip link set lo up && mount --bind {d}/resolv.conf /etc/resolv.conf && "
                  f"mount --bind {d}/nsswitch.conf /etc/nsswitch.conf && "
                  f"exec {sys.executable} {d}/inner.py")
        env = {k: v for k, v in os.environ.items() if k != "TYPESAFE_API_KEY"}
        try:
            p = subprocess.run(["unshare", "-rnm", "sh", "-c", script], capture_output=True,
                               text=True, timeout=60, env=env)
            out = json.loads(p.stdout.strip().splitlines()[-1])
        except Exception as e:
            report("1", "silent DNS (unshare -rnm)", False, f"could not run: {e!r}")
            return
    lines = out["lines"]
    stage = lines[0].get("stage") if len(lines) == 1 else None
    ok = (out["rc"] == 0 and out["stdout"] == "" and len(lines) == 1 and stage == "post"
          and out["elapsed"] < LIMIT_S and not out["leftovers"]
          and out["bare_getaddrinfo"].startswith("still blocked"))
    report("1", "silent DNS (unshare -rnm, files dns, silent 127.0.0.1:53)", ok,
           f"bare getaddrinfo {out['bare_getaddrinfo']}; hook rc={out['rc']} lines={len(lines)} "
           f"stage={stage} elapsed={out['elapsed']:.3f}s queries={out['queries']} "
           f"leftovers={out['leftovers'] or 'none'}")


# ---- 2: stopped lock holder ----

def c2(sb):
    tp = sb.write_transcript("c2.jsonl", [("Edit", None, False)])
    fast = h.SlowServer(delay=0, body=h.answer_body(0.95, 0.05))
    never = h.SlowServer(delay=None)
    for name, kw, want in (("fast block decision", dict(keyring="key", url=fast.url), "block"),
                           ("hanging POST (deadline)", dict(keyring="key", url=never.url), "error")):
        log = os.path.join(sb.dir, f"c2-{want}.jsonl")
        holder = subprocess.Popen(
            [sys.executable, "-c",
             "import fcntl, os, signal, sys\n"
             "fd = os.open(sys.argv[1], os.O_CREAT | os.O_RDWR, 0o600)\n"
             "fcntl.flock(fd, fcntl.LOCK_EX)\n"
             "sys.stdout.write('LOCKED\\n'); sys.stdout.flush()\n"
             "os.kill(os.getpid(), signal.SIGSTOP)\n",
             log + ".lock"], stdout=subprocess.PIPE, text=True)
        holder.stdout.readline()
        while True:
            with open(f"/proc/{holder.pid}/status") as f:
                if next(l for l in f if l.startswith("State:")).split()[1] == "T":
                    break
            time.sleep(0.01)
        try:
            r, elapsed, info = sb.run(sb.stdin(CLAIM, transcript=tp), env={"TS_DONE_CHECK_LOG": log}, **kw)
        finally:
            os.kill(holder.pid, signal.SIGCONT)
            holder.wait(timeout=5)
            holder.stdout.close()
        with open(log, encoding="utf-8") as f:
            lines = [json.loads(l) for l in f if l.strip()]
        got = (lines[0].get("decision") or lines[0].get("result")) if len(lines) == 1 else None
        printed = r.stdout == h.BLOCK_STDOUT
        ok = (r.returncode == 0 and len(lines) == 1 and got == want and printed == (want == "block")
              and elapsed < LIMIT_S and not h.leftovers(info))
        report("2", f"stopped lock holder, {name}", ok,
               f"lines={len(lines)} line={got} printed_block={printed} elapsed={elapsed:.3f}s")
    fast.close()
    never.close()


# ---- 3: stdout and log never disagree around the deadline ----

def c3(sb, n, parallel, spread_ms):
    tp = sb.write_transcript("c3.jsonl", [("Edit", None, False)])
    srv = h.SlowServer(delay=0, body=h.answer_body(0.95, 0.05))
    rng = random.Random(3)
    deltas = [rng.uniform(-spread_ms, spread_ms) for _ in range(n)]
    results = [None] * n

    def one(i):
        log = os.path.join(sb.dir, f"c3-{i}.jsonl")
        r, elapsed, info = sb.run(sb.stdin(CLAIM, session_id=f"sweep-{i}", transcript=tp),
                                  keyring="key", url=srv.url, mode="final_at",
                                  env={"TS_DONE_CHECK_LOG": log,
                                       "TS_TEST_FINAL_DELTA_MS": f"{deltas[i]:.3f}"})
        with open(log, encoding="utf-8") as f:
            lines = [json.loads(l) for l in f if l.strip()]
        results[i] = (r.returncode, r.stdout, lines, elapsed)

    for b in range(0, n, parallel):
        ts = [threading.Thread(target=one, args=(i,)) for i in range(b, min(n, b + parallel))]
        for t in ts:
            t.start()
        for t in ts:
            t.join()
    srv.close()
    bad, blocks, errors, slow = [], 0, 0, 0
    split = {"before": [0, 0], "after": [0, 0]}  # [block, error] by the side the record was held to
    for i, (rc, out, lines, elapsed) in enumerate(results):
        printed = out == h.BLOCK_STDOUT
        split["before" if deltas[i] < 0 else "after"][0 if printed else 1] += 1
        consistent = (rc == 0 and out in ("", h.BLOCK_STDOUT) and len(lines) == 1
                      and printed == (lines[0].get("decision") == "block")
                      and (printed or lines[0].get("result") == "error"))
        if not consistent:
            bad.append((i, deltas[i], rc, out, lines))
        blocks += printed
        errors += (not printed)
        slow += elapsed >= LIMIT_S
    ok = not bad and blocks > 0 and errors > 0 and slow == 0
    report("3", f"{n} runs, final record at deadline +/- {spread_ms:.0f} ms (uniform), "
           f"{parallel} in parallel", ok,
           f"inconsistent={len(bad)} block+printed={blocks} error+silent={errors} "
           f"(held to before the deadline: {split['before'][0]} block/{split['before'][1]} error; "
           f"after: {split['after'][0]} block/{split['after'][1]} error) "
           f">={LIMIT_S}s={slow}" + (f" first bad: {bad[0]}" if bad else ""))


# ---- master copy for 4 and 5 ----

def master_copy(sb):
    root = os.path.join(sb.dir, "master")
    os.makedirs(os.path.join(root, "bin"), exist_ok=True)
    for path in ("ts_common.py", "bin/ts-done-check"):
        data = subprocess.run(["git", "-C", h.ROOT, "show", f"master:{path}"], check=True,
                              capture_output=True).stdout
        with open(os.path.join(root, path), "wb") as f:
            f.write(data)
    return root


def run_master(sb, mroot, stdin, keyring="fail", url=None):
    if keyring != "fail" and not (url or "").startswith("http://127.0.0.1:"):
        raise RuntimeError("refusing: a key for master's copy needs a local URL")
    e = dict(sb.base_env)
    e["PATH"] = sb.fakebin(keyring) + os.pathsep + sb.base_env["PATH"]
    assert "TYPESAFE_API_KEY" not in e and e["HOME"].startswith(sb.dir)
    mbin = os.path.join(mroot, "bin", "ts-done-check")
    if url:
        cmd = [sys.executable, "-c", f"import sys, runpy; sys.path.insert(0, {mroot!r}); "
               f"import ts_common as tc; tc.URL = {url!r}; runpy.run_path({mbin!r}, run_name='__main__')"]
    else:
        cmd = [sys.executable, mbin]
    start = time.monotonic()
    r = subprocess.run(cmd, input=stdin, capture_output=True, text=True, env=e, timeout=60)
    return r, time.monotonic() - start


# ---- 4: fast-path cost ----

def c4(sb, n=50):
    mroot = master_copy(sb)
    tp = sb.write_transcript("c4.jsonl", [("Edit", None, False), ("Bash", "pytest -q", False)])
    srv = h.SlowServer(delay=0, body=h.answer_body(0.2, 0.9))
    noclaim = sb.stdin("Ich habe die Datei gelesen, was als Nächstes?", transcript=tp)
    claim = sb.stdin(CLAIM, transcript=tp)
    t = {"bare": [], "m_noclaim": [], "n_noclaim": [], "m_claim": [], "n_claim": []}
    for _ in range(n):
        s0 = time.monotonic()
        subprocess.run([sys.executable, "-c", "pass"])
        t["bare"].append(time.monotonic() - s0)
        t["m_noclaim"].append(run_master(sb, mroot, noclaim)[1])
        t["n_noclaim"].append(sb.run(noclaim, shim=False)[1])
        t["m_claim"].append(run_master(sb, mroot, claim, keyring="key", url=srv.url)[1])
        t["n_claim"].append(sb.run(claim, keyring="key", url=srv.url)[1])
    srv.close()
    p50 = {k: statistics.median(v) * 1000 for k, v in t.items()}
    unit = p50["bare"]
    for path in ("noclaim", "claim"):
        extra = p50[f"n_{path}"] - p50[f"m_{path}"]
        report("4", f"p50 over {n} runs, {path} path", extra <= unit,
               f"master {p50[f'm_{path}']:.1f} ms, new {p50[f'n_{path}']:.1f} ms, "
               f"extra {extra:+.1f} ms = {extra / unit:+.2f} x python3 -c pass ({unit:.1f} ms)")


# ---- 5: request bodies byte-identical to master ----

V = "wach" + "Wert" + "Echo" + "4711"
AT = "@"  # address-shaped fixtures are put together at runtime, as in test_no_leak.py
_CHAIN = "-".join(["Wachtelberg", "Oberhausener", "Unterbacher", "Mittelfelder", "Kaiserin",
                   "Friedrichs", "Wilhelminen", "Viktorias", "Luisen", "Augusta"])
_CITY = "-".join(["Wachtstadt", "Oberdorf", "Unterdorf", "Mitteldorf", "Hinterdorf",
                  "Vorderdorf", "Wachtneudorf"])
FRAGMENTS = [
    "api" + "_key=" + V, "pass" + "word:" + " " * 200 + V, "tok" + "en" + "a1b2c3d4" * 20 + "=" + V,
    "pass" + 'word = "mein geheimes Passwort 2024"', "DB_PA" + "SS=Hunter" + "2024Secret!",
    "Authorization: Bearer " + "eyJ" + "hbGciOiJIUzI1NiJ9" + "abcdef123456",
    "AKIA" + "Q7X2M4P9R3T6W8Y1", "ghp_" + "4f9a8b7c6d5e4f3a2b1c0d9e8f7a6b5c4d3e",
    "Zq7xK9pL2mN4vB8cR1tY6wE3", "a" * 24 + "３", "b" * 30 + "=", "wache.sentinel" + AT + "mail.example",
    "x.y+z" + AT + "sub.domain.example", "Max Mustermann", "Erika Musterfrau", "Wanda", "Hauptstraße 12",
    "Am Markt 1", "Musterstraße 12-14", _CHAIN + "straße 12", "80331 " + _CITY, "80331 " + "München",
    "10115 " + "Berlin-Mitte", " ".join(["DE89", "3704", "0044", "0532", "0130", "00"]),
    "+49 221 12345678", "y" * 300,
    "grün ✅ 🎉 naïve café", "quoted \"double\" and 'single'", "back\\slash\\path", "tab\tsep",
    "new\nline", "pytest -q tests/", "git commit -m \"fix token refresh\"",
]
CLAIMS = ["Fertig.", "Alles erledigt", "done", "fixed it", "✅", "Tests sind grün", "works now",
          "verified", "läuft", "behoben"]
TOOLS = ["Edit", "Write", "MultiEdit", "NotebookEdit", "Bash", "Bash", "Bash", "Read", "Grep"]


def make_case(rng, i, sb):
    parts = [rng.choice(CLAIMS)] if i % 25 != 7 else ["Ich habe nur gelesen."]
    for _ in range(rng.randint(0, 6)):
        parts.append(rng.choice(FRAGMENTS + ["und dann", "weil", "Text " * rng.randint(1, 20)]))
    if i % 9 == 0:  # longer than MAX_MSG: the tail cut must land the same way
        parts.insert(1, "Füllung " * rng.randint(500, 700))
    rng.shuffle(parts[1:])
    msg = " ".join(parts)
    calls = []
    for _ in range(rng.choice([0, 1, 3, 8, 15, 16, 25, 40])):
        tool = rng.choice(TOOLS)
        cmd = " ".join(rng.choice(FRAGMENTS) for _ in range(rng.randint(1, 3))) if tool == "Bash" else None
        calls.append((tool, cmd, rng.choice([False, False, True, None])))
    tp = sb.write_transcript(f"c5-{i}.jsonl", calls)
    if i % 5 == 3:  # message only in the transcript: last_assistant_text()
        with open(tp, "a", encoding="utf-8") as f:
            f.write(json.dumps({"type": "assistant", "message": {"content": [
                {"type": "text", "text": msg}, {"type": "text", "text": rng.choice(FRAGMENTS)}]}}) + "\n")
        return json.dumps({"session_id": f"c5-{i}", "transcript_path": tp})
    return sb.stdin(msg, session_id=f"c5-{i}", transcript=tp)


def c5(sb, n):
    mroot = master_copy(sb)
    srv = h.SlowServer(delay=0, body=h.answer_body(0.2, 0.9))
    rng = random.Random(5)
    diffs, posted, silent = [], 0, 0
    for i in range(n):
        stdin = make_case(rng, i, sb)
        k = len(srv.requests)
        run_master(sb, mroot, stdin, keyring="key", url=srv.url)
        m = srv.requests[k:]
        k = len(srv.requests)
        sb.run(stdin, keyring="key", url=srv.url, env={"TS_DONE_CHECK_LOG": os.path.join(sb.dir, "c5.jsonl")})
        new = srv.requests[k:]
        if m != new:
            diffs.append(i)
        posted += bool(m)
        silent += not m
    srv.close()
    report("5", f"{n} varied cases, request bodies master vs new", not diffs and posted > n * 0.8,
           f"differing={len(diffs)} {diffs[:10]} with_request={posted} without_request={silent}")


# ---- 6: concurrent writers across the 20 MB rotation ----

def c6(sb, trials=5, n=24):
    tp = sb.write_transcript("c6.jsonl", [("Edit", None, False)])
    srv = h.SlowServer(delay=0, body=h.answer_body(0.95, 0.05))
    max_bytes = 20 * 1024 * 1024
    for trial in range(trials):
        log = os.path.join(sb.dir, f"c6-{trial}.jsonl")
        pad = json.dumps({"prefill": "x" * 1000}) + "\n"
        with open(log, "w", encoding="utf-8") as f:
            f.write(pad * ((max_bytes - 3000) // len(pad)))
        results = [None] * n

        def one(i):
            results[i] = sb.run(sb.stdin(CLAIM, session_id=f"c6-{trial}-{i}", transcript=tp),
                                keyring="key", url=srv.url, env={"TS_DONE_CHECK_LOG": log})

        ts = [threading.Thread(target=one, args=(i,)) for i in range(n)]
        for t in ts:
            t.start()
        for t in ts:
            t.join()
        seen = []
        for path in (log, log + ".1"):
            if os.path.exists(path):
                with open(path, encoding="utf-8") as f:
                    seen += [json.loads(l).get("session_id") for l in f if l.strip()]
        ours = sorted(s for s in seen if s and s.startswith(f"c6-{trial}-"))
        rotated = os.path.exists(log + ".1") and os.path.getsize(log) < max_bytes
        ok = (len(ours) == n and len(set(ours)) == n and rotated
              and all(r[0].returncode == 0 for r in results))
        report("6", f"trial {trial + 1}: {n} parallel runs across the rotation", ok,
               f"lines={len(ours)} unique={len(set(ours))} rotated={rotated} "
               f"active_file_lines={sum(1 for _ in open(log))}")
    srv.close()


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("criteria", nargs="*", default=["c1", "c2", "c3", "c4", "c5", "c6"])
    ap.add_argument("--sweep", type=int, default=500)
    ap.add_argument("--parallel", type=int, default=50)
    ap.add_argument("--spread-ms", type=float, default=15.0)
    ap.add_argument("--bodies", type=int, default=150)
    a = ap.parse_args()
    sb = h.Sandbox(prefix="typesafe-acceptance-")
    try:
        for c in a.criteria:
            t0 = time.monotonic()
            {"c1": lambda: c1(sb), "c2": lambda: c2(sb),
             "c3": lambda: c3(sb, a.sweep, a.parallel, a.spread_ms),
             "c4": lambda: c4(sb), "c5": lambda: c5(sb, a.bodies), "c6": lambda: c6(sb)}[c]()
            print(f"  ({c} took {time.monotonic() - t0:.0f}s)", flush=True)
    finally:
        sb.cleanup()
    failed = [r for r in RESULTS if not r[2]]
    print(f"{len(RESULTS) - len(failed)}/{len(RESULTS)} passed")
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
