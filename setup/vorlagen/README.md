# Rechte-Vorlagen für Claude Code und agy

Zwei Vorlagen mit den geprüften Rechte-Regeln aus einer angewendeten, getesteten Konfiguration.
**Nur der Mensch wendet sie an** (Nutzerentscheid CL-119/CL-123). `imprint-dev setup --apply`
gibt es seit PR 38, aber es schreibt ein Rechte-Ziel (`rechte: true`, wie beide Einträge hier)
nie selbst: für so ein Ziel rendert `--apply` die Vorlage nur in eine Cache-Datei unter
`$XDG_CACHE_HOME/imprint` und druckt zwei `cp`-Befehle (Sicherung, dann Cache → Ziel) auf
stdout; ausgeführt wird davon nichts (siehe `tools/imprint-dev/setup.go`, `runSetupApply` und
`writeRechteCache`). Der gedruckte zweite `cp`-Befehl kopiert dabei roh — er kennt kein
Fragment-Merge —, deshalb bleibt für `claude-rechte.json` weiterhin der jq-Merge unten
Pflicht, nicht der gedruckte Befehl. Beide Vorlagen tragen trotzdem weiterhin eine eigene
Deny-Regel gegen genau diesen Befehl — vorsorglich, damit kein Agent auch nur die
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

```sh
pkill -f 'agy --hub'   # sonst schreiben laufende Hubs den alten Stand zurück (gemessen 2026-09-30,
                         # 00:49; die beendete Prozesszeile lautete "…/.gemini/bin/agy --hub …")
pgrep -af -- '--hub'    # Kontrolle: muss leer sein - sonst die angezeigten PIDs gezielt beenden
                         # statt erneut pkill zu raten

ts="$(date +%Y%m%d-%H%M%S)"
cp -n ~/.gemini/antigravity-cli/settings.json "$HOME/.gemini/antigravity-cli/settings.json.bak-$ts"
cp -n ~/.claude/settings.json "$HOME/.claude/settings.json.bak-$ts"
[ -f ~/.gemini/antigravity-cli/statusline.py ] && \
  cp -n ~/.gemini/antigravity-cli/statusline.py "$HOME/.gemini/antigravity-cli/statusline.py.bak-$ts"

: "${PROJEKTE:?PROJEKTE muss gesetzt sein}" "${TRUSTED_WORKSPACE:?TRUSTED_WORKSPACE muss gesetzt sein}" && \
  HOME="$HOME" PROJEKTE="$PROJEKTE" TRUSTED_WORKSPACE="$TRUSTED_WORKSPACE" \
  envsubst '${HOME} ${PROJEKTE} ${TRUSTED_WORKSPACE}' \
  < setup/vorlagen/agy-settings.json > ~/.gemini/antigravity-cli/settings.json

install -m 0755 setup/vorlagen/statusline.py ~/.gemini/antigravity-cli/statusline.py

# claude-rechte.json liefert NUR den permissions-Block, nicht die ganze Datei. NIEMALS die
# Vorlage direkt über ~/.claude/settings.json kopieren (kein `cp settings.json.neu
# ~/.claude/settings.json` aus der Vorlagen-Datei selbst) — das würde hooks, env,
# enabledPlugins, sandbox und defaultMode aus der bestehenden Datei löschen. Stattdessen nur
# .permissions ersetzen, alles andere aus der bestehenden Datei behalten (Sicherung liegt schon
# oben als settings.json.bak-$ts) und vor dem Übernehmen den Diff ansehen:
jq -s '.[0] * {permissions: .[1].permissions}' \
  ~/.claude/settings.json setup/vorlagen/claude-rechte.json \
  > "$HOME/.claude/settings.json.neu-$ts"
diff -u ~/.claude/settings.json "$HOME/.claude/settings.json.neu-$ts"   # erst ansehen …
mv "$HOME/.claude/settings.json.neu-$ts" ~/.claude/settings.json       # … dann übernehmen
```

Die Sicherungen laufen vor der `:?`-Prüfung, weil sie selbst nichts überschreiben, das nicht schon
da ist (`cp -n`), und weil sie unabhängig von `PROJEKTE`/`TRUSTED_WORKSPACE` sinnvoll sind. Die
`:?`-Prüfung selbst steht in derselben `&&`-Kette wie der `envsubst`-Aufruf: bricht sie ab, läuft
die Kette dahinter nicht weiter, und `>` truncatet die Zieldatei nicht erst und scheitert dann erst
an einem leeren `PROJEKTE` — ein `:?` in einer eigenen, vorangehenden Zeile würde das nicht
verhindern, weil eine interaktive Shell nach dessen Fehlermeldung einfach mit der nächsten Zeile
weiterläuft. `$HOME` selbst ist immer gesetzt; der Mensch wählt bewusst, wie weit
`TRUSTED_WORKSPACE` reicht (siehe Platzhalter oben) — es wird hier absichtlich nicht auf `$HOME`
vorbelegt. `cp -n` überschreibt eine schon vorhandene Sicherung mit demselben Zeitstempel nicht;
derselbe `$ts` für alle drei Kopien hält sie zusammen.

`envsubst` mit der eingeschränkten Variablenliste lässt jedes andere `$`-Zeichen in den Dateien
unverändert.

## Rückbau

```sh
pkill -f 'agy --hub'   # wieder zuerst, sonst schreibt ein laufender Hub die soeben
                         # zurückgespielte Sicherung erneut mit dem angewendeten Stand zurück
pgrep -af -- '--hub'    # Kontrolle: muss leer sein - sonst die angezeigten PIDs gezielt beenden
cp ~/.gemini/antigravity-cli/settings.json.bak-<Zeitstempel> ~/.gemini/antigravity-cli/settings.json
cp ~/.claude/settings.json.bak-<Zeitstempel> ~/.claude/settings.json
# statusline.py.bak-<Zeitstempel> existiert nur, wenn beim Anwenden schon eine Datei da war
# (siehe "Anwenden" oben); dann ebenso zurückkopieren, sonst die Kopie einfach entfernen:
[ -f ~/.gemini/antigravity-cli/statusline.py.bak-<Zeitstempel> ] \
  && cp ~/.gemini/antigravity-cli/statusline.py.bak-<Zeitstempel> ~/.gemini/antigravity-cli/statusline.py \
  || rm -f ~/.gemini/antigravity-cli/statusline.py
```

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
