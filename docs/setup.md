# Setup-Inventar und `imprint-dev setup`

Das Werkzeug `imprint-dev setup` verwaltet und vergleicht Dateien aus dem lokalen Setup-Inventar (`setup/inventar.json`) mit den Zielpfaden im System.

## Befehlsaufruf

```sh
imprint-dev setup (--plan | --apply | --check) [--root dir] [--repo path]
```

Optionen (genau eines der Modi-Flags muss angegeben werden):
* `--plan`: Vergleicht Einträge mit Zielpfaden und gibt Status-Tabelle sowie Diffs aus, ohne Schreibaktionen.
* `--apply`: Wendet Einträge im System an (schreibt Zieldateien, konfiguriert Git-Hooks, rendert Rechte-Vorlagen).
* `--check`: Prüft auf Drift zwischen Soll- und Ist-Zustand ohne Schreibaktionen (Exit 0 bei Übereinstimmung, Exit 1 bei Drift).
* `--root dir`: Wurzelverzeichnis für das Hauptinventar `setup/inventar.json` (Standard: `.`).
* `--repo path`: Optionales Pfad-Argument zu einem Projekt-Repository. Falls angegeben, wird zusätzlich `<repo>/.imprint/setup.json` gelesen.

## Modi-Semantik

### `--plan`
Vergleicht den Quellinhalt aus dem Inventar mit dem Ist-Zustand auf der Festplatte und gibt Status und Diffs aus, ohne Dateien zu verändern. Für Einträge mit `rechte=true` wird nur der Status ausgegeben (z.B. `abweichend`), niemals ein Diff oder Dateiinhalt — solche Ziele sind reale Sicherheits-/Berechtigungsdateien und können Umgebungswerte oder Hooks enthalten, die nicht in ein Transkript oder Log gehören.

### `--apply`
Wendet die Inventareinträge an (nur für Einträge mit `rechte=false` direkt im Ziel):
* `shim`: Schreibt ein ausführbares Shell-Skript unter `ziel` (z.B. in `~/.local/bin`), das `"$IMPRINT_CORE_ROOT/<quelle>" "$@"` aufruft. Erstellt ein `.bak` bei abweichendem Ziel.
* `githook`: Führt `git config core.hooksPath .githooks` im Ziel-Repository aus.
* `copy`: Kopiert `quelle` nach `ziel`. Erstellt ein `.bak` bei abweichendem Ziel.
* `systemd`: Kopiert die Unit-Datei nach `ziel` unter `~/.config/systemd/user/`. Führt niemals `systemctl`-Befehle aus.
* `marketplace` / `mcp`: Druckt den entsprechenden Add-Befehl auf stdout, ohne ihn auszuführen.

#### Rechte-Tor (`rechte=true`)
Für Einträge mit `rechte=true` schreibt `--apply` niemals direkt in `ziel`. Stattdessen wird die Vorlage durch Platzhalter-Ersetzung (`${HOME}`, `${XDG_CONFIG_HOME}`) nach `$XDG_CACHE_HOME/imprint/<id>-<basename>` gerendert und die manuellen `cp`-Befehle werden auf stdout gedruckt.

#### Env-Guard
`--apply` verweigert die Ausführung (exit 2), wenn `CLAUDECODE` oder eine Umgebungsvariable mit Präfix `ANTIGRAVITY_` oder `AGY_` gesetzt ist ("apply führt der Mensch aus").

### `--check`
Vergleicht den Soll-Zustand mit dem Ist-Zustand im System, ohne Dateien oder Cache-Dateien zu schreiben. Wie bei `--plan` gilt für `rechte=true`-Einträge: nur der Status wird ausgegeben, nie ein Diff oder Dateiinhalt.
* Exit 0: Kein Eintrag hat Drift.
* Exit 1: Mindestens ein Eintrag driftet (`fehlt` oder `abweichend`).

## Inventar-Schema (`inventar.json`)

Das Inventar besteht aus einer JSON-Liste von Eintrags-Objekten. Jeder Eintrag erfordert die folgenden Felder:

* `id` (String): Eindeutige Kennung des Eintrags.
* `typ` (String): Einer der erlaubten Typen: `shim`, `githook`, `copy`, `rechte-vorlage`, `marketplace`, `mcp`, `systemd`.
* `quelle` (String): Relativer Pfad zur Quelldatei innerhalb des jeweiligen Inventar-Roots.
* `ziel` (String): Zielpfad im Dateisystem. Kann Umgebungsvariablen wie `${HOME}`, `${XDG_CONFIG_HOME}` und `${XDG_CACHE_HOME}` enthalten.
* `rechte` (Boolean): Markiert Sicherheits- oder Berechtigungsdateien (`true`/`false`).
* `beschreibung` (String): Kurze Beschreibung des Eintrags.

## Status-Werte

* `fehlt`: Die Zieldatei existiert nicht.
* `gleich`: Soll- und Ist-Zustand stimmen überein.
* `abweichend`: Soll- und Ist-Zustand unterscheiden sich.
* `geschrieben`: Datei/Konfiguration wurde erfolgreich durch `--apply` angewendet.
* `unverändert`: Ziel war bereits aktuell.
* `gedruckt` / `hinweis`: Befehl wurde gedruckt oder ist rein informativ.
* `nicht-prüfbar`: Unlesbare Dateien, Pfad-Traversierung ausserhalb erlaubter Wurzeln oder sonstige Fehler.
