"""Shared helpers for the typesafe-dev CLIs (stdlib only).

The API key is never printed, logged or written anywhere.
"""
import json
import os
import re
import subprocess
import urllib.error
import urllib.request

URL = "https://api.typesafe.ai/v1/systemone"
MODEL = "jev-latest"
TIMEOUT = 8


def get_key():
    """TYPESAFE_API_KEY, else `secret-tool lookup service typesafe key api`; None if absent."""
    key = os.environ.get("TYPESAFE_API_KEY", "").strip()
    if key:
        return key
    try:
        r = subprocess.run(["secret-tool", "lookup", "service", "typesafe", "key", "api"],
                           capture_output=True, text=True, timeout=5)
        if r.returncode == 0 and r.stdout.strip():
            return r.stdout.strip()
    except Exception:
        pass
    return None


class _NoRedirect(urllib.request.HTTPRedirectHandler):
    """Never follow redirects: a 3xx becomes an HTTPError, so post() fails open
    instead of re-sending the request (and its Authorization header) elsewhere."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


_OPENER = urllib.request.build_opener(_NoRedirect)


def _mask_tree(obj):
    """Mask every string leaf of state/questions; dict KEYS stay as they are
    (question IDs and choice keys are the answer contract)."""
    if isinstance(obj, str):
        return mask_detail(obj)[0]
    if isinstance(obj, dict):
        return {k: _mask_tree(v) for k, v in obj.items()}
    if isinstance(obj, (list, tuple)):
        return [_mask_tree(v) for v in obj]
    return obj


def post(state, questions, timeout=TIMEOUT):
    """One batched request. Returns the `answers` dict, or None on ANY failure.

    Second line of defence: every string leaf of state and questions is masked here
    again, in addition to the masking each tool does itself."""
    try:
        key = get_key()
        if not key:
            return None
        body = json.dumps({"state": _mask_tree(state), "model": MODEL,
                           "questions": _mask_tree(questions)}).encode()
        req = urllib.request.Request(URL, data=body, method="POST", headers={
            "Content-Type": "application/json",
            "User-Agent": "typesafe-dev/0.1"})
        req.add_unredirected_header("Authorization", "Bearer " + key)
        with _OPENER.open(req, timeout=timeout) as resp:
            if resp.status != 200:
                return None
            answers = json.loads(resp.read().decode()).get("answers")
            return answers if isinstance(answers, dict) else None
    except Exception as e:
        if isinstance(e, urllib.error.HTTPError):  # e.g. an unfollowed 3xx: release the response
            e.close()
        return None


# Masking before anything leaves the machine (user decision 2026-09-29: only masked
# data goes to TypeSafe). Pattern filter, not a proof: unusual secrets and names slip through.
DEFAULT_CATEGORIES = ("secret_kw", "email", "address", "name", "opaque")

_STREET_SUFFIXES = r"(?:stra[ßs]e|str\b\.?|weg|gasse|platz|allee|ring|damm|ufer|chaussee|zeile|stieg|gässchen|pfad|markt)"
_PAT_STREET = (
    r"\b(?:"
    r"(?:[A-ZÄÖÜ][a-zäöüß]+(?:\s+|-))*"
    r"(?:[A-ZÄÖÜ][a-zäöüß]+)?(?i:" + _STREET_SUFFIXES + r")"
    r"|"
    r"[a-zäöüß]+(?i:" + _STREET_SUFFIXES + r")"
    r")"
    r"\s+\d+(?:\s*[a-zA-Z])?(?:\s*[-/]\s*\d+(?:\s*[a-zA-Z])?)?\b"
)
_PAT_PLZ = (
    r"\b\d{5}\s+[A-ZÄÖÜ][a-zäöüß]+(?:[-/][A-ZÄÖÜ][a-zäöüß]+)*(?:\s+(?:(?:am|an\s+der|im)\s+)?[A-ZÄÖÜ][a-zäöüß]+)?\b"
)
RX_ADDRESS = re.compile(rf"{_PAT_STREET}|{_PAT_PLZ}")
RX_BEARER = re.compile(r"(?i)(\b(?:bearer|basic)\s+)\S+")
RX_KEY_VAL = re.compile(
    r"(?i)((?:api[_-]?key|token|secret|passw(?:or)?d|authorization)[\w.-]*[\"']?\s*[=:]\s*[\"']?)(?!<redacted>)[^\s\"',;]+"
)
RX_EMAIL = re.compile(r"[\w.+-]+@[\w-]+\.[\w.-]+")
RX_OPAQUE = re.compile(r"(?=[A-Za-z0-9_\-+/]*\d)(?=[A-Za-z0-9_\-+/]*[A-Za-z])[A-Za-z0-9_\-+/]{24,}={0,2}")

NAMES_FILE = os.environ.get("TYPESAFE_NAMES_FILE", os.path.expanduser("~/.config/typesafe/names.txt"))
_NAMES_CACHE = {}  # filepath -> (mtime, regex)


def load_names(filepath=None):
    """Load name terms from local file outside Git (split full names into first/last names)."""
    if filepath is None:
        filepath = os.environ.get("TYPESAFE_NAMES_FILE", os.path.expanduser("~/.config/typesafe/names.txt"))
    if not os.path.isfile(filepath):
        return []
    terms = set()
    try:
        with open(filepath, "r", encoding="utf-8") as f:
            for raw_line in f:
                line = raw_line.split("#")[0].strip()
                if len(line) >= 2:
                    terms.add(line)
                    for part in line.split():
                        part = part.strip()
                        if len(part) >= 2:
                            terms.add(part)
    except Exception:
        return []
    return sorted(terms, key=lambda s: (len(s), s), reverse=True)


def _update_mask_res(name_rx=None):
    global MASK_RES
    base = [
        ("secret_kw", RX_BEARER, r"\1<redacted>"),
        ("secret_kw", RX_KEY_VAL, r"\1<redacted>"),
        ("email", RX_EMAIL, "<email>"),
        ("address", RX_ADDRESS, "<address>"),
    ]
    if name_rx is not None:
        base.append(("name", name_rx, "<name>"))
    base.append(("opaque", RX_OPAQUE, "<redacted>"))
    MASK_RES = base


def get_name_regex(filepath=None):
    """Compiled regex for configured names, or None if no names available."""
    if filepath is None:
        filepath = os.environ.get("TYPESAFE_NAMES_FILE", os.path.expanduser("~/.config/typesafe/names.txt"))
    try:
        mtime = os.path.getmtime(filepath)
    except OSError:
        _update_mask_res(None)
        return None
    cached = _NAMES_CACHE.get(filepath)
    if cached and cached[0] == mtime:
        # Bug fix: MASK_RES is a single global; a cache hit must still refresh it,
        # otherwise a process that reads mask_detail() under different
        # TYPESAFE_NAMES_FILE values (e.g. tests) can mask against a stale file's names.
        _update_mask_res(cached[1])
        return cached[1]
    terms = load_names(filepath)
    if not terms:
        rx = None
    else:
        pattern = r"(?i)\b(?:" + "|".join(re.escape(t) for t in terms) + r")\b"
        rx = re.compile(pattern)
    _NAMES_CACHE[filepath] = (mtime, rx)
    _update_mask_res(rx)
    return rx


MASK_RES = []
_update_mask_res(get_name_regex())


def mask_detail(text):
    """(masked text, {"secret_kw": n, "email": n, "address": n, "name": n, "opaque": n}); patterns run in this order."""
    get_name_regex()
    counts = {cat: 0 for cat in DEFAULT_CATEGORIES}
    for cat, rx, repl in MASK_RES:
        text, n = rx.subn(repl, text)
        counts[cat] += n
    return text, counts


def mask(text):
    """(masked text, number of masked spans) - backward compatible."""
    text, counts = mask_detail(text)
    return text, sum(counts.values())


CATEGORY_LABELS = ("Schlüsselwort", "E-Mail", "Adresse", "Name", "Token")  # same order as DEFAULT_CATEGORIES


def format_counts(counts):
    """Display line for mask counts, one entry per category in DEFAULT_CATEGORIES order."""
    return ", ".join(f"{label} {counts.get(cat, 0)}"
                     for cat, label in zip(DEFAULT_CATEGORIES, CATEGORY_LABELS))


def add_counts(*dicts):
    """Sum several mask_detail count dicts."""
    out = {cat: 0 for cat in DEFAULT_CATEGORIES}
    for d in dicts:
        for k, v in d.items():
            out[k] = out.get(k, 0) + v
    return out


def noul(answers, qid):
    """Probability 0..1 of a noul answer, or None."""
    try:
        return float(answers[qid]["noul"])
    except Exception:
        return None


def score_level(answers, qid):
    """(argmax level int, confidence) of a score answer, or None."""
    try:
        probs = {int(k): float(v) for k, v in answers[qid]["probabilities"].items()}
        lvl = max(probs, key=probs.get)
        return lvl, float(answers[qid]["confidence"])
    except Exception:
        return None


def choice(answers, qid):
    """(choice string, confidence float, probabilities dict) or None."""
    try:
        a = answers[qid]
        probs = {k: float(v) for k, v in a.get("probabilities", {}).items()}
        return a["choice"], float(a.get("confidence", 0.0)), probs
    except Exception:
        return None
