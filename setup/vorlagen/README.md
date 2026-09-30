# Rechte-Vorlagen für Claude Code und agy

Drei Vorlagen mit den geprüften Rechte-Regeln aus einer angewendeten, getesteten Konfiguration.
**Nur der Mensch wendet sie an** (Nutzerentscheid CL-119/CL-123), mit dem geprüften Skript
`setup/vorlagen/rechte-anwenden.sh` (siehe „Anwenden" unten) — nie mit einem eigenen `cp` von
Hand. Alle drei Einträge tragen in `setup/inventar.json` ein Feld `anwenden`
(`agy-rechte-vorlage`/`agy-statusline`: `ersetzen`, `claude-rechte-vorlage`: `fragment-merge`);
für so einen Eintrag (`rechte: true` und `anwenden` gesetzt) schreibt `imprint-dev setup --apply`
nie selbst und rendert auch keine Cache-Datei — es druckt nur den passenden Aufruf von
`setup/vorlagen/rechte-anwenden.sh` (Argument: die id ohne die Endung `-rechte-vorlage`, siehe
`rechteSkriptCall` in `tools/imprint-dev/setup.go`); ausgeführt wird davon nichts. Gemessen gegen
`gh pr diff 38`, Head `bfc0ecb`, 2026-09-30 (Momentaufnahme, kann bei einem späteren PR-38-Stand
schon abweichen): `--apply` druckt für `agy-rechte-vorlage` `setup/vorlagen/rechte-anwenden.sh
agy`, für `claude-rechte-vorlage` `setup/vorlagen/rechte-anwenden.sh claude` und für
`agy-statusline` `setup/vorlagen/rechte-anwenden.sh agy-statusline` — jeder dieser drei
gedruckten Aufrufe funktioniert (siehe „Anwenden" unten). Beide `settings.json`-Vorlagen tragen
trotzdem weiterhin eine eigene Deny-Regel gegen den `--apply`-Aufruf selbst — vorsorglich, damit
kein Agent auch nur diesen gedruckten Aufruf selbst ausführt.

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
Vorlagen-Inhalten; sie werden von keinem der beiden Unterbefehle ersetzt, sondern müssen als
Umgebungsvariablen gesetzt sein, bevor `setup/vorlagen/rechte-anwenden.sh agy` (bzw.
`agy-statusline`) sie per `envsubst` einsetzt (siehe „Anwenden" unten). Deshalb zeigt
`setup --plan` (und damit auch der `--apply`-Aufruf für `agy-rechte-vorlage`, siehe oben) diesen
einen Eintrag immer als `abweichend` — das ist erwartet, nicht ein Fehler der Vorlage (siehe
Abweichungen unten). `claude-rechte-vorlage` und `agy-statusline` tragen dagegen keine solchen
Platzhalter; nach dem Anwenden zeigt `--plan` für beide `gleich` (gemessen 2026-09-30 gegen
`imprint-dev38 setup --plan`, Head `bfc0ecb`).

`statusLine.command` in `agy-settings.json` zeigt auf `${HOME}/.gemini/antigravity-cli/statusline.py`,
nicht auf einen Checkout-Pfad: agy läuft das Kommando direkt aus, und ein Checkout-Pfad wäre nach
einem `git worktree remove` oder einem Verschieben des Klons weg. Beim Anwenden installiert
`setup/vorlagen/rechte-anwenden.sh agy-statusline` (eigenständig, oder gleichwertig als Teil von
`agy`) `setup/vorlagen/statusline.py` dorthin (siehe „Anwenden" unten); das Inventar trägt dafür
den Eintrag `agy-statusline` (Typ `copy`, `rechte: true`, `anwenden: ersetzen` — agy führt diese
Datei direkt aus, wer sie schreibt, führt damit Code in agys Kontext aus; das ist Rechte-Inhalt,
nicht nur ihr Aufrufpfad in `agy-settings.json`).

## Anwenden (nur der Mensch)

Das frühere Rezept aus `cp`/`jq`/`envsubst`-Zeilen zum Abtippen ist durch ein geprüftes Skript
ersetzt, `setup/vorlagen/rechte-anwenden.sh` (`bash`, `set -euo pipefail`). Es hat drei
Anwenden-Unterbefehle — je einen für `agy-rechte-vorlage`, `claude-rechte-vorlage` und
`agy-statusline` in `setup/inventar.json`, das sind genau die Aufrufe, die `imprint-dev setup
--apply` für diese drei Einträge druckt (siehe oben) — ausgeführt wird nach wie vor nur vom
Menschen, nie automatisch:

```sh
setup/vorlagen/rechte-anwenden.sh claude
setup/vorlagen/rechte-anwenden.sh agy
setup/vorlagen/rechte-anwenden.sh agy-statusline
```

Alle drei Unterbefehle:

* prüfen zuerst, dass die benötigten Werkzeuge (u. a. `mktemp`, `readlink`, `chmod`, `diff`,
  `cp`, `mv`, `install`, `cmp`, `sha256sum`; `claude`/`agy` zusätzlich `jq`/`envsubst`) vorhanden
  sind, lösen die Vorlagendatei relativ zum eigenen Skriptpfad auf (nicht zum Arbeitsverzeichnis)
  und verlangen bei `claude`/`agy` eine existierende, gültige JSON-Zieldatei mit genau einem
  Dokument (`jq -e -s 'length==1'` — eine Zieldatei mit mehr als einem aneinandergehängten
  JSON-Dokument wird abgelehnt); `agy` verlangt zusätzlich, dass `PROJEKTE` und
  `TRUSTED_WORKSPACE` gesetzt und nicht leer sind (`:?`); `agy-statusline` braucht keine der
  beiden Variablen;
* lösen den ganzen Zielpfad zuerst kanonisch auf (`readlink -f`, nicht nur wenn die Zieldatei
  selbst ein Symlink ist — ein Zwischenverzeichnis wie `~/.claude` kann ebenso gut einer sein)
  und brechen erst danach ab, wenn dieser aufgelöste Pfad in den eigenen Checkout dieses Skripts
  zeigt — ein Symlink von `~/.gemini/antigravity-cli/statusline.py`, `~/.claude` oder
  `~/.claude/settings.json` in eine versionierte Datei im Repository würde sonst genau diese
  Datei überschreiben, statt der persönlichen Konfiguration;
* prüfen für jedes in diesem Aufruf mögliche Ziel vorab, dass eine schon vorhandene Zieldatei
  eine reguläre, beschreibbare Datei ist (kein Verzeichnis, kein Gerät) bzw. dass ihr
  Zielverzeichnis existiert und beschreibbar ist, falls sie fehlt — bevor irgendetwas
  geschrieben wird, damit `agy` nicht schon die `settings.json` ändert, während die
  `statusline.py`-Vorbedingung noch scheitert (oder umgekehrt);
* zeigen für jedes in diesem Aufruf anstehende Ziel den Diff und fragen einmal für alle
  zusammen ausdrücklich nach (`Anwenden? [j/N]`); ohne `j`/`J` bricht das Skript ohne jede
  Änderung ab, auch wenn mehrere Ziele etwas zu tun hätten — das gilt seit Runde 6 auch für eine
  `statusline.py`, die noch gar nicht existiert (siehe unten, Leitungsentscheid);
* halten den `sha256sum` des jeweiligen Ziels schon beim Zusammenbauen der neuen Fassung (bzw.
  beim Diff) fest und prüfen ihn nach der Rückfrage erneut — ändert sich die Zieldatei
  währenddessen (ein anderer Prozess, ein gleichzeitiges "immer erlauben"), bricht das Skript
  ohne jede Änderung ab, statt eine Sicherung des schon veralteten Standes anzulegen;
* legen bei Zustimmung zuerst eine Sicherung mit sekundengenauem Zeitstempel neben der
  Zieldatei an (`cp -p -n`, bei einem fehlschlagenden `cp` wird eine dabei schon unvollständig
  angelegte Sicherung wieder entfernt), bestätigen sie per `cmp -s` gegen das Original, erhalten
  die Rechte der Zieldatei (`chmod --reference`) und übernehmen die neue Datei erst danach per
  atomarem `mv`;
* schreiben, wenn die Zieldatei (oder ein Verzeichnis auf ihrem Weg) ein Symlink ist, auf die
  Datei, auf die er zeigt (`readlink -f`) — der Symlink selbst bleibt erhalten.

`claude` mergt den `permissions`-Block aus `claude-rechte.json` per `jq -s '.[0] * .[1]'` in die
bestehende `~/.claude/settings.json`. Das ist ein **tiefer** Merge: `permissions.defaultMode`
und alle anderen Top-Level-Schlüssel (`hooks`, `env`, `enabledPlugins`, `sandbox`, …) bleiben
unverändert, aber Listen wie `permissions.allow`/`permissions.deny` werden dabei **vollständig
durch die Vorlage ersetzt**, nicht mit dem Bestand vereinigt — ein `jq *`-Merge überschreibt
gleichnamige Schlüssel, statt ihre Arrays zusammenzuführen. Vor dem Übernehmen prüft das Skript
zur Plausibilität, dass `permissions.deny` im Ergebnis nicht leer ist.

`agy` ersetzt `~/.gemini/antigravity-cli/settings.json` komplett durch `agy-settings.json` mit
`envsubst` für `${PROJEKTE}`/`${TRUSTED_WORKSPACE}` (validiert danach per
`jq -e -s 'length==1'`, dass das Ergebnis gültiges JSON mit genau einem Dokument ist und kein
unersetztes `${` mehr enthält). `statusline.py` wird davon unabhängig behandelt, nicht nur dann,
wenn `settings.json` sich ändert, über denselben Codepfad wie der eigenständige Unterbefehl
`agy-statusline`: Fehlt `~/.gemini/antigravity-cli/statusline.py`, zeigt das Skript den ganzen
neuen Inhalt als Diff gegen `/dev/null` und fragt trotzdem nach — eine neu zu installierende
Datei wird seit Runde 6 nicht mehr stillschweigend installiert (Leitungsentscheid: auch eine
Neuinstallation ist eine Änderung an der persönlichen Konfiguration, die eine Anzeige und eine
Rückfrage verdient); existiert sie schon und weicht von `setup/vorlagen/statusline.py` ab, zeigt
das Skript den gewohnten Diff. Beide Fälle teilen sich dieselbe Rückfrage wie `settings.json`
(bei `agy`) bzw. eine eigene (bei `agy-statusline`) — eine Vorlage bestätigt die andere nicht mit.
`agy-statusline` installiert `statusline.py` unabhängig von `agy`/`settings.json` und braucht
dafür weder `PROJEKTE` noch `TRUSTED_WORKSPACE`.

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

`claude`, `agy` und `agy-statusline` erzeugen je einen eigenen Zeitstempel (getrennte Aufrufe,
siehe „Anwenden" oben, jeweils in der letzten Ausgabezeile — ein `agy`-Aufruf, der beide Ziele
ändert, gibt denselben Zeitstempel für beide aus). Fallen zwei Zeitstempel auseinander und sollen
beide zurückgespielt werden: den **jüngeren zuerst**, den älteren danach — `rueckbau` legt bei
jedem Aufruf selbst eine neue Sicherung des gerade aktuellen Standes an, und in der falschen
Reihenfolge könnte diese zufällig genau auf den noch ausstehenden jüngeren Zeitstempel fallen und
von jenem Aufruf fälschlich als dessen Sicherung gelesen werden (siehe Kopf von
`rechte-anwenden.sh`, gemessen in `rechte-anwenden-test.sh`, T6). `rueckbau <Zeitstempel>` löst zuerst alle drei
Ziele kanonisch auf und prüft (wie `claude`/`agy`) dass keines per Symlink in den eigenen
Checkout zeigt; für jedes Ziel, das dieser Aufruf tatsächlich zurückspielen wird, zusätzlich, dass
es eine reguläre, beschreibbare Datei ist. Erst danach prüft es für
`~/.claude/settings.json`, `~/.gemini/antigravity-cli/settings.json` und
`~/.gemini/antigravity-cli/statusline.py` unabhängig voneinander, ob dazu tatsächlich eine
Sicherung existiert (nicht leer, bei den JSON-Zielen zusätzlich gültiges JSON mit genau einem
Dokument, `jq -e -s 'length==1'`) — eine Datei, die `agy`/`agy-statusline` frisch installiert hat
(keine Sicherung, weil vorher nichts da war), erkennt es an einer eigenen Marker-Datei. Bricht
ohne jede Änderung ab, wenn zu diesem Zeitstempel gar nichts davon existiert oder eine vorhandene
Sicherung leer oder ungültig ist. Zeigt dann für jedes betroffene Ziel den Diff (bzw. bei einer
Entfernung einen Hinweis) und fragt einmal für alle zusammen nach (`Anwenden? [j/N]`); wie bei
`claude`/`agy` hält es dabei den `sha256sum` jedes tatsächlich zurückzuspielenden Ziels schon vor
dieser Rückfrage fest und prüft ihn danach erneut, bevor es irgendetwas schreibt. Erst danach
sichert es den *aktuellen* Stand jeder betroffenen Datei unter einem eigenen, neuen Zeitstempel
(den es am Ende ausgibt — auch dieser Rückbau lässt sich damit per `rueckbau` rückgängig machen)
und spielt per Zwischendatei und atomarem `mv` zurück, mit erhaltenen Dateirechten (`chmod
--reference`). Für „existierte vor dem Anwenden nicht" verschiebt es die Datei auf ihren eigenen
Sicherungsnamen, statt sie zu löschen. Auch hier zuerst der Hub-Hinweis, vor der Rückfrage: ein
laufender Hub würde die soeben zurückgespielte Sicherung sonst erneut mit dem zwischenzeitlich
angewendeten Stand überschreiben.

## Testskript

`rechte-anwenden-test.sh` prüft `rechte-anwenden.sh` selbst (nicht die angewendeten Vorlagen):
ein scheiterndes `cp` bei der Sicherung, eine Änderung der Zieldatei während der Diff-Rückfrage,
ein Abbruch per `HUP`/`INT`/`TERM` während der Rückfrage, dass `statusline.py` unabhängig von
`settings.json` installiert wird (seit Runde 6 auch mit Rückfrage, wenn sie noch fehlt), ein
Symlink-Ziel in den eigenen Checkout (auch über ein symlinked Zwischenverzeichnis wie
`~/.claude`), eine `settings.json` mit mehr als einem JSON-Dokument, ein nicht beschreibbares
bzw. kein reguläres Ziel, `agy-statusline` eigenständig und ein vollständiger
`rueckbau`-Rundlauf über `claude`, `agy` und `statusline.py`. Für den Rundlauf liest der Test den
Zeitstempel jeweils aus der letzten Ausgabezeile von `claude` und von `agy` (nicht aus dem
Dateinamen einer Sicherung) und ruft `rueckbau` einmal je unterschiedlichem Zeitstempel auf —
`claude` und `agy` laufen in getrennten Aufrufen und können, je nach Sekundengrenze, verschiedene
Zeitstempel bekommen; ein Test, der nur den `.claude`-Zeitstempel läse, wäre in genau diesem Fall
flackernd (Befund aus Runde 6). Jedes Szenario legt sein eigenes Fake-HOME per `mktemp -d` an,
ändert nie eine echte Datei unter dem eigenen `HOME` und räumt sich selbst auf; die dafür
nötigen Stub-Werkzeuge (`cp`, `diff`) löst es zur Laufzeit über `command -v` auf, statt einen
Rechnerpfad fest einzucodieren. `.github/workflows/check.yml` führt es bei jedem `check`-Lauf
mit aus.

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
  ein Rechte-Ziel nie selbst — für so ein Ziel druckt er nur den passenden Aufruf von
  `setup/vorlagen/rechte-anwenden.sh` (siehe Kopf dieser Datei und `runSetupApply`/
  `rechteSkriptCall` in `tools/imprint-dev/setup.go`); ausgeführt wird davon nichts, und es gibt
  seit dem `anwenden`-Feld keine Cache-Datei und keinen `cp`-Befehl mehr, gleich für welchen der
  drei Einträge. Die Deny-Regeln bleiben trotzdem stehen, damit kein Agent auch nur diesen
  gedruckten Aufruf selbst ausführt. Auf der `agy`-Seite matchen die Regeln nur den exakten, wörtlichen
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
* Nur `agy-rechte-vorlage` zeigt bei `imprint-dev setup --plan` dauerhaft den Status
  `abweichend`: die Vorlage trägt die Platzhalter `${PROJEKTE}`/`${TRUSTED_WORKSPACE}`, die
  `--plan`/`--apply` nicht auflösen (siehe „Platzhalter" oben), und ihre Bytes weichen deshalb
  immer von der angewendeten Zieldatei ab. `claude-rechte-vorlage` (nur ein Ausschnitt der
  Zieldatei) und der `copy`-Eintrag `agy-statusline` (keine Platzhalter) zeigen `fehlt`, solange
  noch nicht angewendet, und nach dem Anwenden `gleich` — gemessen 2026-09-30 gegen
  `imprint-dev38 setup --plan`, Head `bfc0ecb` (Momentaufnahme, siehe oben).
