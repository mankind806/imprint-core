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
# ${HOME}/.claude/settings.json (ein tiefer Merge; permissions.allow und
# permissions.deny werden dabei als VEREINIGUNG aus Bestand und Vorlage
# gebildet — bestehende Einträge zuerst, dann neue aus der Vorlage,
# doppelte entfernt, Reihenfolge stabil (Leitungsentscheid CL-131: Rechte
# bleiben erhalten — kein bestehendes Recht geht beim Anwenden verloren).
# permissions.defaultMode und alle anderen Top-Level-Schlüssel — hooks, env,
# enabledPlugins, sandbox, … — bleiben unverändert, weil die Vorlage nur
# permissions trägt (die Vorlage darf auch nur genau das tragen — jeder
# andere Top-Level- oder permissions-Schlüssel in claude-rechte.json bricht
# den Merge ab, Befund 3 aus Runde 8, gegen eine manipulierte Vorlage, die
# sonst z. B. defaultMode auf einen unsicheren Wert setzen könnte). Nach dem
# Merge prüft das Skript, dass jeder bestehende allow-/deny-Eintrag noch
# vorhanden ist und dass jeder Wert außerhalb von permissions.allow/deny
# denselben Wert hat wie vor dem Merge (jq "==", also gleiche Werte
# JSON-normalisiert, nicht Byte für Byte — Schlüsselreihenfolge oder
# Formatierung zählen nicht) — nicht nur, dass der Schlüssel noch existiert:
# ein reiner Schlüsselvergleich hätte eine Vorlage, die einen bestehenden
# Wert überschreibt, statt ihn wegzulassen, nicht erkannt (Befund 3 aus
# Runde 8) — sonst bricht es ohne jede Änderung ab (Befund 1, Runde 7;
# Befund 3, Runde 8).
# Direkt beim Merge hält das Skript den sha256sum von
# ${HOME}/.claude/settings.json fest und prüft ihn nach der Rückfrage
# erneut, bevor irgendetwas geschrieben wird — ändert sich die Zieldatei
# während der Diff-Rückfrage (z. B. durch Claude Code selbst oder ein
# gleichzeitiges "immer erlauben"), bricht das Skript ohne jede Änderung ab,
# statt eine Sicherung des schon veralteten Standes anzulegen.
#
# agy mergt setup/vorlagen/agy-settings.json (PROJEKTE/TRUSTED_WORKSPACE
# vorab validiert und per jq eingesetzt, nicht envsubst — siehe unten und
# Befund 1, Runde 9/10) in ${HOME}/.gemini/antigravity-cli/settings.json —
# seit Runde 8 genau wie claude: permissions.allow/permissions.deny als
# Vereinigung aus Bestand und Vorlage, alles andere aus der Vorlage
# (statusLine, trustedWorkspaces) ersetzt den Bestand wie bisher, jeder
# andere, schon vorhandene Top-Level-Schlüssel bleibt unverändert (Befund 1,
# Runde 8: ein voller Ersatz hätte eigene deny-Regeln wie "command(make)"
# stillschweigend gelöscht — mehr Rechte für agy, nicht weniger). Die
# Vorlage darf dabei nur permissions/statusLine/trustedWorkspaces tragen
# (sonst bricht der Merge ab), mit derselben sha256sum-Race-Prüfung wie bei
# claude. Unabhängig davon behandelt agy setup/vorlagen/statusline.py (Ziel
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
# bleiben erhalten (kopiere_rechte: chmod --reference plus eine erweiterte
# ACL, falls vorhanden — reines chmod --reference kopiert keine ACL-
# Einträge, Befund 3, Runde 9). Für "existierte vor dem Anwenden
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
diff_json_alt_tmp=""
diff_json_neu_tmp=""
agy_rendered_tmp=""
SICHERN_BAK=""

aufraeumen() {
  rm -f "${tmp:-}" "${statusline_tmp:-}" "${rueckbau_tmp:-}" \
        "${diff_json_alt_tmp:-}" "${diff_json_neu_tmp:-}" "${agy_rendered_tmp:-}"
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
  need jq
  need mktemp readlink chmod diff cp mv grep install cmp sha256sum ls realpath
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
# etwas geschrieben wurde. Geprüft wird IMMER auch das Zielverzeichnis
# (nicht nur bei einer fehlenden Zieldatei, Befund 2 aus Runde 7): sichern()
# legt die Sicherung per cp und mktemp/mv die Zwischendatei im selben
# Verzeichnis an wie die Zieldatei — eine schreibbare Datei in einem nicht
# mehr beschreibbaren Verzeichnis (z. B. nachträglich chmod 555) lässt genau
# diesen Schritt scheitern, nachdem ein anderes Ziel desselben Aufrufs
# schon geschrieben wurde, wenn nur die Datei selbst geprüft wird.
pruefe_ziel_ok() {
  local resolved="$1"
  if [ -e "$resolved" ]; then
    if [ ! -f "$resolved" ]; then
      die "Ziel ist keine reguläre Datei: $resolved"
    fi
    if [ ! -w "$resolved" ]; then
      die "Ziel ist nicht beschreibbar: $resolved"
    fi
  fi
  local verz="${resolved%/*}"
  if [ ! -d "$verz" ] || [ ! -w "$verz" ]; then
    die "Zielverzeichnis fehlt oder ist nicht beschreibbar: $verz"
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

# zeige_diff_json <alt-JSON-Datei> <neu-JSON-Datei> <Anzeigename>: wie
# zeige_diff, aber beide Seiten werden vorher mit "jq -S ." (sortierte
# Schlüssel, eingerückt) in Zwischendateien geschrieben — eine
# einzeilige/kompakte settings.json würde sonst als ein einziger
# geänderter Diff-Block erscheinen, ohne dass einzelne Einträge erkennbar
# sind (Befund 1, Runde 7). Ändert die eigentlichen Dateien nicht, nur die
# Anzeige.
zeige_diff_json() {
  local alt="$1" neu="$2" anzeigename="$3"
  diff_json_alt_tmp=$(mktemp) || die "mktemp fehlgeschlagen"
  diff_json_neu_tmp=$(mktemp) || die "mktemp fehlgeschlagen"
  jq -S . "$alt" > "$diff_json_alt_tmp" || die "jq fehlgeschlagen: $alt"
  jq -S . "$neu" > "$diff_json_neu_tmp" || die "jq fehlgeschlagen: $neu"
  local status=0
  set +e
  diff -u "$diff_json_alt_tmp" "$diff_json_neu_tmp"
  status=$?
  set -e
  rm -f "$diff_json_alt_tmp" "$diff_json_neu_tmp"
  diff_json_alt_tmp=""
  diff_json_neu_tmp=""
  if [ "$status" -eq 0 ]; then
    echo "Keine Änderung nötig — $anzeigename entspricht bereits der Vorlage."
    return 1
  fi
  return 0
}

# pruefe_vorlage_schluessel <Datei> <erlaubte Top-Level-Schlüssel als JSON-Array> <erlaubte permissions-Schlüssel als JSON-Array>:
# bricht ab, wenn die Datei einen Top-Level- oder permissions-Schlüssel
# trägt, der nicht in der jeweils erlaubten Liste steht. Eine manipulierte
# Vorlage (claude-rechte.json/agy-settings.json) könnte sonst z. B.
# defaultMode, hooks, model oder dangerouslySkipPermissions einschmuggeln —
# der Merge vergleicht danach nur noch Schlüssel/Werte innerhalb der hier
# freigegebenen Felder, ein neuer Schlüssel ausserhalb bricht schon hier ab
# (Befund 3, Runde 8).
# JQ_MERGE_UNION: von "jq -s" gegen zwei Dateien (Ziel, Vorlage) verwendetes
# Programm für claude UND agy — permissions.allow/permissions.deny werden
# als Vereinigung gebildet (Bestand zuerst, doppelte entfernt, Reihenfolge
# stabil; permissions.defaultMode & Co. bleiben dabei erhalten, weil nur
# allow/deny gezielt gesetzt werden, nicht der ganze permissions-Block).
# JEDER ANDERE Top-Level-Schlüssel aus der Vorlage (z. B. statusLine,
# trustedWorkspaces bei agy) ERSETZT den Bestand GANZ — bewusst kein
# rekursiver "*"-Merge mehr für diese Schlüssel: der hätte einzelne
# Unter-Schlüssel eines bestehenden statusLine-Objekts überleben lassen,
# obwohl "ersetzen" dokumentiert ist (Befund 2, Runde 9). Ein Schlüssel, der
# nur im Bestand steht, bleibt unverändert. "(... // {})" überall, damit
# eine Zieldatei ohne permissions-Block keinen jq-Fehler statt einer klaren
# Meldung auslöst (Befund 2, Runde 8).
JQ_MERGE_UNION='
  def uniq_stable: reduce .[] as $x ([]; if any(.[]; . == $x) then . else . + [$x] end);
  .[0] as $alt | .[1] as $vorlage |
  (($alt.permissions // {}).allow // []) as $altallow |
  (($alt.permissions // {}).deny // []) as $altdeny |
  (($altallow + (($vorlage.permissions // {}).allow // [])) | uniq_stable) as $allow |
  (($altdeny + (($vorlage.permissions // {}).deny // [])) | uniq_stable) as $deny |
  reduce ($vorlage | to_entries[]) as {key: $k, value: $v} (
    $alt;
    if $k == "permissions" then
      .permissions = (($alt.permissions // {}) + {allow: $allow, deny: $deny})
    else
      .[$k] = $v
    end
  )
'

pruefe_vorlage_schluessel() {
  local datei="$1" top_erlaubt="$2" perm_erlaubt="$3"
  if ! jq -e --argjson top "$top_erlaubt" --argjson perm "$perm_erlaubt" '
        ((keys - $top) | length == 0) and
        ((((.permissions // {}) | keys) - $perm) | length == 0)
      ' "$datei" >/dev/null 2>&1; then
    die "Vorlage trägt einen unerwarteten Schlüssel (erlaubt: $top_erlaubt, permissions nur $perm_erlaubt): $datei"
  fi
}

# pruefe_pfad_zeichensatz <Name> <Wert>: erlaubt NUR A-Z a-z 0-9 . _ / -
# (Positivliste statt Verbotsliste) — der Wert landet unverändert in
# write_file(...)/read_file(...)/trustedWorkspaces-Mustern; eine
# Verbotsliste (nur *, ?, [ wie bis Runde 9) übersieht z. B. | + ( ) { }
# ^ $ (könnten ein Muster erweitern/verändern) und unsichtbare Zeichen wie
# U+202E (Right-to-Left Override, könnte die Anzeige eines Diffs oder
# einer Fehlermeldung verfälschen) — keines davon ist in der Positivliste
# enthalten, die Prüfung muss sie also nicht einzeln kennen (Befund 2,
# Runde 10).
pruefe_pfad_zeichensatz() {
  local name="$1" wert="$2"
  case "$wert" in
    ''|*[!A-Za-z0-9._/-]*)
      die "$name enthält ein Zeichen außerhalb von A-Z a-z 0-9 . _ / - (z. B. Anführungszeichen, Klammern, Pipe, ^, \$, ein Steuerzeichen oder ein unsichtbares Unicode-Zeichen wie U+202E): $wert"
      ;;
  esac
}

# pruefe_agy_pfad <Name> <Wert> <home_kanon>: validiert PROJEKTE/
# TRUSTED_WORKSPACE, bevor sie in die JSON-Vorlage eingesetzt werden, und
# gibt den aufgelösten, kanonischen Pfad aus. <home_kanon> ist das per
# "realpath -e" aufgelöste $HOME — ein Vergleich gegen das rohe $HOME würde
# auf Systemen mit einem symlinked Home-Verzeichnis (z. B. Bluefin/ostree:
# /home -> var/home) danebengehen: $HOME selbst oder ein Vorfahre von
# $HOME, als TRUSTED_WORKSPACE eingetragen, würde dann trotzdem akzeptiert,
# weil der aufgelöste Wert nie byte-gleich mit dem rohen $HOME ist (Befund
# 1, Runde 10).
pruefe_agy_pfad() {
  local name="$1" wert="$2" home_kanon="$3"
  case "$wert" in
    /*) ;;
    *) die "$name muss ein absoluter Pfad sein: $wert" ;;
  esac
  local aufgeloest
  aufgeloest=$(realpath -e "$wert" 2>/dev/null) || die "$name ist kein vorhandener Pfad (realpath -e): $wert"
  pruefe_pfad_zeichensatz "$name" "$aufgeloest"
  if [ "$aufgeloest" = "/" ]; then
    die "$name darf nicht / sein: $wert"
  fi
  if [ "$aufgeloest" = "$home_kanon" ]; then
    die "$name darf nicht \$HOME selbst sein (aufgelöst: $home_kanon): $wert"
  fi
  case "$home_kanon" in
    "$aufgeloest"/*)
      die "$name ist ein Vorfahre von \$HOME (aufgelöst: $home_kanon) und würde \$HOME mit abdecken: $wert"
      ;;
  esac
  printf '%s\n' "$aufgeloest"
}

# render_agy_vorlage <Vorlage> <HOME> <PROJEKTE> <TRUSTED_WORKSPACE>: ersetzt
# ${HOME}/${PROJEKTE}/${TRUSTED_WORKSPACE} in der rohen Vorlage NICHT mit
# envsubst (reine Textersetzung, ohne JSON-Escaping), sondern in jq: split
# auf den wörtlichen Platzhalter, join mit der per "tojson" escapten,
# anführungszeichenlosen Form des Werts. Ein Wert mit einem eingebetteten
# \"...\", der aus dem umgebenden JSON-String ausbrechen und z. B. die
# ganze deny-Liste ersetzen würde, kann so nicht mehr wirken — er landet
# escaped als harmloser Textbestandteil (Befund 1, Runde 9).
render_agy_vorlage() {
  local vorlage="$1" home="$2" projekte="$3" tw="$4"
  jq -nr --rawfile tpl "$vorlage" --arg home "$home" --arg projekte "$projekte" --arg tw "$tw" '
    def esc($v): ($v | tojson | .[1:-1]);
    $tpl
    | split("${HOME}") | join(esc($home))
    | split("${PROJEKTE}") | join(esc($projekte))
    | split("${TRUSTED_WORKSPACE}") | join(esc($tw))
  ' || die "Vorlage konnte nicht gerendert werden: $vorlage"
}

# hat_acl <Datei>: true (rc=0), wenn die Datei eine erweiterte ACL trägt.
# Ist getfacl da, direkt darüber geprüft (benannte user:/group:-Einträge
# jenseits der drei Basis-Einträge) — das ist eindeutig. Ohne getfacl, aber
# mit getfattr: prüft gezielt das Attribut "system.posix_acl_access", das
# nur bei einer echten POSIX-ACL existiert — ein SELinux-Kontext
# ("security.selinux") setzt es nicht. Ohne beide Werkzeuge ersatzweise
# "ls -ld" (das "+" nach den Rechten, POSIX/Linux-Konvention für
# irgendeine erweiterte Sicherheitseigenschaft): das kann unter SELinux
# fälschlich als ACL gelesen werden, wenn nur der SELinux-Kontext den
# Unterschied macht (gemessen: uutils "ls -ld" hängt "+" auch dafür an) —
# nur der letzte, ungenaueste Ausweg, wenn weder getfacl noch getfattr da
# sind (Info-Hinweis, Runde 11).
hat_acl() {
  local datei="$1"
  if command -v getfacl >/dev/null 2>&1; then
    getfacl -c "$datei" 2>/dev/null | grep -Eq '^(user|group):[^:]+:|^mask::'
    return $?
  fi
  if command -v getfattr >/dev/null 2>&1; then
    getfattr -n system.posix_acl_access -- "$datei" >/dev/null 2>&1
    return $?
  fi
  local eintrag
  eintrag=$(ls -ld -- "$datei" 2>/dev/null)
  case "${eintrag%% *}" in
    *+) return 0 ;;
    *) return 1 ;;
  esac
}

# pruefe_acl_werkzeuge <quelle> <zwischendatei>: prüft FRÜH — vor jeder
# Rückfrage und vor jeder Sicherung —, ob kopiere_acl/kopiere_rechte später
# an fehlenden getfacl/setfacl scheitern würde. <zwischendatei> ist die per
# mktemp im selben Verzeichnis wie das Ziel schon angelegte Datei: existiert
# sie schon (Regelfall), zeigt ihre ACL bereits jetzt, ob das Verzeichnis
# eine Default-ACL an neue Dateien vererbt (Befund 4, Runde 10) — ohne
# diese Vorab-Prüfung bräche das Skript erst nach "j" und nach einer schon
# angelegten Sicherung ab, mit einer überzähligen .bak-Datei als Rest
# (Befund 3, Runde 10).
pruefe_acl_werkzeuge() {
  local quelle="$1" zwischendatei="$2"
  if command -v getfacl >/dev/null 2>&1 && command -v setfacl >/dev/null 2>&1; then
    return 0
  fi
  if hat_acl "$quelle"; then
    die "$quelle trägt eine erweiterte ACL, aber getfacl/setfacl fehlen — abgebrochen, bevor irgendetwas geschrieben wird: $quelle"
  fi
  if [ -e "$zwischendatei" ] && hat_acl "$zwischendatei"; then
    die "$zwischendatei hat schon jetzt eine (vermutlich vom Zielverzeichnis per Default-ACL geerbte) ACL, aber setfacl fehlt, um sie zu entfernen: $zwischendatei"
  fi
}

# pruefe_acl_werkzeuge_frueh <aufgelöster Pfad> <Quelle>: wie
# pruefe_acl_werkzeuge, aber mit einer echten, frisch per mktemp im
# selben Verzeichnis angelegten und sofort wieder entfernten Probe-Datei
# statt der (möglicherweise gar nicht vorhandenen) Zieldatei selbst — nur
# eine echte, neu angelegte Datei zeigt zuverlässig, ob das Verzeichnis
# eine Default-ACL an neue Dateien vererbt (Befund 2b, Runde 11: rueckbaus
# eigene, frühere Prüfung fragte nur die Zieldatei selbst, nicht das
# Verzeichnis, und übersah so eine geerbte ACL bis zum tatsächlichen
# mktemp weiter unten — nachdem claude schon zurückgespielt war). Nutzt
# die globale Variable rueckbau_tmp, damit die EXIT-Falle die Probe auch
# bei einem Abbruch mitten in pruefe_acl_werkzeuge aufräumt.
pruefe_acl_werkzeuge_frueh() {
  local resolved="$1" quelle="$2"
  rueckbau_tmp=$(mktemp "${resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
  pruefe_acl_werkzeuge "$quelle" "$rueckbau_tmp"
  rm -f "$rueckbau_tmp"
  rueckbau_tmp=""
}

# kopiere_acl <von> <nach>: überträgt eine erweiterte ACL der Quelle auf
# das Ziel (chmod --reference kopiert nur die klassischen rwx-Bits, keine
# ACL-Einträge) — oder entfernt am Ziel eine ACL, die es nicht von der
# Quelle hat, sondern nur per Default-ACL vom Zielverzeichnis geerbt haben
# kann (mktemp legt die Zwischendatei dort an; ohne dieses "setfacl -b"
# würde eine Datei, deren Quelle keine ACL trägt, nach dem Anwenden
# trotzdem eine tragen, Befund 4, Runde 10). Fehlen getfacl/setfacl in
# einem Fall, den pruefe_acl_werkzeuge (vor der ersten Änderung) nicht
# schon abgefangen hat, bricht die Funktion ebenfalls ab (Befund 3, Runde
# 9).
kopiere_acl() {
  local von="$1" nach="$2"
  if hat_acl "$von"; then
    if command -v getfacl >/dev/null 2>&1 && command -v setfacl >/dev/null 2>&1; then
      getfacl -c "$von" 2>/dev/null | setfacl --set-file=- "$nach" \
        || die "ACL von $von konnte nicht auf $nach übertragen werden"
    else
      die "$von trägt eine erweiterte ACL, aber getfacl/setfacl fehlen — abgebrochen, um sie nicht stillschweigend zu verlieren: $nach"
    fi
  elif hat_acl "$nach"; then
    if command -v setfacl >/dev/null 2>&1; then
      setfacl -b "$nach" || die "vom Zielverzeichnis geerbte ACL auf $nach konnte nicht entfernt werden"
    else
      die "$nach hat eine vermutlich vom Zielverzeichnis geerbte ACL, aber setfacl fehlt, um sie zu entfernen (Quelle $von hat keine ACL): $nach"
    fi
  fi
}

# kopiere_rechte <von> <nach>: chmod --reference plus kopiere_acl (siehe
# dort) — für Ziele, deren Rechte (nicht nur die ACL) von der Quelle
# übernommen werden (settings.json bei claude/agy/rueckbau). statusline.py
# bekommt ihre Rechte stattdessen fest über "install -m 0755" (siehe
# statusline_schreiben) und ruft nur kopiere_acl auf.
kopiere_rechte() {
  local von="$1" nach="$2"
  chmod --reference="$von" "$nach" || die "chmod fehlgeschlagen: $nach"
  kopiere_acl "$von" "$nach"
}

# entferne_acl <Datei>: entfernt eine erweiterte ACL bedingungslos, ohne
# sie je von einer Quelle zu übernehmen — für ein frisch installiertes
# Ziel, das keine sinnvolle ACL-Referenz hat (eine neu installierte
# statusline.py: die "Quelle" ist der eigene Checkout, dessen zufällige
# ACL nie auf das installierte Ziel übertragen werden darf — statusline.py
# bekommt ihre Rechte fest über "install -m 0755", nicht von einer Quelle
# übernommen, Befund 1, Runde 11).
entferne_acl() {
  local datei="$1"
  if hat_acl "$datei"; then
    if command -v setfacl >/dev/null 2>&1; then
      setfacl -b "$datei" || die "ACL auf $datei konnte nicht entfernt werden"
    else
      die "$datei hat eine ACL, aber setfacl fehlt, um sie zu entfernen: $datei"
    fi
  fi
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

  # Die Zwischendatei entsteht schon hier, nicht erst in statusline_schreiben
  # (nach der Rückfrage) — sonst würde ein fehlendes getfacl/setfacl bei
  # "agy" erst NACH der Rückfrage UND NACH dem schon geschriebenen
  # settings.json auffallen (Befund 2a, Runde 11). Referenz für
  # pruefe_acl_werkzeuge ist das bestehende Ziel (dessen ACL statusline_
  # schreiben später per kopiere_acl übernimmt) — oder, existiert noch
  # nichts, "/dev/null" (keine ACL): die Quelle im Checkout liefert nie die
  # ACL für eine Neuinstallation, siehe entferne_acl (Befund 1, Runde 11).
  statusline_tmp=$(mktemp "${STATUSLINE_RESOLVED%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
  if [ "$STATUSLINE_EXISTIERTE" -eq 1 ]; then
    pruefe_acl_werkzeuge "$STATUSLINE_RESOLVED" "$statusline_tmp"
  else
    pruefe_acl_werkzeuge "/dev/null" "$statusline_tmp"
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
  # statusline_tmp existiert schon (statusline_vorbereiten hat sie angelegt
  # und die ACL-Werkzeuge dafür schon vor der Rückfrage geprüft, Befund 2a,
  # Runde 11) — hier nur noch befüllt.
  if [ "$STATUSLINE_EXISTIERTE" -eq 1 ]; then
    local sbak
    sichern "$STATUSLINE_RESOLVED" "$ts"
    sbak="$SICHERN_BAK"
    install -m 0755 "$STATUSLINE_QUELLE" "$statusline_tmp" || die "install fehlgeschlagen: $statusline_tmp"
    # statusline.py bekommt ihre Rechte fest über "install -m 0755", nicht
    # von einer Quelle übernommen. Die ACL kommt vom BESTEHENDEN Ziel, nie
    # vom Checkout (dessen eigene, zufällige ACL — z. B. u:nobody:rwx auf
    # einem CI-Runner — sonst auf das installierte Ziel übertragen würde,
    # obwohl "fest 0755" dokumentiert ist, Befund 1, Runde 11); ein vom
    # Zielverzeichnis per Default-ACL geerbtes ACL auf der Zwischendatei
    # darf ebenfalls nicht überleben, falls das bestehende Ziel selbst
    # keine ACL trägt (Befund 4, Runde 10).
    kopiere_acl "$STATUSLINE_RESOLVED" "$statusline_tmp"
    mv "$statusline_tmp" "$STATUSLINE_RESOLVED" || die "mv fehlgeschlagen: $STATUSLINE_RESOLVED"
    statusline_tmp=""
    echo "Angewendet. Sicherung: $sbak"
  else
    if [ -e "${STATUSLINE_RESOLVED}.installed-${ts}" ]; then
      die "Marker existiert schon: ${STATUSLINE_RESOLVED}.installed-${ts}"
    fi
    install -m 0755 "$STATUSLINE_QUELLE" "$statusline_tmp" || die "install fehlgeschlagen: $statusline_tmp"
    # Keine bestehende Zieldatei, deren ACL übernommen werden könnte — die
    # ACL des Checkouts wird nie übernommen (Befund 1, Runde 11); eine vom
    # Zielverzeichnis geerbte ACL wird bedingungslos entfernt.
    entferne_acl "$statusline_tmp"
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
  pruefe_ziel_ok "$resolved"

  # Die Vorlage darf NUR permissions.allow/permissions.deny tragen — eine
  # manipulierte claude-rechte.json könnte sonst z. B. defaultMode auf
  # einen unsicheren Wert setzen, ohne dass ein reiner Schlüsselvergleich
  # danach etwas davon merkt (Befund 3, Runde 8).
  pruefe_vorlage_schluessel "$vorlage" '["permissions"]' '["allow","deny"]'

  tmp=$(mktemp "${resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
  pruefe_acl_werkzeuge "$resolved" "$tmp"

  # sha256sum direkt beim Merge (vor jeder Rückfrage) festgehalten, damit
  # eine Änderung an $ziel während der Rückfrage — auch eine, die genau
  # während des "diff" oberhalb der Rückfrage passiert — unten erkannt wird.
  local vor_sha
  vor_sha=$(sha256sum "$resolved") || die "sha256sum fehlgeschlagen: $resolved"

  # permissions.allow/permissions.deny werden als VEREINIGUNG aus Bestand
  # und Vorlage gebildet (Bestand zuerst, dann neue Einträge aus der
  # Vorlage, doppelte entfernt, Reihenfolge stabil) — ein einfacher
  # "jq -s '.[0] * .[1]'" würde beide Listen vollständig durch die Vorlage
  # ERSETZEN, weil der "*"-Operator Arrays nie zusammenführt, nur Objekte
  # rekursiv merged (Befund 1, Runde 7; Leitungsentscheid CL-131).
  # JQ_MERGE_UNION (oben, gemeinsam mit agy) baut das Ergebnis seit Runde 9
  # per "reduce" über die Vorlagen-Schlüssel, nicht mehr über "*": jeder
  # Schlüssel außer permissions wird GANZ aus der Vorlage gesetzt (bei
  # claude ist das nur permissions selbst, dessen allow/deny hier gezielt
  # durch die Vereinigung ersetzt werden — permissions.defaultMode und alle
  # anderen Top-Level-Schlüssel bleiben unverändert, weil sie nicht in der
  # Vorlage stehen). "(.permissions // {})" schützt überall gegen eine
  # Zieldatei ohne permissions-Block (Befund 2, Runde 8).
  jq -s "$JQ_MERGE_UNION" "$resolved" "$vorlage" > "$tmp" || die "jq-Merge fehlgeschlagen"

  # Sicherheitsprüfung NACH dem Merge, VOR jeder Anzeige/Rückfrage: jeder
  # bestehende allow-/deny-Eintrag muss im Ergebnis noch vorhanden sein, UND
  # jeder Wert außerhalb von permissions.allow/permissions.deny muss
  # denselben Wert (jq "==", JSON-normalisiert, nicht Byte für Byte) wie vor
  # dem Merge haben — ein reiner
  # Schlüsselvergleich hätte eine Vorlage, die einen bestehenden Wert
  # (z. B. defaultMode) überschreibt statt ihn wegzulassen, nicht erkannt
  # (Befund 3, Runde 8). "(.permissions // {})" schützt wieder gegen eine
  # Zieldatei ohne permissions-Block (Befund 2, Runde 8).
  if ! jq -n -e --slurpfile alt "$resolved" --slurpfile neu "$tmp" '
        ($alt[0].permissions // {}) as $altperm |
        ($neu[0].permissions // {}) as $neuperm |
        ((($altperm.allow // []) - ($neuperm.allow // [])) | length == 0) and
        ((($altperm.deny // []) - ($neuperm.deny // [])) | length == 0) and
        (($alt[0] | del(.permissions)) == ($neu[0] | del(.permissions))) and
        (($altperm | del(.allow, .deny)) == ($neuperm | del(.allow, .deny)))
      ' >/dev/null 2>&1; then
    die "Plausibilitätsprüfung fehlgeschlagen: der Merge hat bestehende allow-/deny-Einträge verloren oder einen anderen Wert verändert"
  fi

  echo "Merge von $vorlage in $ziel (permissions.allow/permissions.deny als" \
       "Vereinigung aus Bestand und Vorlage, Bestand zuerst, doppelte" \
       "entfernt; alles andere unverändert):"
  if zeige_diff_json "$resolved" "$tmp" "$ziel"; then
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
  kopiere_rechte "$resolved" "$tmp"
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

  # $HOME kanonisch aufgelöst — ein Vergleich gegen das rohe $HOME würde auf
  # einem symlinked Home-Verzeichnis danebengehen (Befund 1, Runde 10, siehe
  # pruefe_agy_pfad). Der kanonische Wert wird auch in die Vorlage
  # eingesetzt (nicht das rohe $HOME), damit die entstehenden write_file(...)/
  # read_file(...)-Muster den tatsächlichen, kanonischen Pfad tragen, und
  # selbst gegen die Positivliste geprüft (Befund 2, Runde 10) — er landet
  # genau wie PROJEKTE/TRUSTED_WORKSPACE in diesen Mustern.
  local home_kanon
  home_kanon=$(realpath -e "$HOME" 2>/dev/null) || die "\$HOME ist kein vorhandener Pfad (realpath -e): $HOME"
  pruefe_pfad_zeichensatz '$HOME' "$home_kanon"

  # Validiert und löst PROJEKTE/TRUSTED_WORKSPACE auf, BEVOR sie in die
  # Vorlage eingesetzt werden (Befund 1, Runde 9) — siehe pruefe_agy_pfad.
  local projekte_aufgeloest tw_aufgeloest
  projekte_aufgeloest=$(pruefe_agy_pfad PROJEKTE "$PROJEKTE" "$home_kanon")
  tw_aufgeloest=$(pruefe_agy_pfad TRUSTED_WORKSPACE "$TRUSTED_WORKSPACE" "$home_kanon")

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

  local vor_sha
  vor_sha=$(sha256sum "$resolved") || die "sha256sum fehlgeschlagen: $resolved"

  # Rendern über jq (split/join + tojson-Escaping), nicht envsubst — envsubst
  # fügt PROJEKTE/TRUSTED_WORKSPACE als reinen Text ein, ohne JSON zu
  # escapen; ein Wert mit einem eingebetteten Anführungszeichen könnte so
  # aus dem umgebenden JSON-String ausbrechen und z. B. die ganze deny-Liste
  # ersetzen (gemessen: TRUSTED_WORKSPACE mit einem passend platzierten '"'
  # löschte alle 28 deny-Einträge der Vorlage). render_agy_vorlage schließt
  # das strukturell aus (Befund 1, Runde 9).
  agy_rendered_tmp=$(mktemp "${resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
  render_agy_vorlage "$vorlage" "$home_kanon" "$projekte_aufgeloest" "$tw_aufgeloest" > "$agy_rendered_tmp"

  if ! jq -e -s 'length==1' "$agy_rendered_tmp" >/dev/null 2>&1; then
    die "gerenderte Vorlage ist kein gültiges JSON mit genau einem Dokument — PROJEKTE/TRUSTED_WORKSPACE prüfen"
  fi
  if grep -qF '${' "$agy_rendered_tmp"; then
    die "unersetztes \${ im Ergebnis — ein vierter, unbekannter Platzhalter in der Vorlage?"
  fi

  # Die Vorlage darf nur permissions/statusLine/trustedWorkspaces tragen —
  # eine manipulierte agy-settings.json könnte sonst einen fremden
  # Top-Level-Schlüssel einschmuggeln (Befund 3, Runde 8, analog zu claude).
  pruefe_vorlage_schluessel "$agy_rendered_tmp" '["permissions","statusLine","trustedWorkspaces"]' '["allow","deny"]'

  # statusLine ist eine Anweisung an agy, welchen Befehl es ausführt — eine
  # manipulierte Vorlage könnte hier beliebigen Code unterbringen
  # (z. B. statusLine.command = "/bin/sh -c …"). Trägt die Vorlage
  # statusLine, muss es exakt der erwartete, fest einprogrammierte Wert
  # sein (Befund 2, Runde 9); fehlt der Schlüssel ganz, wird nur der
  # Bestand übernommen (siehe JQ_MERGE_UNION), hier also nichts geprüft.
  # type:"" (nicht "command") entspricht der real angewendeten, getesteten
  # ~/.gemini/antigravity-cli/settings.json (gemessen 2026-09-30, siehe
  # PR-Text „Runde 10") — nicht auf "command" geändert, weil das gegen die
  # echte, funktionierende Konfiguration nicht verifiziert ist.
  local statusline_erwartet
  statusline_erwartet=$(jq -nc --arg cmd "${home_kanon}/.gemini/antigravity-cli/statusline.py" \
    '{"type":"","command":$cmd}')
  if ! jq -e --argjson erw "$statusline_erwartet" \
        'if has("statusLine") then (.statusLine == $erw) else true end' "$agy_rendered_tmp" >/dev/null 2>&1; then
    die "statusLine der Vorlage weicht vom erwarteten, fest einprogrammierten Wert ab (Manipulation?): $vorlage"
  fi

  # Vorlagen-Regeln für die Superset-/Subset-Prüfung nach dem Merge
  # festgehalten (Befund 1, Runde 9), bevor die gerenderte Vorlage entfernt
  # wird.
  local vorlage_allow vorlage_deny
  vorlage_allow=$(jq -c '(.permissions // {}).allow // []' "$agy_rendered_tmp")
  vorlage_deny=$(jq -c '(.permissions // {}).deny // []' "$agy_rendered_tmp")

  # permissions.allow/permissions.deny als Vereinigung aus Bestand und
  # gerenderter Vorlage (wie bei claude, JQ_MERGE_UNION); statusLine und
  # trustedWorkspaces (und jeder andere, von der Vorlage getragene
  # Schlüssel) ersetzen weiterhin den Bestand — das ist beabsichtigt
  # ("ersetzen" in setup/inventar.json), nur die beiden Regel-Listen werden
  # zusammengeführt statt ersetzt (Befund 1, Runde 8: ein voller Ersatz hätte
  # eigene deny-Regeln wie "command(make)" stillschweigend gelöscht — mehr
  # Rechte für agy, nicht weniger). Top-Level-Schlüssel, die nur im Bestand
  # stehen (z. B. ein von Hand ergänztes "model"), bleiben unverändert.
  tmp=$(mktemp "${resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
  pruefe_acl_werkzeuge "$resolved" "$tmp"
  jq -s "$JQ_MERGE_UNION" "$resolved" "$agy_rendered_tmp" > "$tmp" || die "jq-Merge fehlgeschlagen"
  rm -f "$agy_rendered_tmp"
  agy_rendered_tmp=""

  # Sicherheitsprüfung wie bei claude, aber ohne Werte außerhalb von
  # permissions als unveränderlich zu behandeln — statusLine/
  # trustedWorkspaces SOLLEN sich ändern ("ersetzen"); geprüft wird, dass
  # kein bestehender allow-/deny-Eintrag und kein bestehender Top-Level-
  # oder permissions-Schlüssel verloren geht, dass deny die Vorlage
  # vollständig umfasst, dass allow nur aus Bestand und Vorlage stammt, und
  # dass trustedWorkspaces exakt der aufgelöste TRUSTED_WORKSPACE ist
  # (Befund 1, Runde 9).
  if ! jq -n -e --slurpfile alt "$resolved" --slurpfile neu "$tmp" \
        --argjson vallow "$vorlage_allow" --argjson vdeny "$vorlage_deny" \
        --arg tw "$tw_aufgeloest" '
        ($alt[0].permissions // {}) as $altperm |
        ($neu[0].permissions // {}) as $neuperm |
        (($altperm.allow // []) + $vallow) as $allow_erlaubt |
        ((($altperm.allow // []) - ($neuperm.allow // [])) | length == 0) and
        ((($altperm.deny // []) - ($neuperm.deny // [])) | length == 0) and
        (($vdeny - ($neuperm.deny // [])) | length == 0) and
        ((($neuperm.allow // []) - $allow_erlaubt) | length == 0) and
        ((($alt[0] | keys) - ($neu[0] | keys)) | length == 0) and
        ((($altperm | keys) - ($neuperm | keys)) | length == 0) and
        ($neu[0].trustedWorkspaces == [$tw])
      ' >/dev/null 2>&1; then
    die "Plausibilitätsprüfung fehlgeschlagen: der Merge hat bestehende Einträge/Schlüssel verloren, die Vorlagen-deny-Liste nicht vollständig übernommen, einen fremden allow-Eintrag eingeführt, oder trustedWorkspaces weicht vom aufgelösten TRUSTED_WORKSPACE ab"
  fi

  local settings_frage=0 statusline_frage=0

  echo "Merge von $vorlage in $ziel (PROJEKTE/TRUSTED_WORKSPACE per jq" \
       "eingesetzt, nicht envsubst; permissions.allow/permissions.deny als" \
       "Vereinigung aus Bestand und Vorlage, alles andere aus der Vorlage" \
       "ersetzt den Bestand):"
  if zeige_diff_json "$resolved" "$tmp" "$ziel"; then
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
    kopiere_rechte "$resolved" "$tmp"
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

  # Ziele, die dieser Aufruf tatsächlich zurückspielt (oder entfernt), müssen
  # VORAB reguläre, beschreibbare Dateien in einem beschreibbaren Verzeichnis
  # sein — sonst könnte claude/agy schon zurückgespielt sein, bevor
  # statusline.py an einem unbeschreibbaren Ziel scheitert (Befund 2b aus
  # Runde 7: sonst würde der Rückbau mitten in "statusline.py" abbrechen,
  # ohne dass der Zeitstempel für einen Rückbau DIESES Rückbaus (unten) je
  # ausgegeben wird, obwohl claude/agy davor schon geändert wurden).
  [ "$restore_claude" -eq 1 ] && pruefe_ziel_ok "$claude_resolved"
  [ "$restore_agy" -eq 1 ] && pruefe_ziel_ok "$agy_resolved"
  { [ "$restore_statusline" -eq 1 ] || [ "$remove_statusline" -eq 1 ]; } && pruefe_ziel_ok "$statusline_resolved"

  # Dieselbe ACL-Werkzeug-Prüfung wie bei claude/agy, hier schon vor der
  # Diff-Anzeige und der Rückfrage — nicht erst nach einer schon angelegten
  # Sicherung. Eine Prüfung nur gegen die schon vorhandene Zieldatei selbst
  # (wie noch in Runde 10) übersieht eine vom Zielverzeichnis per
  # Default-ACL geerbte ACL: die zeigt sich erst an einer echten, frisch
  # angelegten Zwischendatei in genau diesem Verzeichnis — ohne das käme
  # der Abbruch bei "agy" erst beim tatsächlichen mktemp weiter unten,
  # NACHDEM claude schon zurückgespielt wurde, und der Zeitstempel für
  # einen Rückbau dieses Rückbaus würde nie ausgegeben (Befund 2b, Runde
  # 11). Für alle Ziele dieses Aufrufs, bevor irgendetwas geschrieben wird
  # (all-or-nothing) — siehe pruefe_acl_werkzeuge_frueh (oben).
  [ "$restore_claude" -eq 1 ] && pruefe_acl_werkzeuge_frueh "$claude_resolved" "$claude_resolved"
  [ "$restore_agy" -eq 1 ] && pruefe_acl_werkzeuge_frueh "$agy_resolved" "$agy_resolved"
  [ "$restore_statusline" -eq 1 ] && [ -e "$statusline_resolved" ] && pruefe_acl_werkzeuge_frueh "$statusline_resolved" "$statusline_resolved"

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
    # mktemp (und damit die ACL-Werkzeug-Prüfung) VOR sichern — sonst
    # bräche ein fehlendes setfacl erst NACH der Sicherung ab, mit einer
    # überzähligen .bak-Datei als Rest (Befund 3, Runde 10).
    rueckbau_tmp=$(mktemp "${claude_resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
    pruefe_acl_werkzeuge "$claude_resolved" "$rueckbau_tmp"
    sichern "$claude_resolved" "$rueck_ts"
    cp -p "$claude_bak" "$rueckbau_tmp" || die "cp fehlgeschlagen: $claude_bak"
    kopiere_rechte "$claude_resolved" "$rueckbau_tmp"
    mv "$rueckbau_tmp" "$claude_resolved" || die "mv fehlgeschlagen: $claude_resolved"
    rueckbau_tmp=""
    echo "Zurückgespielt: $claude_resolved"
  fi

  if [ "$restore_agy" -eq 1 ]; then
    rueckbau_tmp=$(mktemp "${agy_resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
    pruefe_acl_werkzeuge "$agy_resolved" "$rueckbau_tmp"
    sichern "$agy_resolved" "$rueck_ts"
    cp -p "$agy_bak" "$rueckbau_tmp" || die "cp fehlgeschlagen: $agy_bak"
    kopiere_rechte "$agy_resolved" "$rueckbau_tmp"
    mv "$rueckbau_tmp" "$agy_resolved" || die "mv fehlgeschlagen: $agy_resolved"
    rueckbau_tmp=""
    echo "Zurückgespielt: $agy_resolved"
  fi

  if [ "$restore_statusline" -eq 1 ]; then
    rueckbau_tmp=$(mktemp "${statusline_resolved%/*}/.rechte-anwenden.XXXXXX") || die "mktemp fehlgeschlagen"
    if [ -e "$statusline_resolved" ]; then
      pruefe_acl_werkzeuge "$statusline_resolved" "$rueckbau_tmp"
    fi
    cp -p "$statusline_bak" "$rueckbau_tmp" || die "cp fehlgeschlagen: $statusline_bak"
    if [ -e "$statusline_resolved" ]; then
      sichern "$statusline_resolved" "$rueck_ts"
      kopiere_rechte "$statusline_resolved" "$rueckbau_tmp"
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
