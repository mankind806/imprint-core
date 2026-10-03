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


def _die_with_parent_preexec():
    """A preexec_fn that makes the child get SIGKILL when its parent dies (Linux
    prctl PR_SET_PDEATHSIG; kept across exec). None where libc/prctl isn't
    available. Only for a single-threaded parent: preexec_fn runs between fork and
    exec. libc is looked up here, in the parent; the returned function never raises."""
    try:
        import ctypes
        import signal
        prctl = ctypes.CDLL(None, use_errno=True).prctl
        args = (ctypes.c_int(1), ctypes.c_ulong(int(signal.SIGKILL)),  # 1 = PR_SET_PDEATHSIG
                ctypes.c_ulong(0), ctypes.c_ulong(0), ctypes.c_ulong(0))
    except Exception:
        return None
    parent = os.getpid()

    def preexec():
        try:
            prctl(*args)
            if os.getppid() != parent:  # the parent died before prctl took effect
                os._exit(0)
        except BaseException:
            pass
    return preexec


def get_key(timeout=5, die_with_parent=False):
    """TYPESAFE_API_KEY, else `secret-tool lookup service typesafe key api`; None if absent.

    `timeout` bounds only the secret-tool subprocess (the env-var path is instant);
    callers under a tighter deadline (e.g. a Stop hook) can cap it below the default.
    die_with_parent=True (ts-done-check's worker) makes secret-tool get SIGKILL if
    the caller dies first; it applies only while the caller has a single thread
    (otherwise secret-tool is started as before)."""
    key = os.environ.get("TYPESAFE_API_KEY", "").strip()
    if key:
        return key
    try:
        kw = {}
        if die_with_parent:
            import threading
            if threading.active_count() == 1:
                preexec = _die_with_parent_preexec()
                if preexec is not None:
                    kw["preexec_fn"] = preexec
        r = subprocess.run(["secret-tool", "lookup", "service", "typesafe", "key", "api"],
                           capture_output=True, text=True, timeout=timeout, **kw)
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


def _failure_reason(e):
    """(reason, HTTP status or None) for an exception raised inside post()."""
    import socket
    if isinstance(e, urllib.error.HTTPError):  # includes an unfollowed 3xx
        return "http_status", e.code
    if isinstance(e, (TimeoutError, socket.timeout)) or (
            isinstance(e, urllib.error.URLError) and isinstance(e.reason, (TimeoutError, socket.timeout))):
        return "timeout", None
    import http.client
    if isinstance(e, (OSError, http.client.HTTPException)):  # refused, DNS, TLS, reset, truncated
        return "network", None
    return "internal", None


def post(state, questions, timeout=TIMEOUT, key=None, detail=None):
    """One batched request. Returns the `answers` dict, or None on ANY failure.

    Second line of defence: every string leaf of state and questions is masked here
    again, in addition to the masking each tool does itself.

    `key` lets a caller that already fetched the key (e.g. under its own keyring-
    timeout budget) hand it over directly instead of having post() call get_key()
    again with get_key()'s own default timeout.

    `detail`, if given a dict, receives why a None came back: detail["reason"] is
    no_key, http_status (with detail["status"], e.g. 401, 500, an unfollowed 302),
    timeout, network (refused, DNS, TLS, reset), bad_response (body not JSON, or no
    `answers` object) or internal. The return value is the same with or without it."""
    if detail is None:
        detail = {}
    try:
        if key is None:
            key = get_key()
        if not key:
            detail["reason"] = "no_key"
            return None
        body = json.dumps({"state": _mask_tree(state), "model": MODEL,
                           "questions": _mask_tree(questions)}).encode()
        req = urllib.request.Request(URL, data=body, method="POST", headers={
            "Content-Type": "application/json",
            "User-Agent": "typesafe-dev/0.1"})
        req.add_unredirected_header("Authorization", "Bearer " + key)
        with _OPENER.open(req, timeout=timeout) as resp:
            if resp.status != 200:
                detail["reason"], detail["status"] = "http_status", resp.status
                return None
            raw = resp.read()
            try:
                parsed = json.loads(raw.decode())
            except ValueError:  # not UTF-8 or not JSON
                detail["reason"] = "bad_response"
                return None
            answers = parsed.get("answers") if isinstance(parsed, dict) else None
            if not isinstance(answers, dict):
                detail["reason"] = "bad_response"
                return None
            return answers
    except Exception as e:
        if isinstance(e, urllib.error.HTTPError):  # e.g. an unfollowed 3xx: release the response
            e.close()
        detail["reason"], status = _failure_reason(e)
        if status is not None:
            detail["status"] = status
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


# [\w.+-]+@ is quadratic in practice for the same reason RX_OPAQUE's lookaheads
# were: re retries the greedy local-part scan from every position in a long run
# of the charset with no '@' reachable from there. Confirmed empirically
# (2026-10-02): 50k such characters took 5.5s (letters) to 10.7s (hex) to mask.
# _LinearEmailMatcher below is a drop-in replacement supporting every way this
# module uses RX_EMAIL -- .subn(str, text) (mask_detail), .sub(callable, text)
# (alarm_view, which needs a real-enough match object for m.group(0)), and
# .search(text) (local_alarm) -- finding the same matches in linear time: '@'
# is never itself part of any surrounding character class, so once anchored on
# a real '@' there is no backtracking ambiguity in what can match around it
# (the local part before it, the domain and TLD after it are each a single
# greedy run with a required literal in between, checked once per '@', not
# once per starting position).
_EMAIL_LOCAL_CHAR = re.compile(r"[\w.+-]")
_EMAIL_DOMAIN_CHAR = re.compile(r"[\w-]")
_EMAIL_TLD_CHAR = re.compile(r"[\w.-]")


class _EmailMatch:
    """Just enough of re.Match for alarm_view()'s repl(m): m.group(0)."""
    __slots__ = ("_text",)

    def __init__(self, text):
        self._text = text

    def group(self, n=0):
        if n not in (0, "0"):
            raise IndexError("_EmailMatch only has group 0")
        return self._text


class _LinearEmailMatcher:
    def _spans(self, text):
        """[(start, end), ...] of non-overlapping matches, left to right --
        the same set .finditer() would yield, computed without ever trying a
        starting position that isn't a real '@' in the text."""
        spans = []
        n = len(text)
        consumed_to = 0  # end of the last match; nothing before this can start a new one
        search_from = 0
        while True:
            at = text.find("@", search_from)
            if at == -1 or at >= n:
                break
            # local part: walk backward from `at` while [\w.+-], never past
            # the end of a previous match (that text is already consumed).
            start = at
            while start > consumed_to and _EMAIL_LOCAL_CHAR.match(text, start - 1, start):
                start -= 1
            if start == at:  # empty local part: [\w.+-]+ needs at least 1
                search_from = at + 1
                continue
            # domain: walk forward from just after '@' while [\w-]
            i = at + 1
            while i < n and _EMAIL_DOMAIN_CHAR.match(text, i, i + 1):
                i += 1
            if i == at + 1 or i >= n or text[i] != ".":
                search_from = at + 1
                continue
            # tld: walk forward from just after the literal '.' while [\w.-]
            j = i + 1
            k = j
            while k < n and _EMAIL_TLD_CHAR.match(text, k, k + 1):
                k += 1
            if k == j:  # [\w.-]+ needs at least 1
                search_from = at + 1
                continue
            spans.append((start, k))
            consumed_to = k
            search_from = k
        return spans

    def subn(self, repl, text):
        spans = self._spans(text)
        if not spans:
            return text, 0
        out, last = [], 0
        is_callable = callable(repl)
        for start, end in spans:
            out.append(text[last:start])
            out.append(repl(_EmailMatch(text[start:end])) if is_callable else repl)
            last = end
        out.append(text[last:])
        return "".join(out), len(spans)

    def sub(self, repl, text):
        return self.subn(repl, text)[0]

    def search(self, text):
        spans = self._spans(text)
        return _EmailMatch(text[spans[0][0]:spans[0][1]]) if spans else None


RX_EMAIL = _LinearEmailMatcher()
RX_IBAN = re.compile(r"(?<![A-Za-z0-9])[A-Z]{2}\d{2}(?: ?[A-Z0-9]){11,30}(?![A-Za-z0-9])")
# German numbers: +49 or a leading 0, then at least 8 more digits, separators allowed.
RX_PHONE = re.compile(r"(?<![\w+.])(?:\+49|0)(?:[ \t./()-]*\d){8,}(?!\d)")
# Known token prefixes (GitHub, OpenAI/Stripe, Slack, AWS, Google, GitLab, npm).
KNOWN_PREFIX = (r"(?:ghp_|gho_|ghs_|github_pat_|sk-|sk_live_|sk_test_|rk_live_|rk_test_|pk_live_"
                r"|xox[abprs]-|AKIA|ASIA|AIza|GOCSPX-|ya29\.|1//|eyJ|glpat-|npm_)")
_TOKEN_CHARS = r"[A-Za-z0-9_\-./+=]"
RX_KNOWN_TOKEN = re.compile(r"(?<![A-Za-z0-9])" + KNOWN_PREFIX + _TOKEN_CHARS + r"{8,}")
RX_AWS_KEY_ID = re.compile(r"(?<![A-Za-z0-9])(?:AKIA|ASIA)[0-9A-Z]{16}(?![A-Za-z0-9])")
# The two lookaheads below ((?=...*\d)(?=...*[A-Za-z])) are quadratic in practice:
# re's backtracking engine re-tries each lookahead from every position in a long
# run of the charset, so a long run with no digit (or no letter) anywhere in the
# rest of the string makes each position's lookahead re-scan up to the end of
# that run. Confirmed empirically (2026-10-02): 40k such characters took ~5.8s,
# scaling quadratically (doubling the input ~4x'd the time). _LinearOpaqueMatcher
# below is a drop-in replacement (same .subn(repl, text) interface, the only
# method mask_detail() calls on it) that finds the same matches in linear time:
# a qualifying run (length >= 24, contains a digit, contains a letter) is always
# consumed WHOLE by the original's greedy {24,} once found from its own start,
# so checking each maximal charset run once -- instead of re-deriving "is there
# a digit/letter ahead" from every position within it -- gives identical matches.
_RX_OPAQUE_RUN = re.compile(r"[A-Za-z0-9_\-+/]+")  # a single greedy class run: never backtracks
_RX_OPAQUE_HAS_DIGIT = re.compile(r"\d")  # also used as a single-char check at a fixed position, below
_RX_OPAQUE_HAS_ALPHA = re.compile(r"[A-Za-z]")


class _LinearOpaqueMatcher:
    def subn(self, repl, text):
        out, count, last = [], 0, 0
        for m in _RX_OPAQUE_RUN.finditer(text):
            start, end = m.span()
            run = m.group(0)
            if len(run) < 24 or not _RX_OPAQUE_HAS_ALPHA.search(run):
                continue
            # \d is Unicode: the original lookahead's greedy [charset]* always
            # consumes the WHOLE run (since the run is already maximal) before
            # checking \d, so a run with no ASCII digit of its own can still
            # qualify if the single character immediately AFTER it (not part
            # of the charset, so never part of the final match either) happens
            # to be a Unicode decimal digit outside A-Za-z0-9_-+/ (confirmed:
            # "a"*24 + "３" (fullwidth 3) -- found during review, 2026-10-02).
            if not _RX_OPAQUE_HAS_DIGIT.search(run) \
                    and not _RX_OPAQUE_HAS_DIGIT.match(text, end, end + 1):
                continue
            pad_end = end
            for _ in range(2):  # the original's trailing ={0,2}, greedy but capped at 2
                if pad_end < len(text) and text[pad_end] == "=":
                    pad_end += 1
                else:
                    break
            out.append(text[last:start])
            out.append(repl)
            count += 1
            last = pad_end
        out.append(text[last:])
        return "".join(out), count


RX_OPAQUE = _LinearOpaqueMatcher()

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


# Besides \n, str.splitlines() breaks a line at these; git does not. added_lines splits
# at \n only and reads each of these as a space, so the rest of such a line - also one
# that then starts with "diff " - stays part of the added line.
_LINE_BREAKS = str.maketrans({c: " " for c in "\r\x0b\x0c\x1c\x1d\x1e\x85\u2028\u2029"})


def added_lines(diff_text):
    """The added lines of a unified diff, without the leading '+'. '+++' is skipped only
    in file headers (between 'diff ...' and the first '@@'), so an added line that itself
    starts with '++' still counts. Text without any 'diff ' header counts as one hunk.
    A line ends at a newline only, as git writes it; see _LINE_BREAKS."""
    out, in_hunk = [], True
    for line in diff_text.split("\n"):
        if line.startswith("diff "):
            in_hunk = False
        elif line.startswith("@@"):
            in_hunk = True
        elif in_hunk and line.startswith("+"):
            out.append(line[1:].translate(_LINE_BREAKS))
    return "\n".join(out)


def leak_found(message, diff_text, file_names, allowed=frozenset()):
    """Local leak alarm for one change: message, added diff lines and file names."""
    return any(local_alarm(alarm_view(t, allowed))
               for t in (message or "", added_lines(diff_text or ""), file_names or ""))


def git_text(args, cwd=None):
    """stdout of git ARGS, None if git fails. Read as bytes: a byte that is not UTF-8
    becomes U+FFFD and a lone CR stays one, where text=True would fail the whole read
    or end the line there. GIT_DIFF_OPTS could override -U0; a `git replace` would
    show something other than the commit; diff.relative=true would narrow a diff to a
    subdirectory CWD names. -c rather than --no-relative: a git that does not know the
    setting ignores -c, but would fail on the flag and so refuse every commit."""
    env = {k: v for k, v in os.environ.items() if k != "GIT_DIFF_OPTS"}
    env["GIT_NO_REPLACE_OBJECTS"] = "1"
    try:
        r = subprocess.run(["git", "-c", "diff.relative=false"] + args, cwd=cwd, capture_output=True,
                           timeout=120, env=env)
    except Exception:
        return None
    return r.stdout.decode("utf-8", "replace") if r.returncode == 0 else None


def _git(args, cwd=None):
    """git_text, "" if git fails: an unset config key reads as empty."""
    return git_text(args, cwd) or ""


# How every diff the local alarm reads is read: as text even with a NUL byte or a -diff
# attribute, without an external diff, textconv or colour (color.ui=always hides every '+').
DIFF_AS_TEXT = ["--text", "--no-ext-diff", "--no-textconv", "--no-color"]


def commit_units(rng, cwd=None):
    """(message, diff, file names) per commit of a range, oldest first, each diff
    against the first parent: a value added in one commit and removed in the next is
    still in history, so the local alarm must see every commit, not the net diff.
    None if git cannot read the range or one of its commits: a commit not read must
    not pass as one without a leak. --root: log.showRoot=false would show a root
    commit without its diff."""
    shas = git_text(["rev-list", "--reverse", rng], cwd)
    if shas is None:
        return None
    units = []
    for sha in shas.split():
        unit = (git_text(["log", "-1", "--format=%B", sha], cwd),
                git_text(["show", "--format=", "--root", *DIFF_AS_TEXT, "--diff-merges=first-parent", "-p", sha],
                         cwd),
                git_text(["show", "--format=", "--root", "--name-only", "--diff-merges=first-parent", sha], cwd))
        if None in unit:
            return None
        units.append(unit)
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
