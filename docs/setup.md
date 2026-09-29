# Setup-Inventar und `imprint-dev setup`

Das Werkzeug `imprint-dev setup --plan` vergleicht Dateien aus dem lokalen Setup-Inventar (`setup/inventar.json`) mit den Zielpfaden im System, ohne Änderungen auf der Festplatte vorzunehmen.

## Befehlsaufruf

```sh
imprint-dev setup --plan [--root dir] [--repo path]
```

Optionen:
* `--plan`: Führt den Vergleich aus und gibt die Status-Tabelle aus. Pflichtflag.
* `--root dir`: Wurzelverzeichnis für das Hauptinventar `setup/inventar.json` (Standard: `.`).
* `--repo path`: Optionales Pfad-Argument zu einem Projekt-Repository. Falls angegeben, wird zusätzlich `<repo>/.imprint/setup.json` gelesen.

## Inventar-Schema (`inventar.json`)

Das Inventar besteht aus einer JSON-Liste von Eintrags-Objekten. Jeder Eintrag erfordert die folgenden Felder:

* `id` (String): Eindeutige Kennung des Eintrags.
* `typ` (String): Einer der erlaubten Typen: `shim`, `githook`, `copy`, `rechte-vorlage`, `marketplace`, `mcp`, `systemd`.
* `quelle` (String): Relativer Pfad zur Quelldatei innerhalb des jeweiligen Inventar-Roots.
* `ziel` (String): Zielpfad im Dateisystem. Kann Umgebungsvariablen wie `${HOME}` und `${XDG_CONFIG_HOME}` enthalten.
* `rechte` (Boolean): Markiert Sicherheits- oder Berechtigungsdateien (`true`/`false`).
* `beschreibung` (String): Kurze Beschreibung des Eintrags.

## Status-Werte

Für jeden Eintrag wird genau ein Status ermittelt:

* `fehlt`: Die Zieldatei existiert nicht.
* `gleich`: Quelle und Ziel sind byte-identisch.
* `abweichend`: Quelle und Ziel unterscheiden sich (ein kurzer Diff wird ausgegeben).
* `nicht-prüfbar`: Die Quell- oder Zieldatei ist nicht lesbar, ein Verzeichnis oder blockiert den Vergleich anderweitig.
