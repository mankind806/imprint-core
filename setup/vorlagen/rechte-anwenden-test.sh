#!/usr/bin/env bash
# rechte-anwenden-test.sh — testet setup/vorlagen/rechte-anwenden.sh gegen
# die Runde-5-Befunde aus PR 36 (CL-012): ein scheiterndes cp bei der
# Sicherung, ein gleichzeitiger Schreibzugriff während der Diff-Rückfrage,
# ein Abbruch per HUP/INT/TERM während der Rückfrage, statusline.py
# unabhängig von settings.json, ein Symlink-Ziel in den eigenen Checkout
# und ein vollständiger Rückbau. Jedes Szenario legt sein eigenes
# Fake-HOME per mktemp -d an, ändert nie eine echte Datei unter dem
# eigenen HOME und räumt sich danach selbst auf. Ohne Netz- oder
# Systemzugriff, gedacht für .github/workflows/check.yml.

set -uo pipefail

script_dir=$(cd "${BASH_SOURCE[0]%/*}" && pwd -P)
skript="$script_dir/rechte-anwenden.sh"
vorlagen_dir="$script_dir"

pass=0
fail=0
report() {
  local name="$1" ok="$2" detail="${3:-}"
  if [ "$ok" -eq 0 ]; then
    echo "PASS $name"
    pass=$((pass + 1))
  else
    echo "FAIL $name - $detail"
    fail=$((fail + 1))
  fi
}

# Echte Werkzeuge VOR jeder PATH-Änderung auflösen, damit die Stubs unten
# sie aufrufen können, ohne selbst einen Rechnerpfad fest einzucodieren
# (der Befund an den Stubs aus Runde 5: die trugen /usr/bin und
# /home/linuxbrew/... fest codiert).
real_cp=$(command -v cp) || { echo "cp fehlt" >&2; exit 2; }

aufraeumen_dirs=()
trap 'for d in ${aufraeumen_dirs[@]+"${aufraeumen_dirs[@]}"}; do rm -rf "$d"; done' EXIT

neues_home() {
  # neues_home <var>: legt ein frisches Fake-HOME mit .claude/settings.json
  # und .gemini/antigravity-cli/settings.json an, schreibt seinen Pfad in
  # die genannte Variable und merkt es zum Aufräumen vor.
  local __var="$1" __neues_home_dir
  __neues_home_dir=$(mktemp -d)
  aufraeumen_dirs+=("$__neues_home_dir")
  mkdir -p "$__neues_home_dir/.claude" "$__neues_home_dir/.gemini/antigravity-cli"
  cat >"$__neues_home_dir/.claude/settings.json" <<'EOF'
{"env":{"FOO":"bar"},"hooks":{},"permissions":{"allow":["Bash(ls:*)"],"deny":["Bash(rm -rf /:*)"]}}
EOF
  cat >"$__neues_home_dir/.gemini/antigravity-cli/settings.json" <<'EOF'
{"permissions":{"allow":[],"deny":[]},"trustedWorkspaces":["/old"]}
EOF
  chmod 600 "$__neues_home_dir/.claude/settings.json" "$__neues_home_dir/.gemini/antigravity-cli/settings.json"
  printf -v "$__var" '%s' "$__neues_home_dir"
}

neuer_stub_dir() {
  local __var="$1" __neuer_stub_dir
  __neuer_stub_dir=$(mktemp -d)
  aufraeumen_dirs+=("$__neuer_stub_dir")
  printf -v "$__var" '%s' "$__neuer_stub_dir"
}

# --- T1 (Befund 1): ein cp, das beim Schreiben der Sicherung scheitert,
# muss ohne jede Änderung an der Zieldatei abbrechen und darf keine (leere)
# Sicherung zurücklassen, auf die rueckbau hereinfallen könnte. ---
t1_cp_scheitert() {
  local h; neues_home h
  local stub; neuer_stub_dir stub
  cat >"$stub/cp" <<EOF
#!/usr/bin/env bash
last="\${@: -1}"
case "\$last" in *.bak-*) : > "\$last"; exit 1;; esac
exec "$real_cp" "\$@"
EOF
  chmod +x "$stub/cp"

  local vor nach rc
  vor=$(cat "$h/.claude/settings.json")
  echo j | PATH="$stub:$PATH" HOME="$h" bash "$skript" claude >/dev/null 2>&1
  rc=$?
  nach=$(cat "$h/.claude/settings.json")
  local reste; reste=$(find "$h" \( -name '*.bak-*' -o -name '.rechte-anwenden.*' \) 2>/dev/null)

  if [ "$rc" -ne 0 ] && [ "$vor" = "$nach" ] && [ -z "$reste" ]; then
    report "T1 scheiterndes cp bei der Sicherung bricht ohne Änderung ab" 0
  else
    report "T1 scheiterndes cp bei der Sicherung bricht ohne Änderung ab" 1 \
      "rc=$rc, Ziel unverändert=$([ "$vor" = "$nach" ] && echo ja || echo nein), Reste=[$reste]"
  fi
}

# --- T2 (Befund 2): ändert sich die Zieldatei während der Diff-Rückfrage
# (z. B. durch Claude Code selbst), muss das Skript abbrechen, statt eine
# Sicherung des inzwischen veralteten Standes anzulegen. ---
t2_race_bei_rueckfrage() {
  local h; neues_home h
  local stub; neuer_stub_dir stub
  local real_diff; real_diff=$(command -v diff)
  cat >"$stub/diff" <<EOF
#!/usr/bin/env bash
"$real_diff" "\$@" >/dev/null; rc=\$?
t="\$2"
printf '%s' '{"env":{"FOO":"bar-von-anderswo-geaendert"},"permissions":{"allow":["Bash(ls:*)"],"deny":["Bash(rm -rf /:*)"]}}' > "\$t"
exit \$rc
EOF
  chmod +x "$stub/diff"

  local vor nach rc
  vor=$(cat "$h/.claude/settings.json")
  echo j | PATH="$stub:$PATH" HOME="$h" bash "$skript" claude >/dev/null 2>&1
  rc=$?
  nach=$(cat "$h/.claude/settings.json")
  local reste; reste=$(find "$h" \( -name '*.bak-*' -o -name '.rechte-anwenden.*' \) 2>/dev/null)

  # Die konkurrierende Änderung selbst bleibt stehen (das Skript überschreibt
  # sie nicht rückwirkend) - geprüft wird nur, dass unser eigener Merge NICHT
  # zusätzlich angewendet wurde und keine Sicherung/Zwischendatei übrig blieb.
  if [ "$rc" -ne 0 ] && ! grep -qF 'Bash(gh pr merge:*)' "$h/.claude/settings.json" && [ -z "$reste" ]; then
    report "T2 Änderung während der Rückfrage bricht ohne Sicherung ab" 0
  else
    report "T2 Änderung während der Rückfrage bricht ohne Sicherung ab" 1 "rc=$rc, Reste=[$reste]"
  fi
}

# --- T3 (Befund 3): HUP/INT/TERM während der Rückfrage räumen die
# Zwischendatei zuverlässig auf, egal wie das Skript endet. ---
t3_signal_raeumt_auf() {
  local sig="$1" erwarteter_rc="$2"
  local h; neues_home h
  local stub; neuer_stub_dir stub
  local real_diff; real_diff=$(command -v diff)
  cat >"$stub/diff" <<EOF
#!/usr/bin/env bash
"$real_diff" "\$@" >/dev/null; kill -$sig \$PPID; sleep 0.3; exit 1
EOF
  chmod +x "$stub/diff"

  echo j | PATH="$stub:$PATH" HOME="$h" timeout 5 bash "$skript" claude >/dev/null 2>&1
  local rc=$?
  local reste; reste=$(find "$h" \( -name '*.bak-*' -o -name '.rechte-anwenden.*' \) 2>/dev/null)

  if [ "$rc" -eq "$erwarteter_rc" ] && [ -z "$reste" ]; then
    report "T3 $sig während der Rückfrage räumt auf (rc=$erwarteter_rc)" 0
  else
    report "T3 $sig während der Rückfrage räumt auf (rc=$erwarteter_rc)" 1 "rc=$rc (erwartet $erwarteter_rc), Reste=[$reste]"
  fi
}

# --- T4 (Befund 4): statusline.py wird auch installiert, wenn
# settings.json schon der Vorlage entspricht - unabhängig davon. ---
t4_statusline_unabhaengig() {
  local h; h=$(mktemp -d); aufraeumen_dirs+=("$h")
  mkdir -p "$h/.gemini/antigravity-cli"
  HOME="$h" PROJEKTE=/nicht-verwendet TRUSTED_WORKSPACE="$h" \
    envsubst '${HOME} ${PROJEKTE} ${TRUSTED_WORKSPACE}' \
    <"$vorlagen_dir/agy-settings.json" >"$h/.gemini/antigravity-cli/settings.json"
  chmod 600 "$h/.gemini/antigravity-cli/settings.json"
  # statusline.py existiert bewusst noch nicht.

  local out
  out=$(echo j | HOME="$h" PROJEKTE=/nicht-verwendet TRUSTED_WORKSPACE="$h" bash "$skript" agy 2>&1)
  local rc=$?

  if [ "$rc" -eq 0 ] && [ -x "$h/.gemini/antigravity-cli/statusline.py" ] \
     && printf '%s' "$out" | grep -qF 'Keine Änderung nötig'; then
    report "T4 statusline.py wird installiert, obwohl settings.json schon passt" 0
  else
    report "T4 statusline.py wird installiert, obwohl settings.json schon passt" 1 "rc=$rc, output=[$out]"
  fi
}

# --- T5 (Befund 4): ein Ziel, das per Symlink in den eigenen Checkout
# zeigt, muss VOR jeder Änderung abbrechen - auch vor dem mv der
# settings.json. Läuft gegen eine Kopie von vorlagen/, nie gegen den
# echten Checkout. ---
t5_symlink_in_checkout() {
  local repo; repo=$(mktemp -d); aufraeumen_dirs+=("$repo")
  mkdir -p "$repo/setup"
  cp -r "$vorlagen_dir" "$repo/setup/vorlagen"
  local kopie_skript="$repo/setup/vorlagen/rechte-anwenden.sh"
  local kopie_statusline="$repo/setup/vorlagen/statusline.py"

  local h; neues_home h
  ln -s "$kopie_statusline" "$h/.gemini/antigravity-cli/statusline.py"

  local vor_statusline vor_settings
  vor_statusline=$(cat "$kopie_statusline")
  vor_settings=$(cat "$h/.gemini/antigravity-cli/settings.json")

  echo j | HOME="$h" PROJEKTE=/nicht-verwendet TRUSTED_WORKSPACE="$h" bash "$kopie_skript" agy >/dev/null 2>&1
  local rc=$?

  local nach_statusline nach_settings
  nach_statusline=$(cat "$kopie_statusline")
  nach_settings=$(cat "$h/.gemini/antigravity-cli/settings.json")

  if [ "$rc" -ne 0 ] && [ "$vor_statusline" = "$nach_statusline" ] && [ "$vor_settings" = "$nach_settings" ]; then
    report "T5 Symlink in den Checkout bricht vor jeder Änderung ab" 0
  else
    report "T5 Symlink in den Checkout bricht vor jeder Änderung ab" 1 \
      "rc=$rc, statusline unverändert=$([ "$vor_statusline" = "$nach_statusline" ] && echo ja || echo nein), settings unverändert=$([ "$vor_settings" = "$nach_settings" ] && echo ja || echo nein)"
  fi
}

# --- T6 (Befund 5): rueckbau zeigt den Diff, fragt einmal nach, sichert
# den aktuellen Stand vorher unter einem neuen Zeitstempel und spielt dann
# claude, agy und eine frisch installierte statusline.py wieder zurück
# (letztere per Marker entfernt, weil sie vorher nicht existierte). ---
t6_rueckbau_rundlauf() {
  local h; neues_home h

  local vor_claude vor_agy
  vor_claude=$(cat "$h/.claude/settings.json")
  vor_agy=$(cat "$h/.gemini/antigravity-cli/settings.json")

  echo j | HOME="$h" bash "$skript" claude >/dev/null 2>&1 || { report "T6 Rückbau-Rundlauf" 1 "claude-Anwenden fehlgeschlagen"; return; }
  echo j | HOME="$h" PROJEKTE=/nicht-verwendet TRUSTED_WORKSPACE="$h" bash "$skript" agy >/dev/null 2>&1 || { report "T6 Rückbau-Rundlauf" 1 "agy-Anwenden fehlgeschlagen"; return; }

  local ts; ts=$(find "$h/.claude" -name '*.bak-*' | sed -E 's/.*\.bak-//')
  if [ -z "$ts" ]; then
    report "T6 Rückbau-Rundlauf" 1 "kein Zeitstempel gefunden"
    return
  fi

  echo j | HOME="$h" bash "$skript" rueckbau "$ts" >/dev/null 2>&1
  local rc=$?

  local nach_claude nach_agy
  nach_claude=$(cat "$h/.claude/settings.json")
  nach_agy=$(cat "$h/.gemini/antigravity-cli/settings.json")

  if [ "$rc" -eq 0 ] && [ "$vor_claude" = "$nach_claude" ] && [ "$vor_agy" = "$nach_agy" ] \
     && [ ! -e "$h/.gemini/antigravity-cli/statusline.py" ]; then
    report "T6 Rückbau-Rundlauf (claude+agy+statusline)" 0
  else
    report "T6 Rückbau-Rundlauf (claude+agy+statusline)" 1 \
      "rc=$rc, claude wiederhergestellt=$([ "$vor_claude" = "$nach_claude" ] && echo ja || echo nein), agy wiederhergestellt=$([ "$vor_agy" = "$nach_agy" ] && echo ja || echo nein), statusline entfernt=$([ ! -e "$h/.gemini/antigravity-cli/statusline.py" ] && echo ja || echo nein)"
  fi
}

t1_cp_scheitert
t2_race_bei_rueckfrage
t3_signal_raeumt_auf HUP 129
t3_signal_raeumt_auf INT 130
t3_signal_raeumt_auf TERM 143
t4_statusline_unabhaengig
t5_symlink_in_checkout
t6_rueckbau_rundlauf

echo "---"
echo "$pass PASS, $fail FAIL"
[ "$fail" -eq 0 ]
