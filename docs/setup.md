# Setup-Inventar und `imprint-dev setup`

Das Werkzeug `imprint-dev setup` verwaltet und vergleicht Dateien aus dem lokalen Setup-Inventar (`setup/inventar.json`) mit den Zielpfaden im System.

## Befehlsaufruf

```sh
imprint-dev setup (--plan | --check) [--root dir] [--repo path]
imprint-dev setup --apply --root dir [--repo path]
```

Optionen (genau eines der Modi-Flags muss angegeben werden):
* `--plan`: Vergleicht Einträge mit Zielpfaden und gibt Status-Tabelle sowie Diffs aus, ohne Schreibaktionen.
* `--apply`: Wendet Einträge im System an (schreibt Zieldateien, konfiguriert Git-Hooks, rendert Rechte-Vorlagen).
* `--check`: Prüft auf Drift zwischen Soll- und Ist-Zustand ohne Schreibaktionen (Exit 0 bei Übereinstimmung, Exit 1 bei Drift).
* `--root dir`: Wurzelverzeichnis für das Hauptinventar `setup/inventar.json` (Standard für `--plan`/`--check`: `.`). Bei `--apply` Pflicht, ohne Standard: der Root muss der Plugin-Root sein, aus dem das laufende `imprint-dev` gebaut ist (Anker, siehe Sicherheitsgrenze), `.claude-plugin/plugin.json` mit `name` `imprint` tragen (reguläre Datei; weder `.claude-plugin/` noch die Datei ein Symlink) und darf nicht dasselbe Verzeichnis wie `--repo` sein. Ein bloßes `setup --apply` in einem fremden Checkout endet mit exit 2.
* `--repo path`: Optionales Pfad-Argument zu einem Projekt-Repository. Falls angegeben, wird zusätzlich `<repo>/.imprint/setup.json` gelesen. Einträge aus `--repo` werden nie angewendet und nie mit Diff gezeigt, nur ihr Status (siehe Sicherheitsgrenze).

## Sicherheitsgrenze (Rechte-Tor)

Was `setup` lesen, zeigen und schreiben darf, entscheiden die Pfadprüfungen in `tools/imprint-dev/setup.go`, nicht das Feld `rechte` des Inventars und nicht der Env-Guard (der ist nur beratend). Durchgesetzt von `setup.go` (und `setup_fs_linux.go` für die fd-Pfade), bewacht von `setup_security_test.go`, `setup_round2_test.go` und `setup_round3_test.go`.

| Regel | Wirkung |
|---|---|
| `id` nur `^[a-z0-9][a-z0-9-]{0,63}$`; `quelle`/`ziel` nur `[A-Za-z0-9._/+-]` (plus `${HOME}`, `${XDG_CONFIG_HOME}`, `${XDG_CACHE_HOME}`, `${XDG_DATA_HOME}`) | sonst ist das Inventar ungültig (exit 2) |
| `typ=rechte-vorlage` verlangt `rechte=true`; `rechte=true` verlangt `anwenden`; `anwenden` nur `ersetzen` oder `fragment-merge`, `fragment-merge` nur mit `rechte=true` und `.json`-Ziel | sonst ist das Inventar ungültig (exit 2) |
| `--apply` nur mit ausdrücklichem `--root`, der der Plugin-Root ist, aus dem das laufende Binary gebaut ist (Anker: der vom Go-Compiler eingebettete absolute Quellpfad `<root>/tools/imprint-dev/`, verglichen per `os.SameFile`), `.claude-plugin/plugin.json` mit `name` `imprint` trägt (ohne Symlink ab `--root`) und nicht `--repo` ist | sonst exit 2, nichts geschrieben; ein mit `-trimpath` gebautes Binary hat keinen Anker und verweigert `--apply` |
| Allowlist für `rechte=false`: `shim` → `${HOME}/.local/bin/<id>` mit `id` = `imprint-…`; `systemd` → `${XDG_CONFIG_HOME}/systemd/user/imprint-<id>.service\|.timer`; `copy` → unter `${XDG_CONFIG_HOME}/imprint/` oder `${XDG_DATA_HOME}/imprint/`; `githook` → nur `git config --local core.hooksPath` | alles andere: Status `verweigert` |
| Shim: Name schon ein anderes ausführbares Kommando in `PATH` (außerhalb `~/.local/bin`; ein führendes `~`, `~+`, `~-` oder `~name` in einem `PATH`-Element wird wie von bash expandiert), oder vorhandenes Ziel ohne die Markerzeile `# imprint-shim: …` | Status `verweigert`; eine fremde Datei wird nie ersetzt |
| Shim: `quelle` fehlt im Root oder ist nicht ausführbar | Fehler, kein Shim geschrieben |
| systemd: Unit-Name existiert schon in einem anderen User-Unit-Pfad (`systemd-analyze --user unit-paths` plus die Standardpfade aus systemd.unit(5), z. B. `/etc/systemd/user`, `/usr/lib/systemd/user`, `${XDG_DATA_HOME}/systemd/user`) | wird wie `rechte=true` behandelt: nur gedruckt, Status mit `(Mensch)` |
| Hardlinks: Ziel oder `.bak` mit mehr als einem Link (`fstat`, `Nlink>1`) | nie gelesen, nie im Diff gezeigt, nie ersetzt |
| Schreiben: Ziel-Verzeichnis über Verzeichnis-fds ab der aufgelösten erlaubten Wurzel, jeder Bestandteil mit `O_DIRECTORY\|O_NOFOLLOW` per `openat` (Linux; die Garantie von `openat2` mit `RESOLVE_NO_SYMLINKS\|RESOLVE_BENEATH`, ohne `golang.org/x/sys`); Datei über Temp-Datei im selben Verzeichnis plus `renameat` | ein nach der Prüfung vertauschtes Verzeichnis wird nicht verfolgt |
| `XDG_*_HOME` nicht unterhalb von `$HOME` (etwa `/`, `/etc` oder `$HOME` selbst) oder `HOME=/` | gilt als nicht gesetzt; ein XDG-Ordner unter `$HOME`, der per Symlink hinausführt, ergibt keine erlaubte Wurzel |
| Allowlist-Basis (`~/.config/imprint`, `~/.local/share/imprint`, `~/.local/bin`, `~/.config/systemd/user`) oder ein Ordner zwischen `$HOME` und dem Ziel ist ein Symlink | Status `verweigert`, auch wenn der Symlink innerhalb von `$HOME` bleibt |
| Modus geschriebener Dateien | `0600`, ein Shim `0755`; das Inventar kann ihn nicht aufweiten |
| Sperrliste (immer Rechte-Pfad): `~/.claude/`, `~/.gemini/`, `~/.codex/`, `~/.ssh/`, `~/.gnupg/`, `~/.config/archiv/`, `~/.local/share/archiv/`, Keyrings, `~/.bashrc*`, `~/.profile` und weitere Shell-Startdateien, `*.wants`/`*.requires` unter systemd u.a. | Status `gesperrt`: nie geschrieben, nie mit Diff gezeigt |
| Symlinks: `quelle` muss nach `EvalSymlinks` im Inventar-Root liegen und wird dann wie ein Ziel gelesen: jeder Bestandteil ab dem aufgelösten Root per `openat` mit `O_NOFOLLOW`, kein zweites `EvalSymlinks` zwischen Prüfung und Lesen; Ziel-Verzeichnis wird aufgelöst und neu geprüft; Ziel und `.bak` dürfen kein Symlink sein (Lstat + `O_NOFOLLOW`) | sonst `nicht-prüfbar` bzw. Fehler; ein nach der Prüfung vertauschter quelle-Ordner (etwa auf `~/.ssh`) wird nicht verfolgt |
| `--repo`-Einträge | nie geschrieben, nie Diff, keine gedruckten Befehle; `rechte=true` oder Ziele außerhalb der Allowlist werden nicht einmal gelesen |
| Diff (`--plan`) | nur für `rechte=false`-Einträge des Plugin-Inventars mit Ziel in der Allowlist |

## Modi-Semantik

### `--plan`
Vergleicht den Quellinhalt aus dem Inventar mit dem Ist-Zustand auf der Festplatte und gibt Status und Diffs aus, ohne Dateien zu verändern. Für Einträge mit `rechte=true` wird nur der Status ausgegeben (z.B. `abweichend`), niemals ein Diff oder Dateiinhalt — solche Ziele sind reale Sicherheits-/Berechtigungsdateien und können Umgebungswerte oder Hooks enthalten, die nicht in ein Transkript oder Log gehören.

### `--apply`
Wendet die Inventareinträge an (nur für Einträge mit `rechte=false` direkt im Ziel):
* `shim`: Schreibt `${HOME}/.local/bin/<id>` (`id` beginnt mit `imprint-`, Modus 0755), ein Shell-Skript mit Markerzeile, das `"${IMPRINT_CORE_ROOT:?}"/'<quelle>' "$@"` aufruft (quelle in einfachen Anführungszeichen; ohne gesetztes `IMPRINT_CORE_ROOT` bricht der Shim ab). Die quelle muss im Root existieren und ausführbar sein. Ersetzt nur einen eigenen Shim (Markerzeile), mit `.bak`.
* `githook`: Führt `git config --local core.hooksPath .githooks` im Plugin-Root aus.
* `copy`: Kopiert `quelle` nach `ziel` (nur unter `${XDG_CONFIG_HOME}/imprint/` oder `${XDG_DATA_HOME}/imprint/`, Modus 0600). Erstellt ein `.bak` bei abweichendem Ziel.
* `systemd`: Zeigt die Unit vor jeder Kopie auf stdout (Steuerzeichen maskiert, jede Zeile mit dem festen Präfix `│ `, damit eine gefälschte Ende-Zeile nicht wirkt) und kopiert sie dann nach `${XDG_CONFIG_HOME}/systemd/user/imprint-<id>.service|.timer` (Modus 0600). Führt niemals `systemctl`-Befehle aus. Gibt es den Namen schon in einem anderen Unit-Pfad, wird nur gedruckt.
* `marketplace` / `mcp`: Druckt den entsprechenden Add-Befehl (shell-gequotet) auf stdout, ohne ihn auszuführen.

Das `.bak` behält die Rechte des Originals. Lässt es sich nicht schreiben (Symlink, Verzeichnis, I/O-Fehler), bleibt das Ziel unverändert und der Eintrag gilt als Fehler. `--apply` endet mit exit 1, sobald ein Eintrag verweigert wurde oder fehlschlug.

#### Rechte-Tor (`rechte=true`)
Für Einträge mit `rechte=true` schreibt `--apply` niemals direkt in `ziel`:

| Eintrag | `--apply` druckt |
|---|---|
| `rechte=true` (verlangt `anwenden`: `ersetzen` oder `fragment-merge`) | nur den Aufruf `<root>/setup/vorlagen/rechte-anwenden.sh <name>`, `<name>` = `id` ohne `-rechte-vorlage` (`claude-rechte-vorlage` → `claude`); das Skript prüft, zeigt den Diff, fragt nach und mergt bei `fragment-merge` tief (jq `*`). Kein `cp`, kein jq, keine Cache-Datei. Fehlt das Skript im Root, gilt der Eintrag als Fehler und es wird nichts gedruckt. |
| systemd-Units mit Namenskollision (ein `rechte=true` ohne `anwenden` ist seit Runde 3 ein ungültiges Inventar, weil es sonst ein ersetzendes `install` bekäme) | Unit-Inhalt unverändert nach `$XDG_CACHE_HOME/imprint/<id>-<basename>` (0600), dann `install -m <modus> ziel ziel.bak` (nur wenn das Ziel existiert) und `install -m <modus> cache ziel`. `install` ersetzt das Ziel, statt durch einen dort liegenden Link zu schreiben (gemessen mit uutils coreutils 0.12.0); zusätzlich prüft `--apply` Ziel und `.bak` vorher (kein Symlink, kein zweiter Hardlink), sonst ist der Eintrag ein Fehler und es wird nichts gedruckt. `<modus>` ist der Modus des vorhandenen Ziels, sonst 0600, plus Ausführungsbits dort, wo Lesebits stehen, wenn die quelle ausführbar ist (ein Skript bleibt ausführbar, eine 0600-Datei wird nicht aufgeweitet). |

#### Warum systemd nicht grundsätzlich `rechte=true` ist
Eine User-Unit führt Code erst aus, wenn der Mensch sie mit `systemctl --user enable/start` aktiviert; `setup` ruft `systemctl` nie auf. Die Wege, auf denen eine Kopie ohne diesen Schritt Code ausführt, sind geschlossen: eine gleichnamige Unit in einem anderen Unit-Pfad (sie könnte schon aktiv sein oder würde überdeckt) macht den Eintrag druck-only, und der Präfix `imprint-` schließt fremde Namen aus. Bleibt das Aktualisieren einer schon aktivierten eigenen `imprint-*`-Unit: dafür zeigt `--apply` jede Unit vor dem Schreiben und legt ein `.bak` an. Das ist dieselbe Vertrauensgrenze wie das Plugin selbst, dessen Hooks ohnehin Code aus dem identitätsgeprüften `--root` ausführen; ein grundsätzliches `rechte=true` brächte einen Handgriff mehr, aber keinen geschlossenen Weg mehr.

#### Env-Guard
`--apply` verweigert die Ausführung (exit 2), wenn `CLAUDECODE` oder eine Umgebungsvariable mit Präfix `ANTIGRAVITY_` oder `AGY_` gesetzt ist, auch mit leerem Wert ("apply führt der Mensch aus"). Der Guard ist nur beratend; die Grenze liegt in den Pfadprüfungen.

### `--check`
Vergleicht den Soll-Zustand mit dem Ist-Zustand im System, ohne Dateien oder Cache-Dateien zu schreiben. Wie bei `--plan` gilt für `rechte=true`-Einträge: nur der Status wird ausgegeben, nie ein Diff oder Dateiinhalt.
* Exit 0: Kein Eintrag, den `setup` selbst anwendet, hat Drift.
* Exit 1: Mindestens ein solcher Eintrag driftet (`fehlt`, `abweichend`, `gesperrt`, `verweigert` oder `nicht-prüfbar`), oder das mit `--repo` angegebene Inventar ist nicht prüfbar (fehlt, vertippter Pfad, Symlink, unlesbar).

Einträge, die nur der Mensch anwendet (Plugin-Einträge mit `rechte=true` und systemd-Units mit Namenskollision), zeigen ihren Vergleichsstatus mit dem Zusatz `(Mensch)`, zählen aber nicht als Drift, solange ihre Pfadprüfung besteht. Grund: der Mensch setzt Platzhalter wie `${PROJEKTE}` selbst ein und mergt Fragmente, sodass eine Abweichung kein Fehler ist, den `setup` beurteilen kann; ein dauerhaftes exit 1 würde echte Drift der übrigen Einträge verdecken. Für `anwenden=fragment-merge` ist der Status `gleich`, wenn ein tiefer Merge der Vorlage (jq `*`) das Ziel nicht ändert.

## Inventar-Schema (`inventar.json`)

Das Inventar besteht aus einer JSON-Liste von Eintrags-Objekten. Jeder Eintrag erfordert die folgenden Felder:

* `id` (String): Eindeutige Kennung des Eintrags, `^[a-z0-9][a-z0-9-]{0,63}$`.
* `typ` (String): Einer der erlaubten Typen: `shim`, `githook`, `copy`, `rechte-vorlage`, `marketplace`, `mcp`, `systemd`.
* `quelle` (String): Relativer Pfad zur Quelldatei innerhalb des jeweiligen Inventar-Roots.
* `ziel` (String): Zielpfad im Dateisystem. Kann nur `${HOME}`, `${XDG_CONFIG_HOME}`, `${XDG_CACHE_HOME}` und `${XDG_DATA_HOME}` enthalten; für `githook`, `marketplace` und `mcp` wird es nicht verwendet.
* `rechte` (Boolean): Markiert Sicherheits- oder Berechtigungsdateien (`true`/`false`).
* `anwenden` (String, bei `rechte=true` Pflicht, sonst optional): `ersetzen` (Standard, die ganze Zieldatei) oder `fragment-merge` (nur mit `rechte=true` und `.json`-Ziel: nur die Schlüssel der Vorlage, tief gemergt, alle anderen bleiben). Bei `rechte=true` druckt `--apply` nur den Skript-Aufruf (siehe Rechte-Tor).
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
* `… (Mensch)`: Zusatz für Einträge, die nur der Mensch anwendet; `--check` zählt sie nicht als Drift.
* `verweigert`: Ziel liegt außerhalb der erlaubten Wurzeln oder der Allowlist des `typ`.
* `Fehler`: `--apply` konnte den Eintrag nicht anwenden (Details auf stderr).
