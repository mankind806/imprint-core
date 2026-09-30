#!/usr/bin/env bash
# rechte-anwenden-test.sh — testet setup/vorlagen/rechte-anwenden.sh gegen
# die Runde-5- und Runde-6-Befunde aus PR 36 (CL-012): ein scheiterndes cp
# bei der Sicherung, ein gleichzeitiger Schreibzugriff während der
# Diff-Rückfrage, ein Abbruch per HUP/INT/TERM während der Rückfrage,
# statusline.py unabhängig von settings.json (seit Runde 6 auch mit
# Rückfrage bei einer Neuinstallation), ein Symlink-Ziel in den eigenen
# Checkout (auch über ein symlinked Zwischenverzeichnis), eine
# settings.json mit mehr als einem JSON-Dokument, ein nicht beschreibbares
# bzw. kein reguläres Ziel, der eigenständige Unterbefehl agy-statusline
# und ein vollständiger Rückbau. Jedes Szenario legt sein eigenes Fake-HOME
# per mktemp -d an, ändert nie eine echte Datei unter dem eigenen HOME und
# räumt sich danach selbst auf. Ohne Netz- oder Systemzugriff, gedacht für
# .github/workflows/check.yml.

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
  # Schreibt direkt auf die bekannte Zieldatei ($h, zur Erzeugungszeit fest
  # eingesetzt), statt sich auf "$2" zu verlassen: seit Runde 7 zeigt
  # "claude" den Diff über zeige_diff_json auf zwei mit "jq -S ."
  # vorformatierten Zwischendateien an (Befund 1), diff bekommt also nicht
  # mehr direkt die Zieldatei als Argument.
  cat >"$stub/diff" <<EOF
#!/usr/bin/env bash
"$real_diff" "\$@" >/dev/null; rc=\$?
printf '%s' '{"env":{"FOO":"bar-von-anderswo-geaendert"},"permissions":{"allow":["Bash(ls:*)"],"deny":["Bash(rm -rf /:*)"]}}' > "$h/.claude/settings.json"
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

# --- T6 (Befund 5, Runde 6: Zeitstempel-Flackern behoben): rueckbau zeigt
# den Diff, fragt einmal nach, sichert den aktuellen Stand vorher unter
# einem neuen Zeitstempel und spielt dann claude, agy und eine frisch
# installierte statusline.py wieder zurück (letztere per Marker entfernt,
# weil sie vorher nicht existierte). claude und agy laufen in getrennten
# Aufrufen und können — je nach Sekundengrenze — unterschiedliche
# Zeitstempel bekommen; der Test liest deshalb den Zeitstempel aus der
# jeweils letzten Ausgabezeile "Zeitstempel für rueckbau: …" JEDES Aufrufs
# (nicht aus einem Sicherungs-Dateinamen) und ruft rueckbau einmal je
# unterschiedlichem Zeitstempel auf. Ein Test, der nur den
# .claude-Zeitstempel gelesen und rueckbau nur damit aufgerufen hätte, wäre
# genau dann flackernd gewesen, wenn agy einen anderen Zeitstempel bekam
# (Befund aus Runde 6) — der agy-Anteil (settings.json + statusline.py)
# wäre dann unbemerkt nicht zurückgespielt worden. ---
t6_rueckbau_rundlauf() {
  local h; neues_home h

  local vor_claude vor_agy
  vor_claude=$(cat "$h/.claude/settings.json")
  vor_agy=$(cat "$h/.gemini/antigravity-cli/settings.json")

  local claude_out agy_out
  claude_out=$(echo j | HOME="$h" bash "$skript" claude 2>&1) || { report "T6 Rückbau-Rundlauf" 1 "claude-Anwenden fehlgeschlagen: $claude_out"; return; }
  agy_out=$(echo j | HOME="$h" PROJEKTE=/nicht-verwendet TRUSTED_WORKSPACE="$h" bash "$skript" agy 2>&1) || { report "T6 Rückbau-Rundlauf" 1 "agy-Anwenden fehlgeschlagen: $agy_out"; return; }

  local ts_claude ts_agy
  ts_claude=$(printf '%s\n' "$claude_out" | sed -n 's/^Zeitstempel für rueckbau: //p' | tail -1)
  ts_agy=$(printf '%s\n' "$agy_out" | sed -n 's/^Zeitstempel für rueckbau: //p' | tail -1)
  if [ -z "$ts_claude" ] || [ -z "$ts_agy" ]; then
    report "T6 Rückbau-Rundlauf" 1 "kein Zeitstempel in der Ausgabe gefunden (claude=[$ts_claude], agy=[$ts_agy])"
    return
  fi

  # agy läuft chronologisch NACH claude, ts_agy ist also nie kleiner als
  # ts_claude. rueckbau legt bei jedem Aufruf eine eigene, neue Sicherung
  # des GERADE AKTUELLEN Standes unter einem frischen Zeitstempel an; würde
  # dieser zweite Aufruf zuerst für ts_claude laufen, könnte seine eigene
  # (später, in Echtzeit gebildete) Sicherung zufällig genau auf ts_agy
  # fallen und von einem anschließenden "rueckbau ts_agy" fälschlich als
  # dessen Sicherung gelesen werden. Der jüngere Zeitstempel zuerst schließt
  # das aus: eine Selbst-Sicherung entsteht dabei immer erst NACH ts_agy,
  # kann also nie mit dem noch ausstehenden, kleineren ts_claude kollidieren.
  local rc=0
  if [ "$ts_agy" != "$ts_claude" ]; then
    echo j | HOME="$h" bash "$skript" rueckbau "$ts_agy" >/dev/null 2>&1 || rc=$?
  fi
  echo j | HOME="$h" bash "$skript" rueckbau "$ts_claude" >/dev/null 2>&1 || rc=$?

  local nach_claude nach_agy
  nach_claude=$(cat "$h/.claude/settings.json")
  nach_agy=$(cat "$h/.gemini/antigravity-cli/settings.json")

  if [ "$rc" -eq 0 ] && [ "$vor_claude" = "$nach_claude" ] && [ "$vor_agy" = "$nach_agy" ] \
     && [ ! -e "$h/.gemini/antigravity-cli/statusline.py" ]; then
    report "T6 Rückbau-Rundlauf (claude+agy+statusline)" 0
  else
    report "T6 Rückbau-Rundlauf (claude+agy+statusline)" 1 \
      "rc=$rc, ts_claude=$ts_claude, ts_agy=$ts_agy, claude wiederhergestellt=$([ "$vor_claude" = "$nach_claude" ] && echo ja || echo nein), agy wiederhergestellt=$([ "$vor_agy" = "$nach_agy" ] && echo ja || echo nein), statusline entfernt=$([ ! -e "$h/.gemini/antigravity-cli/statusline.py" ] && echo ja || echo nein)"
  fi
}

# --- T7 (Befund 1, Runde 6): der ganze Zielpfad wird IMMER mit readlink -f
# aufgelöst, auch wenn nur ein Zwischenverzeichnis (nicht die Zieldatei
# selbst) ein Symlink ist — z. B. ~/.claude als Symlink in eine Kopie des
# Checkouts. Muss ohne jede Änderung abgelehnt werden. ---
t7_symlink_zwischenverzeichnis() {
  local repo; repo=$(mktemp -d); aufraeumen_dirs+=("$repo")
  mkdir -p "$repo/setup"
  cp -r "$vorlagen_dir" "$repo/setup/vorlagen"
  local kopie_skript="$repo/setup/vorlagen/rechte-anwenden.sh"

  local h; h=$(mktemp -d); aufraeumen_dirs+=("$h")
  local planted="$repo/planted-claude-dir"
  mkdir -p "$planted"
  cat >"$planted/settings.json" <<'EOF'
{"env":{},"permissions":{"allow":[],"deny":["x"]}}
EOF
  ln -s "$planted" "$h/.claude"

  local vor; vor=$(cat "$planted/settings.json")
  echo j | HOME="$h" bash "$kopie_skript" claude >/dev/null 2>&1
  local rc=$?
  local nach; nach=$(cat "$planted/settings.json")

  if [ "$rc" -ne 0 ] && [ "$vor" = "$nach" ]; then
    report "T7 Symlink-Zwischenverzeichnis (~/.claude) wird abgelehnt" 0
  else
    report "T7 Symlink-Zwischenverzeichnis (~/.claude) wird abgelehnt" 1 \
      "rc=$rc, unverändert=$([ "$vor" = "$nach" ] && echo ja || echo nein)"
  fi
}

# --- T8 (Befund 2, Runde 6): der eigenständige Unterbefehl agy-statusline
# installiert statusline.py unabhängig von agy/settings.json, braucht
# weder PROJEKTE noch TRUSTED_WORKSPACE, und braucht auch für eine
# Neuinstallation eine Rückfrage (Befund 5). ---
t8_agy_statusline_eigenstaendig() {
  local h; h=$(mktemp -d); aufraeumen_dirs+=("$h")
  mkdir -p "$h/.gemini/antigravity-cli"

  local out
  out=$(echo j | HOME="$h" bash "$skript" agy-statusline 2>&1)
  local rc=$?

  if [ "$rc" -eq 0 ] && [ -x "$h/.gemini/antigravity-cli/statusline.py" ] \
     && printf '%s' "$out" | grep -qF 'Neu installieren'; then
    report "T8 agy-statusline installiert eigenständig, ohne PROJEKTE/TRUSTED_WORKSPACE" 0
  else
    report "T8 agy-statusline installiert eigenständig, ohne PROJEKTE/TRUSTED_WORKSPACE" 1 "rc=$rc, output=[$out]"
  fi
}

# --- T9 (Befund 7, Runde 6): eine settings.json mit mehr als einem
# aneinandergehängten JSON-Dokument wird abgelehnt, ohne jede Änderung. ---
t9_mehrere_json_dokumente() {
  local h; neues_home h
  printf '{"a":1}\n{"b":2}\n' >"$h/.claude/settings.json"
  local vor; vor=$(cat "$h/.claude/settings.json")

  echo j | HOME="$h" bash "$skript" claude >/dev/null 2>&1
  local rc=$?
  local nach; nach=$(cat "$h/.claude/settings.json")

  if [ "$rc" -ne 0 ] && [ "$vor" = "$nach" ]; then
    report "T9 settings.json mit mehr als einem JSON-Dokument wird abgelehnt" 0
  else
    report "T9 settings.json mit mehr als einem JSON-Dokument wird abgelehnt" 1 "rc=$rc, unverändert=$([ "$vor" = "$nach" ] && echo ja || echo nein)"
  fi
}

# --- T10 (Befund 3, Runde 6): agy prüft vorab, dass ALLE seine Ziele
# (settings.json UND statusline.py) reguläre, beschreibbare Dateien sind —
# eine statusline.py, die ein Verzeichnis ist, darf settings.json nicht
# mehr anfassen (kein Teilschreiben). ---
t10_agy_ziel_kein_teilschreiben() {
  local h; neues_home h
  mkdir -p "$h/.gemini/antigravity-cli/statusline.py"
  local vor; vor=$(cat "$h/.gemini/antigravity-cli/settings.json")

  echo j | HOME="$h" PROJEKTE=/nicht-verwendet TRUSTED_WORKSPACE="$h" bash "$skript" agy >/dev/null 2>&1
  local rc=$?
  local nach; nach=$(cat "$h/.gemini/antigravity-cli/settings.json")

  if [ "$rc" -ne 0 ] && [ "$vor" = "$nach" ]; then
    report "T10 agy bricht vor jeder Änderung ab, wenn statusline.py kein reguläres Ziel ist" 0
  else
    report "T10 agy bricht vor jeder Änderung ab, wenn statusline.py kein reguläres Ziel ist" 1 \
      "rc=$rc, settings unverändert=$([ "$vor" = "$nach" ] && echo ja || echo nein)"
  fi
}

# --- T11 (Befund 1, Runde 7): claude bildet die Vereinigung aus Bestand
# und Vorlage für permissions.allow/permissions.deny (CL-131) - eigene,
# vor dem Anwenden vorhandene Regeln (auch eine so scharfe wie
# "Bash(rm -rf /:*)") gehen dabei nicht verloren, defaultMode bleibt
# ebenfalls erhalten. ---
t11_claude_merge_vereinigung() {
  local h; h=$(mktemp -d); aufraeumen_dirs+=("$h")
  mkdir -p "$h/.claude"
  cat >"$h/.claude/settings.json" <<'EOF'
{"env":{"FOO":"bar"},"permissions":{"allow":["Bash(ls:*)"],"deny":["Bash(rm -rf /:*)","Read(~/.config/archiv/**)","Bash(git push --force:*)"],"defaultMode":"auto"}}
EOF
  chmod 600 "$h/.claude/settings.json"

  echo j | HOME="$h" bash "$skript" claude >/dev/null 2>&1
  local rc=$?
  local f="$h/.claude/settings.json"

  local fehlt=""
  for e in "Bash(rm -rf /:*)" "Read(~/.config/archiv/**)" "Bash(git push --force:*)" "Bash(ls:*)"; do
    jq -e --arg e "$e" '(.permissions.allow + .permissions.deny) | index($e) != null' "$f" >/dev/null 2>&1 \
      || fehlt="$fehlt [$e]"
  done
  local defaultmode_ok=1
  jq -e '.permissions.defaultMode=="auto" and .env.FOO=="bar"' "$f" >/dev/null 2>&1 && defaultmode_ok=0

  if [ "$rc" -eq 0 ] && [ -z "$fehlt" ] && [ "$defaultmode_ok" -eq 0 ]; then
    report "T11 claude-Merge verliert keine bestehenden allow-/deny-Einträge (Vereinigung)" 0
  else
    report "T11 claude-Merge verliert keine bestehenden allow-/deny-Einträge (Vereinigung)" 1 \
      "rc=$rc, fehlende Einträge=[$fehlt], defaultMode/env erhalten=$([ "$defaultmode_ok" -eq 0 ] && echo ja || echo nein)"
  fi
}

# --- T12 (Befund 2b, Runde 7): rueckbau prüft VOR jeder Änderung auch
# statusline.py auf ein beschreibbares Zielverzeichnis - eine
# statusline.py, die per Symlink in ein nicht mehr beschreibbares
# Verzeichnis zeigt, darf claude nicht schon zurückgespielt haben, bevor
# der Rückbau daran scheitert. ---
t12_rueckbau_statusline_nicht_beschreibbar() {
  local h; neues_home h
  local ts
  ts=$(echo j | HOME="$h" bash "$skript" claude 2>&1 | sed -n 's/^Zeitstempel für rueckbau: //p' | tail -1)
  if [ -z "$ts" ]; then
    report "T12 rueckbau bricht ab, wenn statusline.py-Verzeichnis nicht beschreibbar ist" 1 "kein Zeitstempel"
    return
  fi

  local ro; ro=$(mktemp -d); aufraeumen_dirs+=("$ro")
  echo "old-content" >"$ro/statusline.py"
  echo "backup-content" >"$ro/statusline.py.bak-$ts"
  ln -s "$ro/statusline.py" "$h/.gemini/antigravity-cli/statusline.py"
  chmod 555 "$ro"

  local vor_claude; vor_claude=$(cat "$h/.claude/settings.json")
  echo j | HOME="$h" bash "$skript" rueckbau "$ts" >/dev/null 2>&1
  local rc=$?
  chmod 755 "$ro"
  local nach_claude; nach_claude=$(cat "$h/.claude/settings.json")

  if [ "$rc" -ne 0 ] && [ "$vor_claude" = "$nach_claude" ]; then
    report "T12 rueckbau bricht ab, wenn statusline.py-Verzeichnis nicht beschreibbar ist" 0
  else
    report "T12 rueckbau bricht ab, wenn statusline.py-Verzeichnis nicht beschreibbar ist" 1 \
      "rc=$rc, claude unverändert=$([ "$vor_claude" = "$nach_claude" ] && echo ja || echo nein)"
  fi
}

# --- T13 (Befund 1, Runde 8): agy bildet die Vereinigung aus Bestand und
# Vorlage für permissions.allow/permissions.deny genau wie claude — eigene,
# vorher vorhandene Regeln (allow UND deny) und ein zusätzlicher
# Top-Level-Schlüssel (hier "model") gehen dabei nicht verloren, obwohl
# agy "ersetzen" ist und statusLine/trustedWorkspaces weiterhin aus der
# Vorlage übernommen werden. ---
t13_agy_merge_vereinigung() {
  local h; h=$(mktemp -d); aufraeumen_dirs+=("$h")
  mkdir -p "$h/.gemini/antigravity-cli"
  cat >"$h/.gemini/antigravity-cli/settings.json" <<'EOF'
{"model":"custom-model","permissions":{"allow":["command(make)"],"deny":["command(git reset --hard)"]},"trustedWorkspaces":["/old"]}
EOF
  chmod 600 "$h/.gemini/antigravity-cli/settings.json"

  echo j | HOME="$h" PROJEKTE=/nicht-verwendet TRUSTED_WORKSPACE="$h" bash "$skript" agy >/dev/null 2>&1
  local rc=$?
  local f="$h/.gemini/antigravity-cli/settings.json"

  local fehlt=""
  jq -e '.permissions.allow|index("command(make)")!=null' "$f" >/dev/null 2>&1 || fehlt="$fehlt [allow command(make)]"
  jq -e '.permissions.deny|index("command(git reset --hard)")!=null' "$f" >/dev/null 2>&1 || fehlt="$fehlt [deny command(git reset --hard)]"
  local model_ok=1
  jq -e '.model=="custom-model"' "$f" >/dev/null 2>&1 && model_ok=0
  local trustedws_ok=1
  jq -e --arg h "$h" '.trustedWorkspaces[0]==$h' "$f" >/dev/null 2>&1 && trustedws_ok=0

  if [ "$rc" -eq 0 ] && [ -z "$fehlt" ] && [ "$model_ok" -eq 0 ] && [ "$trustedws_ok" -eq 0 ]; then
    report "T13 agy-Merge verliert keine bestehenden Regeln/Schlüssel (Vereinigung)" 0
  else
    report "T13 agy-Merge verliert keine bestehenden Regeln/Schlüssel (Vereinigung)" 1 \
      "rc=$rc, fehlend=[$fehlt], model erhalten=$([ "$model_ok" -eq 0 ] && echo ja || echo nein), trustedWorkspaces aus Vorlage=$([ "$trustedws_ok" -eq 0 ] && echo ja || echo nein)"
  fi
}

# --- T14 (Befund 3, Runde 8): eine manipulierte claude-rechte.json mit
# einem fremden Top-Level-Schlüssel (defaultMode) wird VOR jeder Änderung
# abgelehnt — läuft gegen eine Kopie von vorlagen/, nie gegen den echten
# Checkout. ---
t14_claude_manipulierte_vorlage() {
  local repo; repo=$(mktemp -d); aufraeumen_dirs+=("$repo")
  mkdir -p "$repo/setup"
  cp -r "$vorlagen_dir" "$repo/setup/vorlagen"
  cat >"$repo/setup/vorlagen/claude-rechte.json" <<'EOF'
{"permissions":{"allow":["Bash(gh pr merge:*)"],"deny":["x"]},"defaultMode":"bypassPermissions"}
EOF
  local kopie_skript="$repo/setup/vorlagen/rechte-anwenden.sh"

  local h; h=$(mktemp -d); aufraeumen_dirs+=("$h")
  mkdir -p "$h/.claude"
  cat >"$h/.claude/settings.json" <<'EOF'
{"permissions":{"allow":[],"deny":["y"]},"defaultMode":"default"}
EOF
  local vor; vor=$(cat "$h/.claude/settings.json")

  echo j | HOME="$h" bash "$kopie_skript" claude >/dev/null 2>&1
  local rc=$?
  local nach; nach=$(cat "$h/.claude/settings.json")

  if [ "$rc" -ne 0 ] && [ "$vor" = "$nach" ]; then
    report "T14 manipulierte claude-rechte.json (fremder Top-Level-Schlüssel) wird abgelehnt" 0
  else
    report "T14 manipulierte claude-rechte.json (fremder Top-Level-Schlüssel) wird abgelehnt" 1 \
      "rc=$rc, unverändert=$([ "$vor" = "$nach" ] && echo ja || echo nein)"
  fi
}

# --- T15 (Befund 4, Runde 8): claude prüft jetzt auch vorab, dass die
# Zieldatei beschreibbar ist (0444 wird abgelehnt, nichts geschrieben). ---
t15_claude_ziel_nicht_beschreibbar() {
  local h; h=$(mktemp -d); aufraeumen_dirs+=("$h")
  mkdir -p "$h/.claude"
  cat >"$h/.claude/settings.json" <<'EOF'
{"permissions":{"allow":[],"deny":["x"]}}
EOF
  chmod 444 "$h/.claude/settings.json"
  local vor; vor=$(cat "$h/.claude/settings.json")

  echo j | HOME="$h" bash "$skript" claude >/dev/null 2>&1
  local rc=$?
  chmod 644 "$h/.claude/settings.json" 2>/dev/null || true
  local nach; nach=$(cat "$h/.claude/settings.json")

  if [ "$rc" -ne 0 ] && [ "$vor" = "$nach" ]; then
    report "T15 claude bricht ab, wenn settings.json nicht beschreibbar ist (0444)" 0
  else
    report "T15 claude bricht ab, wenn settings.json nicht beschreibbar ist (0444)" 1 \
      "rc=$rc, unverändert=$([ "$vor" = "$nach" ] && echo ja || echo nein)"
  fi
}

# --- T16 (Befund 2, Runde 8): eine settings.json ohne permissions-Block
# bricht mit einer klaren Meldung ab (kein jq-Fehler aus "null | keys"),
# und wendet trotzdem korrekt an (die Vorlage liefert permissions neu). ---
t16_claude_ohne_permissions_block() {
  local h; h=$(mktemp -d); aufraeumen_dirs+=("$h")
  mkdir -p "$h/.claude"
  echo '{"env":{"FOO":"bar"}}' >"$h/.claude/settings.json"

  local out
  out=$(echo j | HOME="$h" bash "$skript" claude 2>&1)
  local rc=$?
  local f="$h/.claude/settings.json"

  if [ "$rc" -eq 0 ] && jq -e '.permissions.deny|length>0' "$f" >/dev/null 2>&1 \
     && jq -e '.env.FOO=="bar"' "$f" >/dev/null 2>&1; then
    report "T16 settings.json ohne permissions-Block wird sauber gemerged" 0
  else
    report "T16 settings.json ohne permissions-Block wird sauber gemerged" 1 "rc=$rc, output=[$out]"
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
t7_symlink_zwischenverzeichnis
t8_agy_statusline_eigenstaendig
t9_mehrere_json_dokumente
t11_claude_merge_vereinigung
t12_rueckbau_statusline_nicht_beschreibbar
t10_agy_ziel_kein_teilschreiben
t13_agy_merge_vereinigung
t14_claude_manipulierte_vorlage
t15_claude_ziel_nicht_beschreibbar
t16_claude_ohne_permissions_block

echo "---"
echo "$pass PASS, $fail FAIL"
[ "$fail" -eq 0 ]
