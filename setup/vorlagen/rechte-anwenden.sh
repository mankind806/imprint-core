#!/usr/bin/env bash
# rechte-anwenden.sh — wendet die geprüften Rechte-Vorlagen für Claude Code
# und agy an, oder macht eine Anwendung rückgängig. Nur der Mensch führt
# dieses Skript aus (Nutzerentscheid CL-119/CL-123) — kein Werkzeug in
# diesem Repository ruft es automatisch auf.
#
# Aufruf:
#   setup/vorlagen/rechte-anwenden.sh claude
#   setup/vorlagen/rechte-anwenden.sh agy
#   setup/vorlagen/rechte-anwenden.sh agy-statusline
#   setup/vorlagen/rechte-anwenden.sh rueckbau <Zeitstempel>
#
# Die drei Anwenden-Unterbefehle sind die Aufrufe, die "imprint-dev setup
# --apply" für die drei rechte:true-Einträge in setup/inventar.json druckt
# (claude-rechte-vorlage, agy-rechte-vorlage, agy-statusline — der Aufruf ist
# jeweils die id ohne die Endung "-rechte-vorlage"); --apply schreibt so ein
# Ziel nie selbst, siehe setup/vorlagen/README.md.
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
# ${HOME}/.gemini/antigravity-cli/statusline.py) unabhängig davon, über
# dieselbe Prüfung/Diff/Rückfrage-Logik wie der eigenständige Unterbefehl
# agy-statusline (siehe unten): eine Vorlage bestätigt die andere nicht mit.
# Beide Vorbedingungen (u. a., dass keines der beiden Ziele per Symlink in
# den eigenen Checkout dieses Skripts zeigt, und dass beide Ziele reguläre,
# beschreibbare Dateien bzw. Verzeichnisse sind) werden geprüft, bevor
# irgendetwas geschrieben wird, insbesondere vor einem mv der settings.json
# — eine kaputte statusline.py-Vorbedingung darf keinen schon geänderten
# settings.json-Stand zurücklassen.
#
# agy-statusline installiert nur setup/vorlagen/statusline.py nach
# ${HOME}/.gemini/antigravity-cli/statusline.py, mit denselben Prüfungen wie
# der statusline.py-Teil von agy: fehlt die Zieldatei, zeigt das Skript den
# ganzen neuen Inhalt als Diff gegen /dev/null und fragt trotzdem nach —
# eine neu zu installierende Datei wird nicht mehr stillschweigend anwendet
# (Leitungsentscheid, Runde 6); existiert sie schon und weicht ab, zeigt es
# den gewohnten Diff. Beide Fälle bekommen dieselbe sha256sum-Race-Prüfung
# zwischen Rückfrage und Schreiben wie claude/agy.
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
# Sicherung leer oder (bei JSON-Zielen) kein gültiges JSON mit genau einem
# Dokument ist (jq -s 'length==1', wie bei claude/agy). Wie claude/agy hält
# rueckbau den sha256sum jedes tatsächlich zurückzuspielenden Ziels schon
# beim Diff fest und prüft ihn nach der Rückfrage erneut, bevor es etwas
# schreibt; und wie claude/agy bricht es ab, wenn ein Ziel (nach
# Symlink-Auflösung) in den eigenen Checkout zeigt oder keine reguläre,
# beschreibbare Datei ist.
#
# Kamen claude und agy aus zwei getrennten Aufrufen mit UNTERSCHIEDLICHEN
# Zeitstempeln (möglich, wenn sie nicht in derselben Sekunde liefen) und
# sollen beide zurückgespielt werden: den JÜNGEREN (späteren) Zeitstempel
# zuerst zurückspielen, den älteren danach. rueckbau legt bei jedem Aufruf
# selbst eine neue Sicherung des gerade aktuellen Standes an; in der
# falschen Reihenfolge (älterer zuerst) könnte diese eigene, in Echtzeit
# gebildete Sicherung zufällig genau auf den noch ausstehenden jüngeren
# Zeitstempel fallen und von jenem zweiten Aufruf fälschlich als dessen
# Sicherung gelesen werden (gemessen in rechte-anwenden-test.sh, T6).
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

# Löst den GANZEN Zielpfad kanonisch auf (readlink -f), nicht nur den Fall,
# dass die Zieldatei selbst ein Symlink ist — ein Zwischenverzeichnis wie
# ~/.claude kann ebenso gut ein Symlink sein (z. B. in eine Kopie dieses
# Checkouts), ohne dass die Datei darin selbst ein Symlink ist; ein Test nur
# auf "[ -L "$ziel" ]" würde diesen Fall übersehen. Der Aufrufer prüft den
# zurückgegebenen Pfad erst danach mit pruefe_nicht_checkout — nie vorher.
# readlink -f schlägt fehl, wenn ein Verzeichnis AUF DEM WEG zum Ziel (nicht
# nur die Zieldatei selbst) noch gar nicht existiert (z. B. ~/.gemini fehlt
# komplett, weil agy nie angewendet wurde); in dem Fall gibt es nichts zum
# Auflösen und damit auch keinen Symlink, über den ein Angriff liefe — der
# unveränderte, unaufgelöste Pfad ist dann sicher genug, um ihn weiterzugeben
# (der Aufrufer erkennt "existiert nicht" ohnehin selbst über [ -e/-f ]).
resolve_ziel() {
  local ziel="$1" resolved
  if resolved=$(readlink -f "$ziel" 2>/dev/null); then
    printf '%s\n' "$resolved"
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

# pruefe_ziel_ok <aufgelöster Pfad>: bricht ab, wenn eine schon vorhandene
# Zieldatei keine reguläre Datei ist (z. B. ein Verzeichnis, FIFO oder
# Gerätedatei) oder nicht beschreibbar ist — beides würde sichern/mv bzw.
# install erst mitten in der Anwendung scheitern lassen, nachdem für einen
# anderen Eintrag desselben Aufrufs (z. B. settings.json bei "agy") schon
# etwas geschrieben wurde. Fehlt die Zieldatei noch (z. B. eine frisch zu
# installierende statusline.py), muss stattdessen ihr Zielverzeichnis
# existieren und beschreibbar sein.
pruefe_ziel_ok() {
  local resolved="$1"
  if [ -e "$resolved" ]; then
    if [ ! -f "$resolved" ]; then
      die "Ziel ist keine reguläre Datei: $resolved"
    fi
    if [ ! -w "$resolved" ]; then
      die "Ziel ist nicht beschreibbar: $resolved"
    fi
  else
    local verz="${resolved%/*}"
    if [ ! -d "$verz" ] || [ ! -w "$verz" ]; then
      die "Zielverzeichnis fehlt oder ist nicht beschreibbar: $verz"
    fi
  fi
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

need_statusline_tools() {
  need mktemp readlink chmod diff cp mv install cmp sha256sum
}

# Geteilte statusline.py-Logik für "agy" und den eigenständigen Unterbefehl
# "agy-statusline" — eine Vorlage, ein Codepfad, damit beide nie
# auseinanderlaufen. Ergebnisse liegen in den globalen Variablen
# STATUSLINE_QUELLE/STATUSLINE_RESOLVED/STATUSLINE_EXISTIERTE/STATUSLINE_VOR_SHA.
STATUSLINE_QUELLE=""
STATUSLINE_RESOLVED=""
STATUSLINE_EXISTIERTE=0
STATUSLINE_VOR_SHA=""

# statusline_vorbereiten: löst Quelle/Ziel auf und prüft beide
# Vorbedingungen (nicht der eigene Checkout, reguläre/beschreibbare Datei
# bzw. beschreibbares Zielverzeichnis) — VOR jeder Änderung, auch vor einem
# mv der settings.json in "agy".
statusline_vorbereiten() {
  STATUSLINE_QUELLE="$script_dir/statusline.py"
  if [ ! -f "$STATUSLINE_QUELLE" ]; then
    die "Vorlage fehlt: $STATUSLINE_QUELLE"
  fi
  STATUSLINE_RESOLVED=$(resolve_ziel "${HOME}/.gemini/antigravity-cli/statusline.py")
  pruefe_nicht_checkout "$STATUSLINE_RESOLVED"
  pruefe_ziel_ok "$STATUSLINE_RESOLVED"
  if [ -e "$STATUSLINE_RESOLVED" ]; then
    STATUSLINE_EXISTIERTE=1
    STATUSLINE_VOR_SHA=$(sha256sum "$STATUSLINE_RESOLVED") || die "sha256sum fehlgeschlagen: $STATUSLINE_RESOLVED"
  else
    STATUSLINE_EXISTIERTE=0
    STATUSLINE_VOR_SHA=""
  fi
}

# statusline_diff_zeigen: zeigt Diff bzw. (Datei fehlt) den ganzen neuen
# Inhalt als Diff gegen /dev/null und gibt 0 zurück, wenn eine Rückfrage
# nötig ist. Eine fehlende Datei braucht seit Runde 6 IMMER eine Rückfrage
# (Leitungsentscheid) — sie wird nicht mehr stillschweigend installiert.
statusline_diff_zeigen() {
  if [ "$STATUSLINE_EXISTIERTE" -eq 1 ]; then
    echo "Ersetzen von $STATUSLINE_RESOLVED durch $STATUSLINE_QUELLE:"
    zeige_diff "$STATUSLINE_RESOLVED" "$STATUSLINE_QUELLE"
    return $?
  fi
  echo "Neu installieren: $STATUSLINE_RESOLVED (aus $STATUSLINE_QUELLE, Datei fehlt bisher):"
  diff -u /dev/null "$STATUSLINE_QUELLE" || true
  return 0
}

# statusline_race_pruefen: nach der Rückfrage, vor jedem Schreiben erneut
# geprüft — dieselbe sha256sum-Race-Prüfung wie bei settings.json in
# claude/agy, auf beide Richtungen: eine Datei, die während der Rückfrage
# geändert wurde, oder eine, die währenddessen neu aufgetaucht ist, obwohl
# sie vorher fehlte.
statusline_race_pruefen() {
  if [ "$STATUSLINE_EXISTIERTE" -eq 1 ]; then
    if [ ! -e "$STATUSLINE_RESOLVED" ]; then
      die "$STATUSLINE_RESOLVED wurde während der Rückfrage entfernt — abgebrochen ohne Änderung"
    fi
    local nach_sha
    nach_sha=$(sha256sum "$STATUSLINE_RESOLVED") || die "sha256sum fehlgeschlagen: $STATUSLINE_RESOLVED"
    if [ "$nach_sha" != "$STATUSLINE_VOR_SHA" ]; then
      die "$STATUSLINE_RESOLVED wurde während der Rückfrage geändert — abgebrochen ohne Änderung"
    fi
  else
    if [ -e "$STATUSLINE_RESOLVED" ]; then
      die "$STATUSLINE_RESOLVED wurde während der Rückfrage angelegt — abgebrochen ohne Änderung"
    fi
  fi
}

# statusline_schreiben <ts>: sichert (falls vorhanden) und schreibt per
# Zwischendatei und atomarem mv/install; setzt bei einer Neuinstallation den
# Marker für rueckbau.
statusline_schreiben() {
  local ts="$1"
  if [ "$STATUSLINE_EXISTIERTE" -eq 1 ]; then
    local sbak
    sichern "$STATUSLINE_RESOLVED" "$ts"
    sbak="$SICHERN_BAK"
    statusline_tmp=$(mktemp "${STATUSLINE_RESOLVED%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
    install -m 0755 "$STATUSLINE_QUELLE" "$statusline_tmp" || die "install fehlgeschlagen: $statusline_tmp"
    mv "$statusline_tmp" "$STATUSLINE_RESOLVED" || die "mv fehlgeschlagen: $STATUSLINE_RESOLVED"
    statusline_tmp=""
    echo "Angewendet. Sicherung: $sbak"
  else
    if [ -e "${STATUSLINE_RESOLVED}.installed-${ts}" ]; then
      die "Marker existiert schon: ${STATUSLINE_RESOLVED}.installed-${ts}"
    fi
    statusline_tmp=$(mktemp "${STATUSLINE_RESOLVED%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
    install -m 0755 "$STATUSLINE_QUELLE" "$statusline_tmp" || die "install fehlgeschlagen: $statusline_tmp"
    mv "$statusline_tmp" "$STATUSLINE_RESOLVED" || die "mv fehlgeschlagen: $STATUSLINE_RESOLVED"
    statusline_tmp=""
    : > "${STATUSLINE_RESOLVED}.installed-${ts}" || die "Marker fehlgeschlagen: ${STATUSLINE_RESOLVED}.installed-${ts}"
    echo "Installiert (fehlte): $STATUSLINE_RESOLVED"
  fi
}

cmd_agy_statusline() {
  need_statusline_tools
  statusline_vorbereiten

  if statusline_diff_zeigen; then
    bestaetigen_oder_abbrechen
  else
    exit 0
  fi

  statusline_race_pruefen

  local ts
  ts="$(date +%Y%m%d-%H%M%S)"
  statusline_schreiben "$ts"
  echo "Zeitstempel für rueckbau: $ts"
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
  if ! jq -e -s 'length==1' "$ziel" >/dev/null 2>&1; then
    die "Zieldatei ist kein gültiges JSON mit genau einem Dokument: $ziel"
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
  if ! jq -e -s 'length==1' "$ziel" >/dev/null 2>&1; then
    die "Zieldatei ist kein gültiges JSON mit genau einem Dokument: $ziel"
  fi

  : "${PROJEKTE:?PROJEKTE muss gesetzt sein}"
  : "${TRUSTED_WORKSPACE:?TRUSTED_WORKSPACE muss gesetzt sein}"

  local resolved
  resolved=$(resolve_ziel "$ziel")
  pruefe_nicht_checkout "$resolved"
  pruefe_ziel_ok "$resolved"

  # statusline.py-Vorbedingungen (Checkout-Symlink, reguläre/beschreibbare
  # Datei) werden hier geprüft, VOR jedem Schreiben — insbesondere vor dem
  # mv der settings.json unten: eine kaputte statusline.py-Vorbedingung darf
  # keinen schon geänderten settings.json-Stand zurücklassen.
  statusline_vorbereiten

  hub_hinweis

  tmp=$(mktemp "${resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"

  local vor_sha
  vor_sha=$(sha256sum "$resolved") || die "sha256sum fehlgeschlagen: $resolved"

  # shellcheck disable=SC2016
  HOME="$HOME" PROJEKTE="$PROJEKTE" TRUSTED_WORKSPACE="$TRUSTED_WORKSPACE" \
    envsubst '${HOME} ${PROJEKTE} ${TRUSTED_WORKSPACE}' \
    < "$vorlage" > "$tmp" || die "envsubst fehlgeschlagen"

  if ! jq -e -s 'length==1' "$tmp" >/dev/null 2>&1; then
    die "envsubst-Ergebnis ist kein gültiges JSON mit genau einem Dokument — PROJEKTE/TRUSTED_WORKSPACE prüfen"
  fi
  if grep -qF '${' "$tmp"; then
    die "unersetztes \${ im Ergebnis — PROJEKTE/TRUSTED_WORKSPACE prüfen"
  fi

  local settings_frage=0 statusline_frage=0

  echo "Ersetzen von $ziel durch $vorlage (envsubst PROJEKTE/TRUSTED_WORKSPACE):"
  if zeige_diff "$resolved" "$tmp"; then
    settings_frage=1
  fi

  # statusline.py unabhängig von settings.json behandelt, über denselben
  # Codepfad wie der eigenständige Unterbefehl agy-statusline: eine
  # fehlende Datei braucht seit Runde 6 ebenfalls eine Rückfrage.
  if statusline_diff_zeigen; then
    statusline_frage=1
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
  if [ "$statusline_frage" -eq 1 ]; then
    statusline_race_pruefen
  fi

  local ts
  ts="$(date +%Y%m%d-%H%M%S)"
  if [ "$settings_frage" -eq 1 ] && [ -e "${resolved}.bak-${ts}" ]; then
    die "Sicherung existiert schon: ${resolved}.bak-${ts}"
  fi
  if [ "$statusline_frage" -eq 1 ]; then
    if [ "$STATUSLINE_EXISTIERTE" -eq 1 ] && [ -e "${STATUSLINE_RESOLVED}.bak-${ts}" ]; then
      die "Sicherung existiert schon: ${STATUSLINE_RESOLVED}.bak-${ts}"
    fi
    if [ "$STATUSLINE_EXISTIERTE" -eq 0 ] && [ -e "${STATUSLINE_RESOLVED}.installed-${ts}" ]; then
      die "Marker existiert schon: ${STATUSLINE_RESOLVED}.installed-${ts}"
    fi
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

  if [ "$statusline_frage" -eq 1 ]; then
    statusline_schreiben "$ts"
    [ "$ausgegeben_ts" -eq 1 ] || { echo "Zeitstempel für rueckbau: $ts"; ausgegeben_ts=1; }
  fi
}

cmd_rueckbau() {
  need cp readlink jq mktemp chmod diff mv cmp sha256sum

  local ts="${1:?Zeitstempel fehlt (rechte-anwenden.sh rueckbau <Zeitstempel>)}"

  hub_hinweis

  local claude_resolved claude_bak
  claude_resolved=$(resolve_ziel "${HOME}/.claude/settings.json")
  pruefe_nicht_checkout "$claude_resolved"
  claude_bak="${claude_resolved}.bak-${ts}"

  local agy_resolved agy_bak
  agy_resolved=$(resolve_ziel "${HOME}/.gemini/antigravity-cli/settings.json")
  pruefe_nicht_checkout "$agy_resolved"
  agy_bak="${agy_resolved}.bak-${ts}"

  local statusline_resolved statusline_bak statusline_marker
  statusline_resolved=$(resolve_ziel "${HOME}/.gemini/antigravity-cli/statusline.py")
  pruefe_nicht_checkout "$statusline_resolved"
  statusline_bak="${statusline_resolved}.bak-${ts}"
  statusline_marker="${statusline_resolved}.installed-${ts}"

  local restore_claude=0 restore_agy=0 restore_statusline=0 remove_statusline=0

  # Jede Sicherung wird geprüft, BEVOR irgendetwas angefasst wird: leer
  # oder (bei den JSON-Zielen) kein gültiges JSON mit genau einem Dokument
  # zählt nicht als Sicherung, sondern bricht den ganzen Rückbau ohne jede
  # Änderung ab.
  if [ -f "$claude_bak" ]; then
    [ -s "$claude_bak" ] || die "Sicherung ist leer, kein Rückbau: $claude_bak"
    jq -e -s 'length==1' "$claude_bak" >/dev/null 2>&1 || die "Sicherung ist kein gültiges JSON mit genau einem Dokument, kein Rückbau: $claude_bak"
    restore_claude=1
  fi
  if [ -f "$agy_bak" ]; then
    [ -s "$agy_bak" ] || die "Sicherung ist leer, kein Rückbau: $agy_bak"
    jq -e -s 'length==1' "$agy_bak" >/dev/null 2>&1 || die "Sicherung ist kein gültiges JSON mit genau einem Dokument, kein Rückbau: $agy_bak"
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

  # Ziele, die dieser Aufruf tatsächlich zurückspielt, müssen VORAB reguläre,
  # beschreibbare Dateien sein — sonst könnte claude schon zurückgespielt
  # sein, bevor agy an einer unbeschreibbaren Zieldatei scheitert.
  [ "$restore_claude" -eq 1 ] && pruefe_ziel_ok "$claude_resolved"
  [ "$restore_agy" -eq 1 ] && pruefe_ziel_ok "$agy_resolved"

  echo "Rückbau zu Zeitstempel $ts:"
  local claude_vor_sha="" agy_vor_sha="" statusline_vor_sha=""
  if [ "$restore_claude" -eq 1 ]; then
    echo "-- $claude_resolved:"
    diff -u "$claude_resolved" "$claude_bak" || true
    # sha256sum schon jetzt festgehalten (wie bei claude/agy beim Anwenden),
    # damit eine Änderung zwischen dieser Anzeige und dem mv unten erkannt
    # wird, statt eine Sicherung über einen inzwischen veralteten Stand zu
    # legen.
    if [ -e "$claude_resolved" ]; then
      claude_vor_sha=$(sha256sum "$claude_resolved") || die "sha256sum fehlgeschlagen: $claude_resolved"
    fi
  fi
  if [ "$restore_agy" -eq 1 ]; then
    echo "-- $agy_resolved:"
    diff -u "$agy_resolved" "$agy_bak" || true
    if [ -e "$agy_resolved" ]; then
      agy_vor_sha=$(sha256sum "$agy_resolved") || die "sha256sum fehlgeschlagen: $agy_resolved"
    fi
  fi
  if [ "$restore_statusline" -eq 1 ]; then
    echo "-- $statusline_resolved:"
    diff -u "$statusline_resolved" "$statusline_bak" || true
    if [ -e "$statusline_resolved" ]; then
      statusline_vor_sha=$(sha256sum "$statusline_resolved") || die "sha256sum fehlgeschlagen: $statusline_resolved"
    fi
  fi
  if [ "$remove_statusline" -eq 1 ]; then
    echo "-- $statusline_resolved wird entfernt (existierte vor dem zugehörigen Anwenden nicht)"
  fi

  bestaetigen_oder_abbrechen

  if [ "$restore_claude" -eq 1 ]; then
    local claude_nach_sha=""
    if [ -e "$claude_resolved" ]; then
      claude_nach_sha=$(sha256sum "$claude_resolved") || die "sha256sum fehlgeschlagen: $claude_resolved"
    fi
    if [ "$claude_nach_sha" != "$claude_vor_sha" ]; then
      die "$claude_resolved wurde während der Rückfrage geändert — abgebrochen ohne Änderung"
    fi
  fi
  if [ "$restore_agy" -eq 1 ]; then
    local agy_nach_sha=""
    if [ -e "$agy_resolved" ]; then
      agy_nach_sha=$(sha256sum "$agy_resolved") || die "sha256sum fehlgeschlagen: $agy_resolved"
    fi
    if [ "$agy_nach_sha" != "$agy_vor_sha" ]; then
      die "$agy_resolved wurde während der Rückfrage geändert — abgebrochen ohne Änderung"
    fi
  fi
  if [ "$restore_statusline" -eq 1 ]; then
    local statusline_nach_sha=""
    if [ -e "$statusline_resolved" ]; then
      statusline_nach_sha=$(sha256sum "$statusline_resolved") || die "sha256sum fehlgeschlagen: $statusline_resolved"
    fi
    if [ "$statusline_nach_sha" != "$statusline_vor_sha" ]; then
      die "$statusline_resolved wurde während der Rückfrage geändert — abgebrochen ohne Änderung"
    fi
  fi

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
    agy-statusline)
      cmd_agy_statusline
      ;;
    rueckbau)
      cmd_rueckbau "${2:-}"
      ;;
    *)
      echo "Nutzung: $0 claude|agy|agy-statusline|rueckbau <Zeitstempel>" >&2
      exit 2
      ;;
  esac
}

main "$@"
