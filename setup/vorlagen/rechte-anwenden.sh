#!/usr/bin/env bash
# rechte-anwenden.sh — wendet die geprüften Rechte-Vorlagen für Claude Code
# und agy an, oder macht eine Anwendung rückgängig. Nur der Mensch führt
# dieses Skript aus (Nutzerentscheid CL-119/CL-123) — kein Werkzeug in
# diesem Repository ruft es automatisch auf.
#
# Aufruf:
#   setup/vorlagen/rechte-anwenden.sh claude
#   setup/vorlagen/rechte-anwenden.sh agy
#   setup/vorlagen/rechte-anwenden.sh rueckbau <Zeitstempel>
#
# claude mergt den permissions-Block aus setup/vorlagen/claude-rechte.json in
# ${HOME}/.claude/settings.json (jq -s '.[0] * .[1]', ein tiefer Merge; dabei
# werden Listen wie permissions.allow/permissions.deny vollständig durch die
# Vorlage ersetzt, nicht mit dem Bestand vereinigt. permissions.defaultMode
# und alle anderen Top-Level-Schlüssel — hooks, env, enabledPlugins,
# sandbox, … — bleiben unverändert, weil die Vorlage nur permissions trägt).
# Direkt beim Merge hält das Skript den sha256sum von
# ${HOME}/.claude/settings.json fest und prüft ihn nach der Rückfrage
# erneut, bevor irgendetwas geschrieben wird — ändert sich die Zieldatei
# während der Diff-Rückfrage (z. B. durch Claude Code selbst oder ein
# gleichzeitiges "immer erlauben"), bricht das Skript ohne jede Änderung ab,
# statt eine Sicherung des schon veralteten Standes anzulegen.
#
# agy setzt ${HOME}/.gemini/antigravity-cli/settings.json komplett aus
# setup/vorlagen/agy-settings.json neu (envsubst für ${PROJEKTE} und
# ${TRUSTED_WORKSPACE}, mit derselben sha256sum-Race-Prüfung wie oben) und
# behandelt setup/vorlagen/statusline.py (Ziel
# ${HOME}/.gemini/antigravity-cli/statusline.py) unabhängig davon: fehlt sie,
# installiert das Skript sie ohne Rückfrage (nichts wird dabei überschrieben);
# existiert sie schon und weicht ab, zeigt das Skript auch dafür einen
# eigenen Diff und fragt separat nach — eine Vorlage bestätigt die andere
# nicht mit. Beide Vorbedingungen (u. a., dass keines der beiden Ziele per
# Symlink in den eigenen Checkout dieses Skripts zeigt) werden geprüft,
# bevor irgendetwas geschrieben wird, insbesondere vor einem mv der
# settings.json — eine kaputte statusline.py-Vorbedingung darf keinen schon
# geänderten settings.json-Stand zurücklassen.
#
# Beide Unterbefehle zeigen vor jeder Änderung den Diff und fragen
# ausdrücklich nach; ohne "j"/"J" bricht das Skript ohne jede Änderung ab,
# auch wenn mehrere Ziele in einem Aufruf zur Änderung anstehen (eine
# einzige Rückfrage für alle in diesem Aufruf anstehenden Änderungen, vor
# der ersten davon). Bei Zustimmung liegt zuerst eine Sicherung mit
# sekundengenauem Zeitstempel neben der Zieldatei (cp -p -n; schlägt das cp
# fehl, entfernt das Skript die dabei möglicherweise schon unvollständig
# angelegte Sicherung wieder und bricht ab, statt sie stehen zu lassen —
# rueckbau prüft Sicherungen vor dem Zurückspielen ohnehin auf Inhalt), und
# ein cmp -s gegen das Original bestätigt die Sicherung, bevor irgendetwas
# überschrieben wird. Erst danach übernimmt ein atomares mv die neue Datei
# (gleiches Dateisystem, weil die Zwischendatei per mktemp im selben
# Verzeichnis liegt). Ist die Zieldatei ein Symlink, bleibt der Symlink
# erhalten — geschrieben wird auf die Datei, auf die er zeigt (readlink -f).
# Alle Zwischendateien liegen in globalen Variablen (nicht "local"), damit
# eine einzige EXIT-Falle sie zuverlässig aufräumt, auch wenn das Skript
# mitten in einer Funktion durch ein Signal beendet wird.
#
# rueckbau <Zeitstempel> zeigt zuerst den Diff für jede zu diesem
# Zeitstempel tatsächlich vorhandene Sicherung (claude, agy, statusline.py —
# unabhängig voneinander, da agy und statusline.py seit Runde 5 getrennt
# geschrieben werden) und fragt einmal für alle zusammen nach. Bei
# Zustimmung sichert es zuerst den jeweils aktuellen Stand unter einem
# neuen, eigenen Zeitstempel (den es am Ende ausgibt, damit auch der
# Rückbau selbst rückgängig gemacht werden kann), bevor es per
# Zwischendatei und atomarem mv zurückspielt; die Rechte der Zieldatei
# bleiben erhalten (chmod --reference). Für "existierte vor dem Anwenden
# nicht" verschiebt es die Datei auf ihren eigenen Sicherungsnamen, statt
# sie zu löschen. rueckbau bricht ohne jede Änderung ab, wenn zu
# <Zeitstempel> gar keine Sicherung existiert oder eine vorhandene
# Sicherung leer oder (bei JSON-Zielen) kein gültiges JSON ist.
#
# Laufende agy-Hubs schreiben eine soeben geänderte agy-settings.json sonst
# zurück (gemessen 2026-09-30). Das Skript tötet dafür keinen Prozess selbst
# — ein pkill-Muster wie "agy --hub" kann auch Hubs anderer, unbeteiligter
# Projekte treffen, und ein getöteter Prozess lässt sich über rueckbau nicht
# rückgängig machen. Es zeigt bei "agy" und "rueckbau" stattdessen VOR jeder
# Rückfrage einen Hinweis inklusive der aktuell laufenden Treffer, und der
# Mensch entscheidet und beendet gezielt, bevor er die Diff-Frage mit "j"
# beantwortet.

set -euo pipefail

case "${BASH_SOURCE[0]}" in
  */*) script_dir=$(cd "${BASH_SOURCE[0]%/*}" && pwd -P) ;;
  *) script_dir=$(pwd -P) ;;
esac

# Wurzel des Checkouts, in dem dieses Skript liegt (setup/vorlagen liegt
# zwei Ebenen darunter) — dient nur dazu, ein Ziel zu erkennen, das (per
# Symlink) in genau diesen Checkout zeigt, siehe pruefe_nicht_checkout.
# pwd -P statt readlink -f auf demselben Wert: beide müssen kanonisch
# aufgelöst sein, sonst würde der Vergleich auf diesem Rechner nie treffen
# (readlink -f liefert hier /var/home/…, ein rohes pwd nur /home/…).
repo_root=$(cd "$script_dir/../.." && pwd -P)

# Zwischendateien: global statt "local", damit die eine EXIT-Falle unten
# sie in jeder Funktion und bei jedem Abbruch findet — eine lokale Variable
# ist für die Falle schon verschwunden, sobald die Funktion zurückgekehrt
# ist, ein Signal aber jederzeit mitten in einer Funktion zuschlagen kann.
tmp=""
statusline_tmp=""
rueckbau_tmp=""
SICHERN_BAK=""

aufraeumen() {
  rm -f "${tmp:-}" "${statusline_tmp:-}" "${rueckbau_tmp:-}"
}
trap aufraeumen EXIT
# Zusätzlich zur EXIT-Falle: HUP/INT/TERM räumen explizit auf und beenden
# mit dem für das Signal üblichen Exitcode, statt sich nur auf das
# (versionsabhängige) Verhalten von bash beim Beenden durch ein nicht
# selbst gefangenes Signal zu verlassen.
signal_abbruch() {
  aufraeumen
  trap - EXIT HUP INT TERM
  exit "$1"
}
trap 'signal_abbruch 129' HUP
trap 'signal_abbruch 130' INT
trap 'signal_abbruch 143' TERM

die() {
  echo "rechte-anwenden.sh: $*" >&2
  exit 1
}

need() {
  local prog
  for prog in "$@"; do
    if ! command -v "$prog" >/dev/null 2>&1; then
      die "benötigt '$prog', aber nicht im PATH gefunden"
    fi
  done
}

need_apply_tools() {
  need jq envsubst
  need mktemp readlink chmod diff cp mv grep install cmp sha256sum
}

# Bei einem Symlink wird auf die Datei geschrieben, auf die er zeigt
# (readlink -f); der Symlink selbst bleibt unverändert. Kein Symlink: der
# Pfad bleibt wie er ist.
resolve_ziel() {
  local ziel="$1"
  if [ -L "$ziel" ]; then
    readlink -f "$ziel"
  else
    printf '%s\n' "$ziel"
  fi
}

# pruefe_nicht_checkout <aufgelöster Pfad>: bricht ab, wenn der Pfad (nach
# Symlink-Auflösung) in den Checkout dieses Skripts selbst zeigt. Ein Ziel
# in ${HOME} kann per Symlink versehentlich (oder über einen alten Testlauf)
# auf eine Datei im eigenen Repository zeigen — dieses Skript würde dann
# eine Sicherung und einen mv/install-Schritt gegen eine versionierte Datei
# im Checkout ausführen, statt gegen die persönliche Konfiguration.
pruefe_nicht_checkout() {
  local resolved="$1"
  case "$resolved" in
    "$repo_root"/*)
      die "Ziel zeigt (per Symlink) in den eigenen Checkout, nicht in \$HOME: $resolved"
      ;;
  esac
}

hub_hinweis() {
  echo "Hinweis: laufende agy-Hubs schreiben diese Datei sonst zurück." >&2
  echo "Vor dem Anwenden/Rückbau prüfen und bei Bedarf gezielt beenden:" >&2
  echo "  pgrep -af -- '--hub'" >&2
  echo "  pkill -f 'agy --hub'" >&2
  if command -v pgrep >/dev/null 2>&1; then
    if pgrep -af -- '--hub' >/dev/null 2>&1; then
      echo "Aktuell laufend:" >&2
      pgrep -af -- '--hub' >&2 || true
    fi
  fi
}

# zeige_diff <alt> <neu>: druckt den Diff und gibt 0 zurück, wenn sich
# beide unterscheiden; gibt 1 zurück (mit einer Meldung statt eines Diffs),
# wenn sie gleich sind. Fragt selbst nichts ab — das übernimmt
# bestaetigen_oder_abbrechen separat, damit mehrere Ziele in einem Aufruf
# (z. B. agy: settings.json und statusline.py) erst alle ihre Diffs zeigen
# können und dann mit einer einzigen Rückfrage bestätigt werden.
zeige_diff() {
  local alt="$1" neu="$2"
  local status=0
  set +e
  diff -u "$alt" "$neu"
  status=$?
  set -e
  if [ "$status" -eq 0 ]; then
    echo "Keine Änderung nötig — $alt entspricht bereits der Vorlage."
    return 1
  fi
  return 0
}

bestaetigen_oder_abbrechen() {
  local ans=""
  read -r -p "Anwenden? [j/N]" ans || ans=""
  if [ "$ans" != "j" ] && [ "$ans" != "J" ]; then
    echo "Abgebrochen ohne Änderung." >&2
    exit 1
  fi
}

# sichern <aufgelöster Zielpfad> <Zeitstempel>: legt eine Sicherung an und
# setzt bei Erfolg die globale Variable SICHERN_BAK auf ihren Pfad. Nicht
# über stdout ("bak=$(sichern …)"), weil eine Zuweisung aus einer
# Command-Substitution einen darin fehlschlagenden Schritt nicht unter
# "set -e" abbricht — jeder Schritt hier bekommt stattdessen ein
# ausdrückliches "|| die". cmp -s bestätigt die Sicherung gegen das
# Original, bevor der Aufrufer irgendetwas überschreibt.
sichern() {
  local resolved="$1" ts="$2"
  local bak="${resolved}.bak-${ts}"
  if [ -e "$bak" ]; then
    die "Sicherung existiert schon: $bak"
  fi
  if ! cp -p -n "$resolved" "$bak"; then
    # cp kann trotz Fehler schon eine unvollständige Datei angelegt haben
    # (gemessen mit einem Stub-cp, der "kein Platz mehr" simuliert); da
    # $bak unmittelbar zuvor nachweislich nicht existierte, ist ihre
    # Entfernung hier sicher — rueckbau könnte sonst auf eine leere
    # Sicherung zurückfallen.
    rm -f "$bak"
    die "Sicherung fehlgeschlagen: $bak"
  fi
  cmp -s "$resolved" "$bak" || die "Sicherung weicht vom Original ab: $bak"
  SICHERN_BAK="$bak"
}

cmd_claude() {
  need_apply_tools

  local vorlage="$script_dir/claude-rechte.json"
  if [ ! -f "$vorlage" ]; then
    die "Vorlage fehlt: $vorlage"
  fi

  local ziel="${HOME}/.claude/settings.json"
  if [ ! -f "$ziel" ]; then
    die "Zieldatei fehlt: $ziel"
  fi
  if ! jq -e . "$ziel" >/dev/null 2>&1; then
    die "Zieldatei ist kein gültiges JSON: $ziel"
  fi

  local resolved
  resolved=$(resolve_ziel "$ziel")
  pruefe_nicht_checkout "$resolved"

  tmp=$(mktemp "${resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"

  # sha256sum direkt beim Merge (vor jeder Rückfrage) festgehalten, damit
  # eine Änderung an $ziel während der Rückfrage — auch eine, die genau
  # während des "diff" oberhalb der Rückfrage passiert — unten erkannt wird.
  local vor_sha
  vor_sha=$(sha256sum "$resolved") || die "sha256sum fehlgeschlagen: $resolved"

  jq -s '.[0] * .[1]' "$resolved" "$vorlage" > "$tmp" || die "jq-Merge fehlgeschlagen"
  if ! jq -e '.permissions.deny|length>0' "$tmp" >/dev/null 2>&1; then
    die "Plausibilitätsprüfung fehlgeschlagen: permissions.deny ist nach dem Merge leer"
  fi

  echo "Merge von $vorlage in $ziel (jq -s '.[0] * .[1]'; Listen wie" \
       "permissions.allow/permissions.deny werden dabei vollständig durch" \
       "die Vorlage ersetzt, nicht mit dem Bestand vereinigt):"
  if zeige_diff "$resolved" "$tmp"; then
    bestaetigen_oder_abbrechen
  else
    exit 0
  fi

  local nach_sha
  nach_sha=$(sha256sum "$resolved") || die "sha256sum fehlgeschlagen: $resolved"
  if [ "$nach_sha" != "$vor_sha" ]; then
    die "$resolved wurde während der Rückfrage geändert — abgebrochen ohne Änderung"
  fi

  local ts bak
  ts="$(date +%Y%m%d-%H%M%S)"
  sichern "$resolved" "$ts"
  bak="$SICHERN_BAK"
  chmod --reference="$resolved" "$tmp" || die "chmod fehlgeschlagen: $tmp"
  mv "$tmp" "$resolved" || die "mv fehlgeschlagen: $resolved"
  tmp=""
  echo "Angewendet. Sicherung: $bak"
  echo "Zeitstempel für rueckbau: $ts"
}

cmd_agy() {
  need_apply_tools

  local vorlage="$script_dir/agy-settings.json"
  if [ ! -f "$vorlage" ]; then
    die "Vorlage fehlt: $vorlage"
  fi

  local ziel="${HOME}/.gemini/antigravity-cli/settings.json"
  if [ ! -f "$ziel" ]; then
    die "Zieldatei fehlt: $ziel"
  fi
  if ! jq -e . "$ziel" >/dev/null 2>&1; then
    die "Zieldatei ist kein gültiges JSON: $ziel"
  fi

  : "${PROJEKTE:?PROJEKTE muss gesetzt sein}"
  : "${TRUSTED_WORKSPACE:?TRUSTED_WORKSPACE muss gesetzt sein}"

  local resolved
  resolved=$(resolve_ziel "$ziel")
  pruefe_nicht_checkout "$resolved"

  local statusline_quelle="$script_dir/statusline.py"
  if [ ! -f "$statusline_quelle" ]; then
    die "Vorlage fehlt: $statusline_quelle"
  fi
  local statusline_ziel="${HOME}/.gemini/antigravity-cli/statusline.py"
  local statusline_resolved
  statusline_resolved=$(resolve_ziel "$statusline_ziel")
  # Vorbedingungen für statusline.py werden hier geprüft, VOR jedem
  # Schreiben — insbesondere vor dem mv der settings.json unten: ein
  # Symlink von statusline.py in den eigenen Checkout darf keinen schon
  # geänderten settings.json-Stand zurücklassen.
  pruefe_nicht_checkout "$statusline_resolved"

  hub_hinweis

  tmp=$(mktemp "${resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"

  local vor_sha
  vor_sha=$(sha256sum "$resolved") || die "sha256sum fehlgeschlagen: $resolved"

  # shellcheck disable=SC2016
  HOME="$HOME" PROJEKTE="$PROJEKTE" TRUSTED_WORKSPACE="$TRUSTED_WORKSPACE" \
    envsubst '${HOME} ${PROJEKTE} ${TRUSTED_WORKSPACE}' \
    < "$vorlage" > "$tmp" || die "envsubst fehlgeschlagen"

  if ! jq -e . "$tmp" >/dev/null 2>&1; then
    die "envsubst-Ergebnis ist kein gültiges JSON — PROJEKTE/TRUSTED_WORKSPACE prüfen"
  fi
  if grep -qF '${' "$tmp"; then
    die "unersetztes \${ im Ergebnis — PROJEKTE/TRUSTED_WORKSPACE prüfen"
  fi

  local settings_frage=0 statusline_frage=0

  echo "Ersetzen von $ziel durch $vorlage (envsubst PROJEKTE/TRUSTED_WORKSPACE):"
  if zeige_diff "$resolved" "$tmp"; then
    settings_frage=1
  fi

  # statusline.py unabhängig von settings.json behandelt: fehlt sie, wird
  # sie unten ohne Rückfrage installiert (nichts wird dabei überschrieben).
  # Existiert sie schon, zeigt das Skript auch dafür einen eigenen Diff.
  if [ -e "$statusline_resolved" ]; then
    echo "Ersetzen von $statusline_resolved durch $statusline_quelle:"
    if zeige_diff "$statusline_resolved" "$statusline_quelle"; then
      statusline_frage=1
    fi
  fi

  # Eine einzige Rückfrage für alles, was in diesem Aufruf ansteht — vor
  # jeder Änderung: ein "N" darf keine schon geschriebene erste Änderung
  # zurücklassen, wenn beide Ziele etwas zu tun hätten.
  if [ "$settings_frage" -eq 1 ] || [ "$statusline_frage" -eq 1 ]; then
    bestaetigen_oder_abbrechen
  fi

  if [ "$settings_frage" -eq 1 ]; then
    local nach_sha
    nach_sha=$(sha256sum "$resolved") || die "sha256sum fehlgeschlagen: $resolved"
    if [ "$nach_sha" != "$vor_sha" ]; then
      die "$resolved wurde während der Rückfrage geändert — abgebrochen ohne Änderung"
    fi
  fi

  local ts
  ts="$(date +%Y%m%d-%H%M%S)"
  if [ "$settings_frage" -eq 1 ] && [ -e "${resolved}.bak-${ts}" ]; then
    die "Sicherung existiert schon: ${resolved}.bak-${ts}"
  fi
  if [ "$statusline_frage" -eq 1 ] && [ -e "${statusline_resolved}.bak-${ts}" ]; then
    die "Sicherung existiert schon: ${statusline_resolved}.bak-${ts}"
  fi

  local ausgegeben_ts=0

  if [ "$settings_frage" -eq 1 ]; then
    local bak
    sichern "$resolved" "$ts"
    bak="$SICHERN_BAK"
    chmod --reference="$resolved" "$tmp" || die "chmod fehlgeschlagen: $tmp"
    mv "$tmp" "$resolved" || die "mv fehlgeschlagen: $resolved"
    tmp=""
    echo "Angewendet. Sicherung: $bak"
    echo "Zeitstempel für rueckbau: $ts"
    ausgegeben_ts=1
  fi

  if [ ! -e "$statusline_resolved" ]; then
    install -m 0755 "$statusline_quelle" "$statusline_resolved" || die "install fehlgeschlagen: $statusline_resolved"
    : > "${statusline_resolved}.installed-${ts}" || die "Marker fehlgeschlagen: ${statusline_resolved}.installed-${ts}"
    echo "Installiert (fehlte): $statusline_resolved"
    [ "$ausgegeben_ts" -eq 1 ] || { echo "Zeitstempel für rueckbau: $ts"; ausgegeben_ts=1; }
  elif [ "$statusline_frage" -eq 1 ]; then
    local sbak
    sichern "$statusline_resolved" "$ts"
    sbak="$SICHERN_BAK"
    statusline_tmp=$(mktemp "${statusline_resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
    install -m 0755 "$statusline_quelle" "$statusline_tmp" || die "install fehlgeschlagen: $statusline_tmp"
    mv "$statusline_tmp" "$statusline_resolved" || die "mv fehlgeschlagen: $statusline_resolved"
    statusline_tmp=""
    echo "Angewendet. Sicherung: $sbak"
    [ "$ausgegeben_ts" -eq 1 ] || { echo "Zeitstempel für rueckbau: $ts"; ausgegeben_ts=1; }
  fi
}

cmd_rueckbau() {
  need cp readlink jq mktemp chmod diff mv

  local ts="${1:?Zeitstempel fehlt (rechte-anwenden.sh rueckbau <Zeitstempel>)}"

  hub_hinweis

  local claude_resolved claude_bak
  claude_resolved=$(resolve_ziel "${HOME}/.claude/settings.json")
  claude_bak="${claude_resolved}.bak-${ts}"

  local agy_resolved agy_bak
  agy_resolved=$(resolve_ziel "${HOME}/.gemini/antigravity-cli/settings.json")
  agy_bak="${agy_resolved}.bak-${ts}"

  local statusline_resolved statusline_bak statusline_marker
  statusline_resolved=$(resolve_ziel "${HOME}/.gemini/antigravity-cli/statusline.py")
  statusline_bak="${statusline_resolved}.bak-${ts}"
  statusline_marker="${statusline_resolved}.installed-${ts}"

  local restore_claude=0 restore_agy=0 restore_statusline=0 remove_statusline=0

  # Jede Sicherung wird geprüft, BEVOR irgendetwas angefasst wird: leer
  # oder (bei den JSON-Zielen) kein gültiges JSON zählt nicht als
  # Sicherung, sondern bricht den ganzen Rückbau ohne jede Änderung ab.
  if [ -f "$claude_bak" ]; then
    [ -s "$claude_bak" ] || die "Sicherung ist leer, kein Rückbau: $claude_bak"
    jq -e . "$claude_bak" >/dev/null 2>&1 || die "Sicherung ist kein gültiges JSON, kein Rückbau: $claude_bak"
    restore_claude=1
  fi
  if [ -f "$agy_bak" ]; then
    [ -s "$agy_bak" ] || die "Sicherung ist leer, kein Rückbau: $agy_bak"
    jq -e . "$agy_bak" >/dev/null 2>&1 || die "Sicherung ist kein gültiges JSON, kein Rückbau: $agy_bak"
    restore_agy=1
  fi
  if [ -f "$statusline_bak" ]; then
    [ -s "$statusline_bak" ] || die "Sicherung ist leer, kein Rückbau: $statusline_bak"
    restore_statusline=1
  elif [ -e "$statusline_marker" ]; then
    # Kein Sicherung zu <ts>, aber der Marker aus einer Neuinstallation:
    # statusline.py existierte vor jenem Lauf nicht.
    remove_statusline=1
  fi

  if [ "$restore_claude" -eq 0 ] && [ "$restore_agy" -eq 0 ] && \
     [ "$restore_statusline" -eq 0 ] && [ "$remove_statusline" -eq 0 ]; then
    die "keine Sicherung zu Zeitstempel $ts gefunden ($claude_bak, $agy_bak, $statusline_bak)"
  fi

  echo "Rückbau zu Zeitstempel $ts:"
  if [ "$restore_claude" -eq 1 ]; then
    echo "-- $claude_resolved:"
    diff -u "$claude_resolved" "$claude_bak" || true
  fi
  if [ "$restore_agy" -eq 1 ]; then
    echo "-- $agy_resolved:"
    diff -u "$agy_resolved" "$agy_bak" || true
  fi
  if [ "$restore_statusline" -eq 1 ]; then
    echo "-- $statusline_resolved:"
    diff -u "$statusline_resolved" "$statusline_bak" || true
  fi
  if [ "$remove_statusline" -eq 1 ]; then
    echo "-- $statusline_resolved wird entfernt (existierte vor dem zugehörigen Anwenden nicht)"
  fi

  bestaetigen_oder_abbrechen

  # Der aktuelle Stand wird zuerst gesichert, unter einem eigenen, neuen
  # Zeitstempel — auch dieser Rückbau soll sich per rueckbau rückgängig
  # machen lassen. Ein sekundengenauer Zeitstempel kann mit dem gerade
  # zurückgespielten <ts> zusammenfallen, wenn claude/agy und dieser
  # rueckbau in derselben Sekunde laufen (z. B. in einem Testlauf) — sonst
  # würde die eigene Sicherung ausgerechnet die Datei träfe, aus der gerade
  # zurückgespielt wird. Die Schleife wartet in diesem Fall auf die nächste
  # Sekunde, statt mit "Sicherung existiert schon" abzubrechen.
  local rueck_ts
  rueck_ts="$(date +%Y%m%d-%H%M%S)"
  while [ "$rueck_ts" = "$ts" ]; do
    sleep 1
    rueck_ts="$(date +%Y%m%d-%H%M%S)"
  done

  if [ "$restore_claude" -eq 1 ]; then
    sichern "$claude_resolved" "$rueck_ts"
    rueckbau_tmp=$(mktemp "${claude_resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
    cp -p "$claude_bak" "$rueckbau_tmp" || die "cp fehlgeschlagen: $claude_bak"
    chmod --reference="$claude_resolved" "$rueckbau_tmp" || die "chmod fehlgeschlagen: $rueckbau_tmp"
    mv "$rueckbau_tmp" "$claude_resolved" || die "mv fehlgeschlagen: $claude_resolved"
    rueckbau_tmp=""
    echo "Zurückgespielt: $claude_resolved"
  fi

  if [ "$restore_agy" -eq 1 ]; then
    sichern "$agy_resolved" "$rueck_ts"
    rueckbau_tmp=$(mktemp "${agy_resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
    cp -p "$agy_bak" "$rueckbau_tmp" || die "cp fehlgeschlagen: $agy_bak"
    chmod --reference="$agy_resolved" "$rueckbau_tmp" || die "chmod fehlgeschlagen: $rueckbau_tmp"
    mv "$rueckbau_tmp" "$agy_resolved" || die "mv fehlgeschlagen: $agy_resolved"
    rueckbau_tmp=""
    echo "Zurückgespielt: $agy_resolved"
  fi

  if [ "$restore_statusline" -eq 1 ]; then
    rueckbau_tmp=$(mktemp "${statusline_resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
    cp -p "$statusline_bak" "$rueckbau_tmp" || die "cp fehlgeschlagen: $statusline_bak"
    if [ -e "$statusline_resolved" ]; then
      sichern "$statusline_resolved" "$rueck_ts"
      chmod --reference="$statusline_resolved" "$rueckbau_tmp" || die "chmod fehlgeschlagen: $rueckbau_tmp"
    else
      chmod 0755 "$rueckbau_tmp" || die "chmod fehlgeschlagen: $rueckbau_tmp"
    fi
    mv "$rueckbau_tmp" "$statusline_resolved" || die "mv fehlgeschlagen: $statusline_resolved"
    rueckbau_tmp=""
    echo "Zurückgespielt: $statusline_resolved"
  elif [ "$remove_statusline" -eq 1 ]; then
    if [ -e "$statusline_resolved" ]; then
      local entfernt_bak="${statusline_resolved}.bak-${rueck_ts}"
      if [ -e "$entfernt_bak" ]; then
        die "Sicherung existiert schon: $entfernt_bak"
      fi
      mv "$statusline_resolved" "$entfernt_bak" || die "mv fehlgeschlagen: $entfernt_bak"
      echo "Entfernt (als Sicherung verschoben nach $entfernt_bak): $statusline_resolved"
    else
      echo "Nichts zu entfernen (existiert nicht): $statusline_resolved"
    fi
  fi

  echo "Zeitstempel für einen Rückbau dieses Rückbaus: $rueck_ts"
}

main() {
  local cmd="${1:-}"
  case "$cmd" in
    claude)
      cmd_claude
      ;;
    agy)
      cmd_agy
      ;;
    rueckbau)
      cmd_rueckbau "${2:-}"
      ;;
    *)
      echo "Nutzung: $0 claude|agy|rueckbau <Zeitstempel>" >&2
      exit 2
      ;;
  esac
}

main "$@"
