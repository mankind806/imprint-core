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
* `--repo path`: Optionales Pfad-Argument zu einem Projekt-Repository. Falls angegeben, wird zusätzlich `<repo>/.imprint/setup.json` gelesen. Einträge aus `--repo` werden nie angewendet und nie mit Diff gezeigt, nur ihr Status (siehe Sicherheitsgrenze).

## Sicherheitsgrenze (Rechte-Tor)

Was `setup` lesen, zeigen und schreiben darf, entscheiden die Pfadprüfungen in `tools/imprint-dev/setup.go`, nicht das Feld `rechte` des Inventars und nicht der Env-Guard (der ist nur beratend). Durchgesetzt von `setup.go`, bewacht von `setup_security_test.go`.

| Regel | Wirkung |
|---|---|
| `id` nur `^[a-z0-9][a-z0-9-]{0,63}$`; `quelle`/`ziel` nur `[A-Za-z0-9._/+-]` (plus `${HOME}`, `${XDG_CONFIG_HOME}`, `${XDG_CACHE_HOME}`, `${XDG_DATA_HOME}`) | sonst ist das Inventar ungültig (exit 2) |
| `typ=rechte-vorlage` verlangt `rechte=true` | sonst ist das Inventar ungültig (exit 2) |
| Allowlist für `rechte=false`: `shim` → `${HOME}/.local/bin/<id>`; `systemd` → `${XDG_CONFIG_HOME}/systemd/user/*.service\|*.timer`; `copy` → unter `${XDG_CONFIG_HOME}/imprint/` oder `${XDG_DATA_HOME}/imprint/`; `githook` → nur `git config --local core.hooksPath` | alles andere: Status `verweigert` |
| Sperrliste (immer Rechte-Pfad): `~/.claude/`, `~/.gemini/`, `~/.codex/`, `~/.ssh/`, `~/.gnupg/`, `~/.config/archiv/`, `~/.local/share/archiv/`, Keyrings, `~/.bashrc*`, `~/.profile` und weitere Shell-Startdateien, `*.wants`/`*.requires` unter systemd u.a. | Status `gesperrt`: nie geschrieben, nie mit Diff gezeigt |
| Symlinks: `quelle` muss nach `EvalSymlinks` im Inventar-Root liegen; Ziel-Verzeichnis wird aufgelöst und neu geprüft; Ziel und `.bak` dürfen kein Symlink sein (Lstat + `O_NOFOLLOW`) | sonst `nicht-prüfbar` bzw. Fehler |
| `--repo`-Einträge | nie geschrieben, nie Diff, keine gedruckten Befehle; `rechte=true` oder Ziele außerhalb der Allowlist werden nicht einmal gelesen |
| Diff (`--plan`) | nur für `rechte=false`-Einträge des Plugin-Inventars mit Ziel in der Allowlist |

## Modi-Semantik

### `--plan`
Vergleicht den Quellinhalt aus dem Inventar mit dem Ist-Zustand auf der Festplatte und gibt Status und Diffs aus, ohne Dateien zu verändern. Für Einträge mit `rechte=true` wird nur der Status ausgegeben (z.B. `abweichend`), niemals ein Diff oder Dateiinhalt — solche Ziele sind reale Sicherheits-/Berechtigungsdateien und können Umgebungswerte oder Hooks enthalten, die nicht in ein Transkript oder Log gehören.

### `--apply`
Wendet die Inventareinträge an (nur für Einträge mit `rechte=false` direkt im Ziel):
* `shim`: Schreibt `${HOME}/.local/bin/<id>`, ein Shell-Skript, das `"$IMPRINT_CORE_ROOT"/'<quelle>' "$@"` aufruft (quelle in einfachen Anführungszeichen). Erstellt ein `.bak` bei abweichendem Ziel.
* `githook`: Führt `git config --local core.hooksPath .githooks` im Plugin-Root aus.
* `copy`: Kopiert `quelle` nach `ziel` (nur unter `${XDG_CONFIG_HOME}/imprint/` oder `${XDG_DATA_HOME}/imprint/`). Erstellt ein `.bak` bei abweichendem Ziel.
* `systemd`: Kopiert die Unit-Datei nach `${XDG_CONFIG_HOME}/systemd/user/<name>.service|.timer`. Führt niemals `systemctl`-Befehle aus.
* `marketplace` / `mcp`: Druckt den entsprechenden Add-Befehl (shell-gequotet) auf stdout, ohne ihn auszuführen.

Das `.bak` behält die Rechte des Originals. Lässt es sich nicht schreiben (Symlink, Verzeichnis, I/O-Fehler), bleibt das Ziel unverändert und der Eintrag gilt als Fehler. `--apply` endet mit exit 1, sobald ein Eintrag verweigert wurde oder fehlschlug.

#### Rechte-Tor (`rechte=true`)
Für Einträge mit `rechte=true` schreibt `--apply` niemals direkt in `ziel`. Stattdessen wird die Vorlage durch Platzhalter-Ersetzung (`${HOME}`, `${XDG_CONFIG_HOME}`, `${XDG_CACHE_HOME}`, `${XDG_DATA_HOME}`) nach `$XDG_CACHE_HOME/imprint/<id>-<basename>` gerendert (Modus 0600, kein Symlink) und die manuellen, shell-gequoteten `cp`-Befehle werden auf stdout gedruckt.

#### Env-Guard
`--apply` verweigert die Ausführung (exit 2), wenn `CLAUDECODE` oder eine Umgebungsvariable mit Präfix `ANTIGRAVITY_` oder `AGY_` gesetzt ist, auch mit leerem Wert ("apply führt der Mensch aus"). Der Guard ist nur beratend; die Grenze liegt in den Pfadprüfungen.

### `--check`
Vergleicht den Soll-Zustand mit dem Ist-Zustand im System, ohne Dateien oder Cache-Dateien zu schreiben. Wie bei `--plan` gilt für `rechte=true`-Einträge: nur der Status wird ausgegeben, nie ein Diff oder Dateiinhalt.
* Exit 0: Kein Eintrag hat Drift.
* Exit 1: Mindestens ein Eintrag driftet (`fehlt`, `abweichend`, `gesperrt`, `verweigert` oder `nicht-prüfbar`).

## Inventar-Schema (`inventar.json`)

Das Inventar besteht aus einer JSON-Liste von Eintrags-Objekten. Jeder Eintrag erfordert die folgenden Felder:

* `id` (String): Eindeutige Kennung des Eintrags, `^[a-z0-9][a-z0-9-]{0,63}$`.
* `typ` (String): Einer der erlaubten Typen: `shim`, `githook`, `copy`, `rechte-vorlage`, `marketplace`, `mcp`, `systemd`.
* `quelle` (String): Relativer Pfad zur Quelldatei innerhalb des jeweiligen Inventar-Roots.
* `ziel` (String): Zielpfad im Dateisystem. Kann nur `${HOME}`, `${XDG_CONFIG_HOME}`, `${XDG_CACHE_HOME}` und `${XDG_DATA_HOME}` enthalten; für `githook`, `marketplace` und `mcp` wird es nicht verwendet.
* `rechte` (Boolean): Markiert Sicherheits- oder Berechtigungsdateien (`true`/`false`).
* `beschreibung` (String): Kurze Beschreibung des Eintrags.

## Status-Werte

* `fehlt`: Die Zieldatei existiert nicht.
* `gleich`: Soll- und Ist-Zustand stimmen überein.
* `abweichend`: Soll- und Ist-Zustand unterscheiden sich.
* `geschrieben`: Datei/Konfiguration wurde erfolgreich durch `--apply` angewendet.
* `unverändert`: Ziel war bereits aktuell.
* `gedruckt` / `hinweis`: Befehl wurde gedruckt oder ist rein informativ.
* `nicht-prüfbar`: Unlesbare Dateien, Symlinks, Pfad-Traversierung der `quelle` oder sonstige Fehler.
* `gesperrt`: Ziel liegt auf der Sperrliste, oder `rechte=true` aus `--repo`.
* `verweigert`: Ziel liegt außerhalb der erlaubten Wurzeln oder der Allowlist des `typ`.
* `Fehler`: `--apply` konnte den Eintrag nicht anwenden (Details auf stderr).
