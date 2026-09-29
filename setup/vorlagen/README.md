# Rechte-Vorlagen für Claude Code und agy

Zwei Vorlagen mit den geprüften Rechte-Regeln aus einer angewendeten, getesteten Konfiguration.
**Nur der Mensch wendet sie an** (Nutzerentscheid CL-119/CL-123). Kein Werkzeug in diesem
Repository schreibt automatisch in eine echte Settings-Datei; `imprint-dev setup --apply` gibt
es noch nicht (nur `--plan`), und beide Vorlagen tragen zusätzlich eine eigene Deny-Regel gegen
genau diesen Befehl — vorsorglich, für den Tag, an dem er kommt.

## Die zwei Vorlagen

| Datei | Vorlage für | Deckt ab |
| --- | --- | --- |
| `agy-settings.json` | `${HOME}/.gemini/antigravity-cli/settings.json` (ganze Datei) | `permissions`, `statusLine`, `trustedWorkspaces` |
| `claude-rechte.json` | den Block `permissions` in `${HOME}/.claude/settings.json` (nur dieser Block, nicht die ganze Datei) | `permissions.allow`, `permissions.deny` |

`agy-settings.json` stammt aus der Fassung, die der Nutzer am 2026-09-30 angewendet hat und die
die Tests T1–T8 bestanden hat (siehe `agy-rechte-test.sh`). `claude-rechte.json` stammt aus dem
`permissions`-Block der eigenen `~/.claude/settings.json`; dieser Block enthielt keine
Rechnerpfade, daher trägt die Vorlage außer den unten beschriebenen Deny-Ergänzungen keine
Platzhalter. Hooks, env, Modelle, `defaultMode` und alles sonst Persönliche aus beiden
Quelldateien bleiben draußen — auch `defaultMode: "auto"` aus der Quelle, das ist eine bewusste
Abweichung, keine Auslassung durch Versehen.

## Platzhalter

* `${HOME}` — Home-Verzeichnis.
* `${PROJEKTE}` — Ordner der Repo-Checkouts (bei der Quelle: `~/Projekte`).
* `${TRUSTED_WORKSPACE}` — der Wert für `trustedWorkspaces`; absichtlich ohne Vorbelegung
  auf `${HOME}`, damit der Mensch beim Anwenden bewusst entscheidet, wie weit dieser Eintrag
  reicht, statt eine stillschweigende Vorbelegung zu übernehmen.

**`imprint-dev setup --plan` löst nur `${HOME}` und `${XDG_CONFIG_HOME}` in `ziel` auf** (siehe
`tools/imprint-dev/setup.go`, `expandZiel`). `${PROJEKTE}` und `${TRUSTED_WORKSPACE}` sind reine
Textplatzhalter in den Vorlagen-Inhalten; sie werden nicht vom Werkzeug ersetzt, sondern von Hand
oder mit `envsubst` beim Anwenden. Deshalb zeigt `setup --plan` diese beiden Einträge immer als
`abweichend` — das ist erwartet, nicht ein Fehler der Vorlage (siehe Abweichungen unten).

`statusLine.command` in `agy-settings.json` zeigt auf die mitgelieferte
`${PROJEKTE}/imprint-core/setup/vorlagen/statusline.py` — die Kopie läuft direkt aus dem
Checkout, es muss nichts zusätzlich nach `~/.gemini/antigravity-cli/` kopiert werden.

## Anwenden (nur der Mensch)

```sh
pkill -f 'agy --hub'   # sonst schreiben laufende Hubs den alten Stand zurück (gemessen 2026-09-30)

cp ~/.gemini/antigravity-cli/settings.json ~/.gemini/antigravity-cli/settings.json.bak-$(date +%F)
cp ~/.claude/settings.json ~/.claude/settings.json.bak-$(date +%F)

HOME="$HOME" PROJEKTE="$HOME/Projekte" TRUSTED_WORKSPACE="$HOME" \
  envsubst '${HOME} ${PROJEKTE} ${TRUSTED_WORKSPACE}' \
  < setup/vorlagen/agy-settings.json > ~/.gemini/antigravity-cli/settings.json

# claude-rechte.json liefert nur den permissions-Block; von Hand in die bestehende
# ~/.claude/settings.json einfügen/mergen (z. B. mit jq), nicht überschreiben.
```

`envsubst` mit der eingeschränkten Variablenliste lässt jedes andere `$`-Zeichen in den Dateien
unverändert.

## Rückbau

```sh
pkill -f 'agy --hub'   # wieder zuerst, sonst schreibt ein laufender Hub die soeben
                         # zurückgespielte Sicherung erneut mit dem angewendeten Stand zurück
cp ~/.gemini/antigravity-cli/settings.json.bak-<Datum> ~/.gemini/antigravity-cli/settings.json
cp ~/.claude/settings.json.bak-<Datum> ~/.claude/settings.json
```

## Testskript

`agy-rechte-test.sh` prüft eine bereits angewendete `agy-settings.json` gegen die Fälle T1–T8
(Lesen erlaubt/verweigert, Schreiben erlaubt/verweigert, `systemctl`-Teilfreigabe, `git push
--force` verweigert). Es wendet selbst nichts an. Alle Pfade kommen über Pflicht-Umgebungsvariablen
herein (Aufruf und Variablen stehen im Skriptkopf) — das Skript liegt in einem öffentlichen
Repository und trägt deshalb keinen Rechner- oder Projektnamen fest eincodiert. T5 läuft
ausdrücklich unter `--mode accept-edits`, weil genau dieser Modus die `write_file`-Regeln umgeht
(siehe nächster Abschnitt).

```sh
AGY_TEST_WORKDIR=~/Projekte/imprint \
AGY_TEST_ALLOWED_DIR=~/Projekte/imprint-wt \
AGY_TEST_DENIED_WRITE=~/agy-rechte-test-verboten.txt \
AGY_TEST_DENIED_READ_1=/pfad/ausserhalb/jeder/allow-liste \
AGY_TEST_DENIED_READ_2=/anderer/pfad/ausserhalb/jeder/allow-liste \
AGY_TEST_SERVICE=imprint-oberflaeche.service \
  setup/vorlagen/agy-rechte-test.sh
```

## Was welche Regel durchsetzt

* Die `write_file(...)`-Deny-Regeln auf beide Settings-Dateien in `agy-settings.json` sind
  `write_file`-Regeln derselben Art, die am 2026-09-30 als wirkungslos gemessen wurden (Schreiben
  läuft unter `--mode accept-edits` am `write_file`-Mechanismus vorbei). Ihre Wirkung unter
  diesem Modus ist **nicht geprüft** — sie stehen als dokumentierte Absicht drin, nicht als
  gemessene Durchsetzung.
* Die `Edit(...)`/`Write(...)`-Deny-Regeln in `claude-rechte.json` decken nur die Werkzeuge Edit
  und Write ab. Ein Schreiben über `Bash` (z. B. `bash -c 'echo ... > ~/.claude/settings.json'`)
  läuft an dieser Regel vorbei; dagegen steht die generische `command(bash -c)`/`command(sh -c)`
  -Deny-Regel in `agy-settings.json`, aber `claude-rechte.json` selbst hat kein generisches
  `Bash(bash -c:*)`-Deny.
* Die `command(imprint-dev setup --apply)`- und `Bash(imprint-dev setup --apply:*)`-Deny-Regeln
  sind vorsorglich: den Unterbefehl `--apply` gibt es im Werkzeug noch nicht (Stand 2026-09-30,
  nur `--plan`). Beide Regeln matchen zudem nur den exakten Präfix; eine andere Flag-Reihenfolge
  (z. B. `imprint-dev setup --root . --apply`) träfe die Regel nicht — das ist eine
  Präfix-Regel, kein Parser.
* Dass diese Vorlagen überhaupt nur von Hand angewendet werden (nie automatisch durch ein
  Werkzeug in diesem Repository), durchsetzt nichts im Code — das ist allein der Nutzerentscheid
  CL-119/CL-123, hier nur dokumentiert.

## Abweichungen von der Quelle

* `defaultMode: "auto"` aus `~/.claude/settings.json` ist nicht in `claude-rechte.json`
  übernommen — der Auftrag verlangte ausdrücklich nur den `permissions`-Block.
* Beide neuen Einträge in `setup/inventar.json` zeigen bei `imprint-dev setup --plan` dauerhaft
  den Status `abweichend`: die Vorlagen tragen Platzhalter bzw. (bei `claude-rechte.json`) nur
  einen Ausschnitt der Zieldatei, nie deren Bytes.
