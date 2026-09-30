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
#
# agy setzt ${HOME}/.gemini/antigravity-cli/settings.json komplett aus
# setup/vorlagen/agy-settings.json neu (envsubst für ${PROJEKTE} und
# ${TRUSTED_WORKSPACE}) und installiert setup/vorlagen/statusline.py nach
# ${HOME}/.gemini/antigravity-cli/statusline.py.
#
# Beide Unterbefehle zeigen vor jeder Änderung den Diff und fragen
# ausdrücklich nach; ohne "j"/"J" bricht das Skript ohne jede Änderung ab.
# Bei Zustimmung liegt zuerst eine Sicherung mit sekundengenauem Zeitstempel
# neben der Zieldatei (cp -p -n), danach übernimmt ein atomares mv die neue
# Datei (gleiches Dateisystem, weil die Zwischendatei per mktemp im selben
# Verzeichnis liegt). Ist die Zieldatei ein Symlink, bleibt der Symlink
# erhalten — geschrieben wird auf die Datei, auf die er zeigt (readlink -f).
#
# rueckbau <Zeitstempel> spielt die mit "claude"/"agy" angelegten
# Sicherungen zu genau diesem Zeitstempel zurück. claude und agy erzeugen je
# einen eigenen Zeitstempel (zwei getrennte Aufrufe) — rueckbau spielt
# zurück, was zu <Zeitstempel> tatsächlich gesichert wurde, und bricht ab,
# wenn dazu gar keine Sicherung existiert.
#
# Laufende agy-Hubs schreiben eine soeben geänderte agy-settings.json sonst
# zurück (gemessen 2026-09-30). Das Skript tötet dafür keinen Prozess selbst
# — ein pkill-Muster wie "agy --hub" kann auch Hubs anderer, unbeteiligter
# Projekte treffen, und ein getöteter Prozess lässt sich über rueckbau nicht
# rückgängig machen. Es zeigt bei "agy" und "rueckbau" stattdessen einen
# Hinweis inklusive der aktuell laufenden Treffer, und der Mensch entscheidet
# und beendet gezielt, bevor er die Diff-Frage mit "j" beantwortet.

set -euo pipefail

case "${BASH_SOURCE[0]}" in
  */*) script_dir=$(cd "${BASH_SOURCE[0]%/*}" && pwd) ;;
  *) script_dir=$(pwd) ;;
esac

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
  need mktemp readlink chmod diff cp mv grep install
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

zeige_diff_und_frage() {
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
  local ans=""
  read -r -p "Anwenden? [j/N]" ans || ans=""
  if [ "$ans" != "j" ] && [ "$ans" != "J" ]; then
    echo "Abgebrochen ohne Änderung." >&2
    exit 1
  fi
  return 0
}

sichern() {
  local resolved="$1" ts="$2"
  local bak="${resolved}.bak-${ts}"
  if [ -e "$bak" ]; then
    die "Sicherung existiert schon: $bak"
  fi
  cp -p -n "$resolved" "$bak"
  printf '%s\n' "$bak"
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

  local tmp
  tmp=$(mktemp "${resolved%/*}/.rechte-anwenden.XXXXXX")
  trap 'rm -f "$tmp"' EXIT

  jq -s '.[0] * .[1]' "$resolved" "$vorlage" > "$tmp"
  if ! jq -e '.permissions.deny|length>0' "$tmp" >/dev/null 2>&1; then
    die "Plausibilitätsprüfung fehlgeschlagen: permissions.deny ist nach dem Merge leer"
  fi

  echo "Merge von $vorlage in $ziel (jq -s '.[0] * .[1]'; Listen wie" \
       "permissions.allow/permissions.deny werden dabei vollständig durch" \
       "die Vorlage ersetzt, nicht mit dem Bestand vereinigt):"
  if ! zeige_diff_und_frage "$resolved" "$tmp"; then
    exit 0
  fi

  local ts bak
  ts="$(date +%Y%m%d-%H%M%S)"
  bak="$(sichern "$resolved" "$ts")"
  chmod --reference="$resolved" "$tmp"
  mv "$tmp" "$resolved"
  trap - EXIT
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

  hub_hinweis

  local resolved
  resolved=$(resolve_ziel "$ziel")

  local tmp
  tmp=$(mktemp "${resolved%/*}/.rechte-anwenden.XXXXXX")
  trap 'rm -f "$tmp"' EXIT

  # shellcheck disable=SC2016
  HOME="$HOME" PROJEKTE="$PROJEKTE" TRUSTED_WORKSPACE="$TRUSTED_WORKSPACE" \
    envsubst '${HOME} ${PROJEKTE} ${TRUSTED_WORKSPACE}' \
    < "$vorlage" > "$tmp"

  if ! jq -e . "$tmp" >/dev/null 2>&1; then
    die "envsubst-Ergebnis ist kein gültiges JSON — PROJEKTE/TRUSTED_WORKSPACE prüfen"
  fi
  if grep -qF '${' "$tmp"; then
    die "unersetztes \${ im Ergebnis — PROJEKTE/TRUSTED_WORKSPACE prüfen"
  fi

  echo "Ersetzen von $ziel durch $vorlage (envsubst PROJEKTE/TRUSTED_WORKSPACE)" \
       "und Installieren von statusline.py:"
  if ! zeige_diff_und_frage "$resolved" "$tmp"; then
    exit 0
  fi

  local statusline_ziel statusline_resolved
  statusline_ziel="${HOME}/.gemini/antigravity-cli/statusline.py"
  statusline_resolved=$(resolve_ziel "$statusline_ziel")

  local ts bak
  ts="$(date +%Y%m%d-%H%M%S)"
  # Beide Sicherungs-Zielpfade auf Kollision prüfen, bevor irgendetwas
  # geschrieben wird — sonst bliebe bei einer Kollision erst bei
  # statusline.py ein schon geänderter settings.json-Stand ohne Rückweg
  # zurück (kein passendes rueckbau für dessen halb angewendeten Lauf).
  if [ -e "${resolved}.bak-${ts}" ]; then
    die "Sicherung existiert schon: ${resolved}.bak-${ts}"
  fi
  if [ -e "$statusline_resolved" ] && [ -e "${statusline_resolved}.bak-${ts}" ]; then
    die "Sicherung existiert schon: ${statusline_resolved}.bak-${ts}"
  fi

  bak="$(sichern "$resolved" "$ts")"
  chmod --reference="$resolved" "$tmp"
  mv "$tmp" "$resolved"

  if [ -e "$statusline_resolved" ]; then
    sichern "$statusline_resolved" "$ts" >/dev/null
  fi
  install -m 0755 "$script_dir/statusline.py" "$statusline_resolved"
  trap - EXIT

  echo "Angewendet. Sicherung: $bak"
  echo "Zeitstempel für rueckbau: $ts"
}

cmd_rueckbau() {
  need cp readlink

  local ts="${1:?Zeitstempel fehlt (rechte-anwenden.sh rueckbau <Zeitstempel>)}"

  hub_hinweis

  local claude_resolved claude_bak
  claude_resolved=$(resolve_ziel "${HOME}/.claude/settings.json")
  claude_bak="${claude_resolved}.bak-${ts}"

  local agy_resolved agy_bak
  agy_resolved=$(resolve_ziel "${HOME}/.gemini/antigravity-cli/settings.json")
  agy_bak="${agy_resolved}.bak-${ts}"

  local statusline_resolved statusline_bak
  statusline_resolved=$(resolve_ziel "${HOME}/.gemini/antigravity-cli/statusline.py")
  statusline_bak="${statusline_resolved}.bak-${ts}"

  local restored=0

  if [ -f "$claude_bak" ]; then
    cp -p "$claude_bak" "$claude_resolved"
    echo "Zurückgespielt: $claude_resolved"
    restored=1
  fi

  if [ -f "$agy_bak" ]; then
    cp -p "$agy_bak" "$agy_resolved"
    echo "Zurückgespielt: $agy_resolved"
    restored=1

    # statusline.py gehört nur zu einem agy-Lauf; nur bei dessen eigenem
    # Zeitstempel (also wenn der agy-Backup zu <ts> existiert) entscheiden,
    # ob es zurückgespielt oder entfernt wird — sonst würde ein rueckbau zu
    # einem claude-Zeitstempel eine unbeteiligte statusline.py löschen.
    if [ -f "$statusline_bak" ]; then
      cp -p "$statusline_bak" "$statusline_resolved"
      echo "Zurückgespielt: $statusline_resolved"
    else
      if [ -f "$statusline_resolved" ]; then
        rm -f "$statusline_resolved"
        echo "Entfernt (existierte vor diesem Anwenden nicht): $statusline_resolved"
      fi
    fi
  fi

  if [ "$restored" -eq 0 ]; then
    die "keine Sicherung zu Zeitstempel $ts gefunden ($claude_bak, $agy_bak)"
  fi
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
