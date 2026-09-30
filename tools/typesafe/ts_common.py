"""Shared helpers for the typesafe-dev CLIs (stdlib only).

The API key is never printed, logged or written anywhere.
"""
import collections
import json
import math
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
DEFAULT_CATEGORIES = ("secret_kw", "email", "address", "name", "opaque", "iban", "phone")

_STREET_SUFFIXES = r"(?:stra[ßs]e|str\b\.?|weg|gasse|platz|allee|ring|damm|ufer|chaussee|zeile|stieg|gässchen|pfad|markt)"
# The range end is capped at 4 digits so a following 5-digit postcode is never read as a
# range end (its PLZ+Ort then gets its own match).
_PAT_STREET = (
    r"\b(?:"
    r"(?:[A-ZÄÖÜ][a-zäöüß]+(?:\s+|-))*"
    r"(?:[A-ZÄÖÜ][a-zäöüß]+)?(?i:" + _STREET_SUFFIXES + r")"
    r"|"
    r"[a-zäöüß]+(?i:" + _STREET_SUFFIXES + r")"
    r")"
    r"\s+\d+(?:\s*[a-zA-Z])?(?:\s*[-/]\s*\d{1,4}(?:\s*[a-zA-Z])?)?\b"
)
_PAT_PLZ = (
    r"\b\d{5}\s+[A-ZÄÖÜ][a-zäöüß]+(?:[-/][A-ZÄÖÜ][a-zäöüß]+)*(?:\s+(?:(?:am|an\s+der|im)\s+)?[A-ZÄÖÜ][a-zäöüß]+)?\b"
)
RX_ADDRESS = re.compile(rf"{_PAT_STREET}|{_PAT_PLZ}")
RX_BEARER = re.compile(r"(?i)(\b(?:bearer|basic)\s+)\S+")
_MASK_KW = (r"(?:api[_-]?key|token|secret|passw(?:or)?d|pass(?:phrase|wort)?|pwd|credential"
            r"|private[_-]?key|access[_-]?key|auth(?:orization)?)")
# A quoted value is masked as a whole, whitespace included; an unquoted one up to the
# next whitespace/quote/comma/semicolon.
RX_KEY_VAL = re.compile(
    r"(?i)(" + _MASK_KW + r"[\w.-]*[\"']?\s*[=:]\s*)"
    r"(?:(?P<dq>\")(?!<redacted>\")[^\"\n]+\"|(?P<sq>')(?!<redacted>')[^'\n]+'|[\"']?(?!<redacted>)[^\s\"',;]+)"
)


def _key_val_repl(m):
    q = m.group("dq") or m.group("sq") or ""
    return m.group(1) + q + "<redacted>" + q


RX_EMAIL = re.compile(r"[\w.+-]+@[\w-]+\.[\w.-]+")
RX_IBAN = re.compile(r"(?<![A-Za-z0-9])[A-Z]{2}\d{2}(?: ?[A-Z0-9]){11,30}(?![A-Za-z0-9])")
# German numbers: +49 or a leading 0, then at least 8 more digits, separators allowed.
RX_PHONE = re.compile(r"(?<![\w+.])(?:\+49|0)(?:[ \t./()-]*\d){8,}(?!\d)")
# Known token prefixes (GitHub, OpenAI/Stripe, Slack, AWS, Google, GitLab, npm).
KNOWN_PREFIX = (r"(?:ghp_|gho_|ghs_|github_pat_|sk-|sk_live_|sk_test_|rk_live_|rk_test_|pk_live_"
                r"|xox[abprs]-|AKIA|ASIA|AIza|GOCSPX-|ya29\.|1//|eyJ|glpat-|npm_)")
_TOKEN_CHARS = r"[A-Za-z0-9_\-./+=]"
RX_KNOWN_TOKEN = re.compile(r"(?<![A-Za-z0-9])" + KNOWN_PREFIX + _TOKEN_CHARS + r"{8,}")
RX_AWS_KEY_ID = re.compile(r"(?<![A-Za-z0-9])(?:AKIA|ASIA)[0-9A-Z]{16}(?![A-Za-z0-9])")
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
        ("secret_kw", RX_KEY_VAL, _key_val_repl),
        ("email", RX_EMAIL, "<email>"),
        ("iban", RX_IBAN, "<iban>"),
        ("phone", RX_PHONE, "<phone>"),
        ("address", RX_ADDRESS, "<address>"),
    ]
    if name_rx is not None:
        base.append(("name", name_rx, "<name>"))
    base.append(("opaque", RX_AWS_KEY_ID, "<redacted>"))
    base.append(("opaque", RX_KNOWN_TOKEN, "<redacted>"))
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
    """(masked text, {category: n} for DEFAULT_CATEGORIES); patterns run in MASK_RES order."""
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


CATEGORY_LABELS = ("Schlüsselwort", "E-Mail", "Adresse", "Name", "Token", "IBAN", "Telefon")  # same order as DEFAULT_CATEGORIES


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


# --- Local leak alarm (ts-commit-check, ts-pr-triage) --------------------------------
# Independent of the masking above: the masking toward TypeSafe stays generous, this
# decides whether a change is BLOCKED locally, so it only fires on values that really
# look like a secret or a personal address. Rules, in short:
#  - an email address, unless no-reply (NOREPLY_RE), on a reserved domain (RFC
#    2606/6761: example.com/.org/.net, *.example, *.test, *.invalid, *.localhost), one
#    of the clone's declared identities (allowed_addresses: user.email and
#    imprint.allowedIdentity, the list .githooks/pre-push reads), or no address at
#    all: systemd instance units (name@inst.service/.timer/...) and Google calendar
#    IDs (...@group.calendar.google.com, ...@import.calendar.google.com); one-letter
#    fixtures like a@b.com count as synthetic;
#  - a known token prefix at a segment start with >= 8 token characters behind it
#    (at least one digit among them), with or without a keyword; AWS key IDs; both
#    not when the part after the prefix is a fixture ("sk-fake-...", "AKIA...EXAMPLE");
#  - after a keyword (api_key, token, secret, credential, private_key, access_key,
#    authorization) and = := or :, a quoted literal that starts with a known prefix,
#    or one of >= 12 characters without whitespace, with a digit or mixed case and a
#    Shannon entropy >= min(3.0, 2.2 + 0.025 * (length - 12)) bits per character;
#    an unquoted value of >= 20 characters with letters and >= 4 digits;
#  - after a password keyword (pass, pwd, passwd, password, passwort, passphrase, as a
#    word of the key name, not label/hint/placeholder keys): any value of >= 8
#    characters with a letter and a digit or special character (unquoted: no dot);
#    fixture markers do NOT apply here, only references like ${...} or <...>;
#    "PASS:" in capitals is a test status, not a password key;
#  - Bearer/Basic with a literal of >= 16 token characters.
# Outside password keys, never an alarm without a known prefix: placeholders (<...>, ${...}, os.Getenv,
# REPLACE_ME, CHANGEME) and values with a fixture segment (a word that starts with
# synth, fake, dummy, beispiel, example, test, platzhalter, changeme, gueltig,
# ungueltig, xxx, geheim, schluessel/schlüssel, page or expired; "Contest" is no
# fixture segment). Outside password keys also never: word values, i.e. two or more
# parts split at - _ . / : + = that are all letters or digit runs of at most 3
# ("page-token-3", "expired_tok_1"). No hit for comparisons (== !=),
# identifiers and expressions (s.Weiter, "Bearer " + tok) and empty strings.
NOREPLY_RE = re.compile(
    r"(?i)(?<![\w.+-])(?:noreply@anthropic\.com|noreply@google\.com|noreply@github\.com|noreply@openai\.com"
    r"|antigravity@google\.com"
    r"|[\w.+-]+@users\.noreply\.github\.com)(?!\.?[\w-])")
RESERVED_DOMAIN_RE = re.compile(
    r"(?i)^(?:[\w-]+\.)*(?:example\.(?:com|org|net)|[\w-]+\.(?:example|test|invalid|localhost))$")
# name@instance.<unit type> is a systemd unit, a calendar ID is a resource: no address.
NOT_ADDRESS_DOMAIN_RE = re.compile(
    r"(?i)^(?:[\w-]+\.)+(?:service|timer|socket|path|target|mount|automount|slice|scope|swap|device)$"
    r"|^(?:group|import)\.calendar\.google\.com$")
_PREFIX_AT_START_RE = re.compile(KNOWN_PREFIX)
_PREFIX_AT_SEGMENT_RE = re.compile(r"(?<=[-_./:=+])" + KNOWN_PREFIX + r"(?P<body>" + _TOKEN_CHARS + r"{8,})")
_FREE_PREFIX_RE = re.compile(r"(?<![A-Za-z0-9])" + KNOWN_PREFIX + r"(?P<body>" + _TOKEN_CHARS + r"{8,})")
_PW_KW = r"(?<![A-Za-z])(?:pass(?:word|wort|wd|phrase)?|pwd)(?![a-z])"
_OTHER_KW = r"(?:api[_-]?key|token|secret|credential|private[_-]?key|access[_-]?key|authorization)"
LOCAL_KV_RE = re.compile(
    r"(?i)(?P<key>(?:(?P<pw>" + _PW_KW + r")|" + _OTHER_KW + r")[\w.-]*)[\"']?[ \t]*(?P<sep>:=|=(?!=)|:(?!=))[ \t]*"
    r"(?:\"(?P<dqv>[^\"\n]*)\"|'(?P<sqv>[^'\n]*)'|(?P<uv>[A-Za-z0-9_\-./+=]+))")
_LABEL_KEY_RE = re.compile(r"(?i)label|hint|placeholder|prompt|text|title|message|msg|field|error|input")
LOCAL_BEARER_RE = re.compile(r"(?i)\b(?:bearer|basic)[ \t]+(?P<v>[A-Za-z0-9_\-./+=]{16,})")
_PLACEHOLDER_RE = re.compile(r"(?i)^<[^>]*>$|\$\{|os\.getenv")
_FIXTURE_MARKERS = ("synth", "fake", "dummy", "beispiel", "example", "test", "platzhalter",
                    "changeme", "gueltig", "ungueltig", "xxx", "geheim", "schluessel",
                    "schlüssel", "page", "expired")
_SEGMENT_RE = re.compile(r"[A-ZÄÖÜ]?[a-zäöüß]+|[A-ZÄÖÜ]+(?![a-zäöüß])|\d+")
_WORD_PART_RE = re.compile(r"[A-Za-zÄÖÜäöüß]+|\d{1,3}")


def allowed_addresses(cwd=None):
    """The clone's declared identities, lower-cased: user.email plus the addresses in
    imprint.allowedIdentity ("Name <address>" or a bare address), the same list
    .githooks/pre-push reads."""
    out = set()
    for line in (_git(["config", "--get", "user.email"], cwd) + "\n"
                 + _git(["config", "--get-all", "imprint.allowedIdentity"], cwd)).splitlines():
        m = re.search(r"<([^<>\s]+@[^<>\s]+)>", line) or re.fullmatch(r"\s*(\S+@\S+)\s*", line)
        if m:
            out.add(m.group(1).lower())
    return frozenset(out)


_SYNTHETIC_ADDRESS_RE = re.compile(r"[A-Za-z0-9]@[A-Za-z0-9]\.[A-Za-z]{2,}")  # a@b.com: a fixture


def _exempt_address(addr, allowed=frozenset()):
    domain = addr.rsplit("@", 1)[1]
    return bool(NOREPLY_RE.fullmatch(addr) or RESERVED_DOMAIN_RE.match(domain)
                or NOT_ADDRESS_DOMAIN_RE.match(domain) or _SYNTHETIC_ADDRESS_RE.fullmatch(addr)
                or addr.lower() in allowed)


def alarm_view(text, allowed=frozenset()):
    """Text as the local alarm sees it: exempt addresses removed, all else unchanged."""
    def repl(m):
        addr = m.group(0).rstrip(".-")  # RX_EMAIL may take a sentence-final dot along
        return "" if _exempt_address(addr, allowed) else m.group(0)
    return RX_EMAIL.sub(repl, text)


def _word_value(v):
    """Two or more parts, all letters or short digit runs: a name, not a key."""
    parts = [p for p in re.split(r"[-_./:+=]+", v) if p]
    return len(parts) >= 2 and all(_WORD_PART_RE.fullmatch(p) for p in parts)


def _entropy(v):
    """Shannon entropy in bits per character."""
    n = len(v)
    return -sum(c / n * math.log2(c / n) for c in collections.Counter(v).values()) if n else 0.0


def _known_prefix(v):
    return bool(_PREFIX_AT_START_RE.match(v) or _PREFIX_AT_SEGMENT_RE.search(v))


def _real_prefix_token(v):
    """A known prefix (at the start or a segment start) whose rest is no fixture:
    "sk-fake-..." and AWS's "AKIA...EXAMPLE" are documentation fakes."""
    m = _PREFIX_AT_START_RE.match(v)
    if m and v[m.end():] and not _fixture(v[m.end():]):
        return True
    return any(not _fixture(m.group("body")) for m in _PREFIX_AT_SEGMENT_RE.finditer(v))


def _fixture(v, first_only=False):
    """Placeholder, or a value with a fixture segment (word starting with a marker);
    first_only (password keys): only if the value STARTS with a fixture segment, so
    "synth-password" is a fixture but "mein geheimes Passwort 2024" is not."""
    if len(set(v)) <= 1 or _PLACEHOLDER_RE.search(v):
        return True
    squashed = re.sub(r"[^a-z]", "", v.lower())
    if "changeme" in squashed or "replaceme" in squashed:
        return True
    segments = _SEGMENT_RE.findall(v)[:1] if first_only else _SEGMENT_RE.findall(v)
    return any(seg.lower().startswith(_FIXTURE_MARKERS) for seg in segments)


def _looks_random(v):
    n = len(v)
    varied = any(c.isdigit() for c in v) or (any(c.islower() for c in v) and any(c.isupper() for c in v))
    return n >= 12 and varied and _entropy(v) >= min(3.0, 2.2 + 0.025 * (n - 12))


def _password_like(v):
    """Password keys: >= 8 characters with a letter and a digit or a special character;
    fixture markers do not count here, only references like ${...} or <...>."""
    return (len(v) >= 8 and any(c.isalpha() for c in v)
            and any(c.isdigit() or not (c.isalnum() or c == "_") for c in v)
            and not (len(set(v)) <= 1 or _PLACEHOLDER_RE.search(v)))


def _kv_alarm(m):
    pw = bool(m.group("pw")) and not _LABEL_KEY_RE.search(m.group("key"))
    if m.group("key") == "PASS" and m.group("sep") == ":":
        pw = False  # a test status line ("PASS: ..."), not a password key
    quoted = m.group("dqv") is not None or m.group("sqv") is not None
    if quoted:
        v = m.group("dqv") if m.group("dqv") is not None else m.group("sqv")
        if _known_prefix(v):
            return True
        if pw:
            return _password_like(v)
        if _fixture(v):
            return False
        return not any(c.isspace() for c in v) and not _word_value(v) and _looks_random(v)
    v = m.group("uv")
    if pw:  # unquoted: a dotted value is an expression (cfg.Password, os.Getwd)
        return "." not in v and _password_like(v)
    if _fixture(v) and not _known_prefix(v):
        return False
    return (len(v) >= 20 and any(c.isalpha() for c in v) and sum(c.isdigit() for c in v) >= 4
            and not _word_value(v))


def local_alarm(text):
    """True if text (already through alarm_view) holds an address or a real-looking secret."""
    if RX_EMAIL.search(text) or any(not _fixture(m.group(0)[4:]) for m in RX_AWS_KEY_ID.finditer(text)):
        return True
    if any(any(c.isdigit() for c in m.group("body")) and not _fixture(m.group("body"))
           for m in _FREE_PREFIX_RE.finditer(text)):
        return True
    if any(_real_prefix_token(m.group("v")) or not (_fixture(m.group("v")) or _word_value(m.group("v")))
           for m in LOCAL_BEARER_RE.finditer(text)):
        return True
    return any(_kv_alarm(m) for m in LOCAL_KV_RE.finditer(text))


def added_lines(diff_text):
    """The added lines of a unified diff, without the leading '+'. '+++' is skipped only
    in file headers (between 'diff ...' and the first '@@'), so an added line that itself
    starts with '++' still counts. Text without any 'diff ' header counts as one hunk."""
    out, in_hunk = [], True
    for line in diff_text.splitlines():
        if line.startswith("diff "):
            in_hunk = False
        elif line.startswith("@@"):
            in_hunk = True
        elif in_hunk and line.startswith("+"):
            out.append(line[1:])
    return "\n".join(out)


def leak_found(message, diff_text, file_names, allowed=frozenset()):
    """Local leak alarm for one change: message, added diff lines and file names."""
    return any(local_alarm(alarm_view(t, allowed))
               for t in (message or "", added_lines(diff_text or ""), file_names or ""))


def _git(args, cwd=None):
    try:
        r = subprocess.run(["git"] + args, cwd=cwd, capture_output=True, text=True, timeout=20)
        return r.stdout if r.returncode == 0 else ""
    except Exception:
        return ""


def commit_units(rng, cwd=None):
    """(message, diff, file names) per commit of a range, oldest first, each diff
    against the first parent: a value added in one commit and removed in the next is
    still in history, so the local alarm must see every commit, not the net diff."""
    units = []
    for sha in _git(["rev-list", "--reverse", rng], cwd).split():
        units.append((_git(["log", "-1", "--format=%B", sha], cwd),
                      _git(["show", "--format=", "--no-color", "--diff-merges=first-parent", "-p", sha], cwd),
                      _git(["show", "--format=", "--name-only", "--diff-merges=first-parent", sha], cwd)))
    return units


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
