# Rechte-Vorlagen für Claude Code und agy

Zwei Vorlagen mit den geprüften Rechte-Regeln aus einer angewendeten, getesteten Konfiguration.
**Nur der Mensch wendet sie an** (Nutzerentscheid CL-119/CL-123), mit dem geprüften Skript
`setup/vorlagen/rechte-anwenden.sh` (siehe „Anwenden" unten) — nie mit einem eigenen `cp` von
Hand. `imprint-dev setup --apply` gibt es seit PR 38, aber es schreibt ein Rechte-Ziel
(`rechte: true`, wie beide Einträge hier) nie selbst: für so ein Ziel rendert `--apply` die
Vorlage nur in eine Cache-Datei unter `$XDG_CACHE_HOME/imprint` und druckt zwei `cp`-Befehle
(Sicherung, dann Cache → Ziel) auf stdout; ausgeführt wird davon nichts (siehe
`tools/imprint-dev/setup.go`, `runSetupApply` und `writeRechteCache`). Gemessen gegen `gh pr
diff 38` am 2026-09-30 druckt `--apply` diese zwei `cp`-Befehle für **jeden** Rechte-Ziel-
Eintrag gleich, unabhängig vom Feld `anwenden` im Inventar — auch für `claude-rechte.json`
(`anwenden: fragment-merge`). Der zweite, gedruckte `cp`-Befehl kopiert dabei roh und kennt
keinen Fragment-Merge; ihn für `claude-rechte.json` auszuführen würde `hooks`, `env`,
`enabledPlugins`, `sandbox` und `permissions.defaultMode` aus der bestehenden Datei löschen.
Deshalb bleibt für `claude-rechte.json` weiterhin `setup/vorlagen/rechte-anwenden.sh claude`
(bzw. dessen `jq -s '.[0] * .[1]'`-Muster) Pflicht, nie der von `--apply` gedruckte `cp`-Befehl
— das gilt so lange, bis PR 38 oder eine Folge-Änderung das Feld `anwenden: fragment-merge`
selbst auswertet und für einen solchen Eintrag keinen rohen `cp` mehr druckt (siehe
Merge-Reihenfolge im Pull-Request-Text). Beide Vorlagen tragen trotzdem weiterhin eine eigene
Deny-Regel gegen den `--apply`-Aufruf selbst — vorsorglich, damit kein Agent auch nur die
Cache-Datei erzeugen oder die gedruckten Befehle selbst ausführen kann.

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
* `${PROJEKTE}` — Ordner der Repo-Checkouts, vom Menschen beim Anwenden per Umgebungsvariable
  gesetzt (z. B. `${PROJEKTE}/imprint`); kein im Repository fest eincodierter Rechnerpfad.
* `${TRUSTED_WORKSPACE}` — der Wert für `trustedWorkspaces`; absichtlich ohne Vorbelegung
  auf `${HOME}`, damit der Mensch beim Anwenden bewusst entscheidet, wie weit dieser Eintrag
  reicht, statt eine stillschweigende Vorbelegung zu übernehmen.

**`imprint-dev setup --plan` und `--apply` lösen nur `${HOME}` und `${XDG_CONFIG_HOME}` (bzw.
`${XDG_CACHE_HOME}`/`${XDG_DATA_HOME}`) auf** (siehe `tools/imprint-dev/setup.go`, `expandZiel`
für `ziel`, `renderTemplate` für den Vorlagen-Inhalt; `--apply` nutzt denselben `replacer` wie
`--plan`). `${PROJEKTE}` und `${TRUSTED_WORKSPACE}` sind reine Textplatzhalter in den
Vorlagen-Inhalten; sie werden von keinem der beiden Unterbefehle ersetzt, sondern müssen vor dem
Anwenden von Hand oder mit `envsubst` gesetzt sein. Deshalb zeigen `setup --plan` und die
gedruckten `--apply`-Befehle diese beiden Einträge immer als `abweichend` — das ist erwartet,
nicht ein Fehler der Vorlage (siehe Abweichungen unten).

`statusLine.command` in `agy-settings.json` zeigt auf `${HOME}/.gemini/antigravity-cli/statusline.py`,
nicht auf einen Checkout-Pfad: agy läuft das Kommando direkt aus, und ein Checkout-Pfad wäre nach
einem `git worktree remove` oder einem Verschieben des Klons weg. Beim Anwenden kopiert der Mensch
`setup/vorlagen/statusline.py` selbst dorthin (siehe „Anwenden" unten); das Inventar trägt dafür
den Eintrag `agy-statusline` (Typ `copy`, `rechte: true` — agy führt diese Datei direkt aus, wer
sie schreibt, führt damit Code in agys Kontext aus; das ist Rechte-Inhalt, nicht nur ihr Aufrufpfad
in `agy-settings.json`). Der Mensch-Schritt zum Kopieren steht in „Anwenden" oben.

## Anwenden (nur der Mensch)

Das frühere Rezept aus `cp`/`jq`/`envsubst`-Zeilen zum Abtippen ist durch ein geprüftes Skript
ersetzt, `setup/vorlagen/rechte-anwenden.sh` (`bash`, `set -euo pipefail`). Es hat zwei
Unterbefehle, einen je Vorlage — ausgeführt wird nach wie vor nur vom Menschen, nie automatisch:

```sh
setup/vorlagen/rechte-anwenden.sh claude
setup/vorlagen/rechte-anwenden.sh agy
```

Beide Unterbefehle:

* prüfen zuerst, dass `jq` und `envsubst` vorhanden sind, lösen die Vorlagendatei relativ zum
  eigenen Skriptpfad auf (nicht zum Arbeitsverzeichnis) und verlangen eine existierende,
  gültige JSON-Zieldatei (`jq -e .`); `agy` verlangt zusätzlich, dass `PROJEKTE` und
  `TRUSTED_WORKSPACE` gesetzt und nicht leer sind (`:?`);
* zeigen den Diff und fragen ausdrücklich nach (`Anwenden? [j/N]`); ohne `j`/`J` bricht das
  Skript ohne jede Änderung ab;
* legen bei Zustimmung zuerst eine Sicherung mit sekundengenauem Zeitstempel neben der
  Zieldatei an (`cp -p -n`), erhalten die Rechte der Zieldatei (`chmod --reference`) und
  übernehmen die neue Datei erst danach per atomarem `mv`;
* schreiben, wenn die Zieldatei ein Symlink ist, auf die Datei, auf die er zeigt
  (`readlink -f`) — der Symlink selbst bleibt erhalten.

`claude` mergt den `permissions`-Block aus `claude-rechte.json` per `jq -s '.[0] * .[1]'` in die
bestehende `~/.claude/settings.json`. Das ist ein **tiefer** Merge: `permissions.defaultMode`
und alle anderen Top-Level-Schlüssel (`hooks`, `env`, `enabledPlugins`, `sandbox`, …) bleiben
unverändert, aber Listen wie `permissions.allow`/`permissions.deny` werden dabei **vollständig
durch die Vorlage ersetzt**, nicht mit dem Bestand vereinigt — ein `jq *`-Merge überschreibt
gleichnamige Schlüssel, statt ihre Arrays zusammenzuführen. Vor dem Übernehmen prüft das Skript
zur Plausibilität, dass `permissions.deny` im Ergebnis nicht leer ist.

`agy` ersetzt `~/.gemini/antigravity-cli/settings.json` komplett durch `agy-settings.json` mit
`envsubst` für `${PROJEKTE}`/`${TRUSTED_WORKSPACE}` (validiert danach per `jq -e .`, dass das
Ergebnis gültiges JSON ist und kein unersetztes `${` mehr enthält) und installiert
`setup/vorlagen/statusline.py` per `install -m 0755` nach
`~/.gemini/antigravity-cli/statusline.py`.

Laufende agy-Hubs schreiben eine geänderte `settings.json` sonst zurück (gemessen 2026-09-30,
00:49; die beendete Prozesszeile lautete "…/.gemini/bin/agy --hub …"). `agy` zeigt dafür vor dem
Diff einen Hinweis inklusive `pgrep`-Kontrolle; das Skript tötet selbst keinen Prozess — ein
`pkill`-Muster wie `agy --hub` kann auch Hubs anderer, unbeteiligter Projekte treffen, und ein
getöteter Prozess lässt sich über `rueckbau` nicht rückgängig machen:

```sh
pgrep -af -- '--hub'    # Kontrolle: welche Hubs laufen gerade
pkill -f 'agy --hub'    # bei Bedarf gezielt beenden, dann erst mit "j" bestätigen
```

## Rückbau

```sh
setup/vorlagen/rechte-anwenden.sh rueckbau <Zeitstempel>
```

`claude` und `agy` erzeugen je einen eigenen Zeitstempel (zwei getrennte Aufrufe, siehe
„Anwenden" oben, jeweils in der letzten Ausgabezeile). `rueckbau <Zeitstempel>` spielt zurück,
was zu genau diesem Zeitstempel tatsächlich gesichert wurde — `~/.claude/settings.json`, wenn
dazu ein `claude`-Lauf existiert, `~/.gemini/antigravity-cli/settings.json`, wenn dazu ein
`agy`-Lauf existiert — und bricht ohne jede Änderung ab, wenn zu diesem Zeitstempel gar keine
Sicherung existiert. `statusline.py` gehört nur zu einem `agy`-Lauf: nur wenn zum selben
Zeitstempel auch `settings.json.bak-<Zeitstempel>` von `agy` existiert, entscheidet das Skript,
ob `statusline.py` zurückgespielt wird (Sicherung vorhanden) oder entfernt (existierte vor dem
Anwenden nicht) — ein `rueckbau` zu einem `claude`-Zeitstempel fasst `statusline.py` gar nicht
erst an. Auch hier zuerst der Hub-Hinweis: ein laufender Hub würde die soeben zurückgespielte
Sicherung sonst erneut mit dem zwischenzeitlich angewendeten Stand überschreiben.

## Testskript

`agy-rechte-test.sh` prüft eine bereits angewendete `agy-settings.json` gegen die Fälle T1–T8
plus T6b (Lesen erlaubt/verweigert, Schreiben erlaubt/verweigert, `systemctl`-Teilfreigabe, `git
push --force` verweigert). Es wendet selbst nichts an. Nur drei Pflicht-Umgebungsvariablen kommen
von außen (Aufruf und Variablen stehen im Skriptkopf) — das Skript liegt in einem öffentlichen
Repository und trägt deshalb keinen Rechner- oder Projektnamen fest eincodiert. Die Zielpfade für
T2, T5, T6, T6b, T7 und T8 (Köder-Dateien, Wegwerf-Repo, Schreibziele) erzeugt das Skript sich
selbst mit `mktemp`, prüft für T6/T6b vorher, dass der generierte Pfad noch nicht existiert, und
löscht danach nur, was es selbst nachweislich angelegt hat — nie eine echte, vom Menschen
mitgegebene Datei. T5 läuft ausdrücklich unter `--mode accept-edits`, weil genau dieser Modus die
`write_file`-Regeln für ein erlaubtes Ziel umgeht (siehe nächster Abschnitt); T6b wiederholt T6s
Schreibversuch außerhalb jedes Worktrees zusätzlich unter demselben Modus, weil dort ebenfalls
verweigert werden muss, nicht nur ohne die Flag.

T2 und T8 zählen zusätzlich als FAIL, sobald der Köder-Inhalt selbst in der `agy`-Ausgabe
auftaucht — unabhängig davon, ob `denied_actions` eine Ablehnung meldet: eine gemeldete Ablehnung
neben einem geleakten Inhalt ist kein Bestehen. Bekannte Grenze: ein Umweg über `command(cat)`,
`command(head)` oder `command(rg)`/`command(grep)` auf dieselbe Köder-Datei probt diese Skript
nicht gesondert (siehe „Was welche Regel durchsetzt" unten).

```sh
AGY_TEST_WORKDIR=${PROJEKTE}/imprint \
AGY_TEST_ALLOWED_DIR=${PROJEKTE}/imprint-wt \
AGY_TEST_SERVICE=imprint-oberflaeche.service \
  setup/vorlagen/agy-rechte-test.sh
```

Leere oder nicht als JSON lesbare `agy`-Ausgabe zählt in keinem der acht Fälle als Ablehnung: das
Skript verlangt zuerst gültiges JSON mit einem gesetzten `status`-Feld, und prüft für die
Lese-Verweigerungen (T2, T8), die Schreib-Verweigerung (T6) und die Kommando-Verweigerungen (T4,
T7) zusätzlich, dass die zurückgemeldeten `denied_actions` tatsächlich `read_file`, `write_file`
beziehungsweise `command` erkennen lassen. Das genaue Feldlayout von `denied_actions` ist dabei nicht dokumentiert bekannt — nur gegen
eine reale `agy`-Ausgabe als Bestehen/Nichtbestehen gemessen, nie gegen ein Schema —, deshalb prüft
das Skript das per Teilstring-Test auf die stringifizierte Form, nicht auf einen konkreten
Schlüssel.

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
* Die `command(imprint-dev setup --apply)`-/`command(go run ./tools/imprint-dev setup --apply)`-
  und die `Bash(imprint-dev setup *--apply*)`-/`Bash(go run ./tools/imprint-dev setup *--apply*)`-
  Deny-Regeln sind vorsorglich: den Unterbefehl `--apply` gibt es seit PR 38, aber er schreibt
  ein Rechte-Ziel nie selbst — für so ein Ziel rendert er nur eine Cache-Datei und druckt zwei
  `cp`-Befehle, ausgeführt wird nichts (siehe Kopf dieser Datei und `runSetupApply` in
  `tools/imprint-dev/setup.go`). Die Deny-Regeln bleiben trotzdem stehen, damit kein Agent auch
  nur diesen Cache-Schritt anstößt oder die gedruckten `cp`-Befehle selbst ausführt. Auf der
  `agy`-Seite matchen die Regeln nur den exakten, wörtlichen
  Präfix (**nicht geprüft**, ob agy Glob-Wildcards in `command(...)` überhaupt unterstützt); ein
  anderer Aufrufweg zum selben Unterbefehl (ein drittes Verzeichnis-Präfix, ein Alias, ein
  Wrapper-Skript) träfe keine der beiden Formen. Auf der `claude-rechte.json`-Seite nutzen die
  Regeln dieselbe Mid-String-Wildcard-Schreibweise wie `Bash(gh pr merge *--admin*)` und decken
  damit auch eine andere Flag-Reihenfolge ab (z. B. `imprint-dev setup --root . --apply`) — auch
  das ist nur nach demselben, ungeprüften Muster gebaut, nicht an echtem Claude-Code-Verhalten
  gemessen.
* Dieses Tor ist Hygiene, keine Sandbox: mit den oben stehenden `allow`-Einträgen bleiben mehrere
  Umgehungen offen, keine davon durch eine Regel in einer der beiden Vorlagen verhindert.
  * `find … -fprintf out %p\n` und `sort -o <ziel>` schreiben Dateien über zwei erlaubte Befehle,
    an jeder `write_file`-Regel vorbei.
  * `find … -delete` löscht Dateien über einen erlaubten Befehl, an der `command(rm)`-Deny-Regel
    vorbei.
  * `command(go run)` ist uneingeschränkt erlaubt: beliebiger Go-Code lässt sich damit ausführen,
    unabhängig von jeder anderen Regel in dieser Datei.
  * `git push +refs/heads/x:y` (die `+`-Syntax für Force-Push) läuft an den wörtlichen
    `git push --force`/`-f`/`--force-with-lease`-Deny-Einträgen vorbei.
  * In `claude-rechte.json` deckt kein Eintrag Bash allgemein ab (anders als `agy-settings.json`
    mit `command(bash -c)`/`command(sh -c)`); jeder der obigen Wege steht Claude über sein
    Bash-Werkzeug offen, auch wenn `Edit`/`Write` auf die Settings-Dateien selbst verweigert sind.
  * `command(cat)`/`command(head)`/`command(rg)`/`command(grep)` sind erlaubt und lesen dieselbe
    Köder-Datei, die eine `read_file`-Deny-Regel blockiert — ein Umweg um genau diese Regel.
    `agy-rechte-test.sh` probt diesen Umweg nicht gesondert; T2/T8 prüfen nur den `read_file`-Weg
    (siehe „Testskript" oben).
* Dass diese Vorlagen überhaupt nur von Hand angewendet werden (nie automatisch durch ein
  Werkzeug in diesem Repository), durchsetzt nichts im Code — das ist allein der Nutzerentscheid
  CL-119/CL-123, hier nur dokumentiert.

## Abweichungen von der Quelle

* `defaultMode: "auto"` aus `~/.claude/settings.json` ist nicht in `claude-rechte.json`
  übernommen — der Auftrag verlangte ausdrücklich nur den `permissions`-Block.
* Die beiden `rechte-vorlage`-Einträge in `setup/inventar.json` zeigen bei `imprint-dev setup
  --plan` dauerhaft den Status `abweichend`: die Vorlagen tragen Platzhalter bzw. (bei
  `claude-rechte.json`) nur einen Ausschnitt der Zieldatei, nie deren Bytes. Der `copy`-Eintrag
  `agy-statusline` trägt keine Platzhalter und zeigt `fehlt`, solange `statusline.py` noch nicht
  kopiert wurde, danach `gleich`.
