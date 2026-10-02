# `imprint-dev judge`: der Jev-Kern

**Stand:** 2026-10-02 · Stufe 1 (Hinweis) · Spezifikation: `jev-kern.md` und Konzept B §4, §5, §9
(Repository jev-mistral, Stand 2026-10-01).

| Was | Wie | Stand |
|---|---|---|
| Aufruf | ein Ereignis als JSON auf stdin, genau ein Verdikt als JSON auf stdout; nie Exit 2 | gebaut; `judge_test.go`, `TestJudgeNeverExits2` |
| Gates | `done`, `foreign_return`, beide Hinweis (`warn`), `fail_mode: open` | gebaut; **in keinem Hook verdrahtet** |
| Fragen, Schwellen, Fail-Modus | eine Datei, `tools/imprint-dev/judge/gates.json`, ins Binary eingebettet | gebaut; `TestEmbeddedRegistry` |
| Modell | gepinnt `jev-1.13.0`, nie `jev-latest` | gebaut; `TestRegistryPinningAndVersion`; live 2026-10-02: API nennt `model: jev-1.13.0` |
| Maskierung | erst kappen (mit Rand), dann maskieren, dann genau kappen; `PostState` maskiert jedes String-Blatt noch einmal | gebaut; `TestJudgeMasksEverythingSent`, `TestPostStateMasksWhatTheBuilderLeft`, `TestMaskCappedCutsAfterMasking` |
| Deadline | zählt ab Start von `judge`: stdin, Transkript, Maskierung, Schlüssel, Anfrage | gebaut; `TestJudgeDeadlineCoversBuild` (20 MB Werkzeugausgabe, 10.000 Aufrufe) |
| Log | eine JSONL-Zeile je Aufruf, ohne Texte | gebaut; `TestJudgeLogOneLinePerCall` |
| Durchsetzung | keine: kein Hook ruft `judge` | **nichts setzt hier etwas durch** |

```
 Hook-JSON oder Umschlag ──stdin──▶ imprint-dev judge --gate G
                                     0 Deadline ab Start von judge (aus gates.json je Ereignis)
                                     1 Gate-Code: Vorfilter, Code-Flags, State (gekappt, dann maskiert)
                                       └─ reicht Code? → allow, kein Jev-Aufruf
                                     2 Schlüssel (TYPESAFE_API_KEY / secret-tool), innerhalb der Deadline
                                     3 EIN Batch-POST: jedes String-Blatt maskiert, model = jev-1.13.0 ──▶ api.typesafe.ai
                                     4 Regeln aus gates.json → allow | warn | ask | block,
                                       herabgestuft auf das, was der Host bei diesem Ereignis kann
                                     5 eine Logzeile
                         stdout ◀── Verdikt-JSON (Exit 0, auch wenn der Aufruf scheiterte)
```

## Gemessen

Ein Live-Aufruf am 2026-10-02 (go1.27.1, Commit 107b79d, `--source test`, eigener Log-Pfad), Gate
`done`, synthetische Eingabe: eine Erfolgsmeldung nach `Edit` und erfolgreichem `go test ./...`.

| Messgröße | Wert |
|---|---|
| Verdikt | `allow` (`claim` 0.97, `backed` 0.94, `local_evidence` true) |
| Latenz | 288 ms gesamt, davon 287 ms Anfrage, 0 ms Schlüssel (aus der Umgebung) |
| Antwortfelder (nur Namen gelesen) | `answers`, `model`, `usage` |
| `model` in der Antwort | `jev-1.13.0`, gleich dem gesendeten |

Zweiter Aufruf, 2026-10-02 nach den Review-Korrekturen (Commit 365034c, Registry `2026-10-02.2`,
Fragen jetzt in Registry-Reihenfolge), gleiche Eingabe: `allow`, `claim` 0.97, `backed` 0.93, 271 ms
(Gate-Code 0 ms, Anfrage 270 ms), Antwortfelder und `model` wie oben.

Nicht gemessen: Aufbau von `usage` (darum noch kein `tokens_in` im Log), Latenz-Verteilung (n = 1),
`foreign_return` gegen die echte API.

## Aufruf

```sh
imprint-dev judge --gate <name> [--host claude|codex|agy|pi] [--deadline-ms N] [--no-ui]
                  [--source hook|bench|test] [--endpoint url] [--registry datei]
imprint-dev judge --list       # Gates, Registry-Version, gepinntes Modell (JSON)
imprint-dev judge --version    # Kern-Version, Registry-Version, Modell (JSON)
imprint-dev version            # auch: imprint-dev --version
```

| Flag | Bedeutung |
|---|---|
| `--gate`, `--host` | optional, wenn der Umschlag sie nennt; nennen beide etwas anderes: Verdikt mit `error_class: call` |
| `--deadline-ms` | Gesamt-Deadline ab Start von `judge`, inkl. stdin, Gate-Code und Schlüsselsuche; ohne: die des Ereignisses aus `gates.json` |
| `--no-ui` | niemand kann gefragt werden: ein kritisches Gate blockt nach einem Fehler, statt zu fragen |
| `--source` | Feld `source` im Log; Tests und Messläufe setzen `test` bzw. `bench` |
| `--endpoint` | wie bei `hook-typesafe-check`: nur `api.typesafe.ai` oder Loopback |
| `--registry` | andere Registry-Datei, nur für Tests und Kalibrierung |
| `--emit` | nur `verdict`; `hook` ist nicht gebaut (Verdikt mit `error_class: call`) |

**stdin** ist maßgeblich und hat eine von zwei Formen:

- Umschlag: `{"host": "claude", "gate": "done", "payload": {…rohes Hook-JSON…}}`
- rohes Hook-JSON des Hosts (so, wie ein einzeiliger Hook-Wrapper es durchreicht); dann ist `--gate` Pflicht.

Höchstens 64 MB; mehr ist ein Eingabefehler.

Das Ereignis kommt aus `payload.hook_event_name`. Fehlt es und bedient das Gate nur ein Ereignis, gilt dieses.

## Ausgabe

```json
{"verdict": "warn", "gate": "done", "event": "Stop", "reasons": ["unbacked_claim"],
 "message": "TypeSafe: Erfolgsbehauptung ohne sichtbaren Beleg – …",
 "scores": {"backed": 0.2, "claim": 0.9}, "code_flags": {"claim_re": true, "local_evidence": false, "stop_hook_active": false},
 "model": "jev-1.13.0", "registry_version": "2026-10-02.2", "core_version": "devel+1d28c21",
 "latency_ms": 335, "latency_parts_ms": {"build": 1, "key": 0, "post": 330},
 "fail_mode": "open", "failed": false, "error_class": ""}
```

| Feld | Bedeutung |
|---|---|
| `verdict` | das einzige Feld, nach dem ein Adapter handelt |
| `reasons`, `message` | Regel-Gründe aus `gates.json` und ihre Hinweistexte; bei `allow` leer |
| `scores`, `code_flags` | Jev-Antworten (Noul 0..1) und die Ergebnisse der Code-Prüfungen |
| `prefilter` | nur wenn Code allein entschied (z. B. `no_claim`, `too_short`): dann kein Jev-Aufruf |
| `model_resolved` | nur wenn die API ein Feld `model` zurückgibt |
| `latency_ms`, `latency_parts_ms` | Gesamtzeit ab Start; `build` (Gate-Code), `key` (Schlüsselsuche), `post` (Anfrage) |
| `failed`, `error_class` | `no_key`, `timeout`, `network`, `http_status`, `parse`, `missing_answer`, `internal`, `call`, `input`; dann entscheidet `fail_mode` |

**Exit-Codes.** `judge` endet nie mit 2: Claude Code liest 2 als blockierend, und ein Hinweis-Gate darf
nicht blockieren.

| Exit | Wann | Verdikt, Logzeile |
|---|---|---|
| 0 | jedes Urteil, auch ein gescheitertes (`failed: true`) | ja |
| 0 | Aufruf- oder Eingabefehler: Gate, Host, Ereignis, `--emit`, `--source`, `--endpoint`, `--deadline-ms` (`error_class: call`); stdin unlesbar, über 64 MB, kein JSON-Objekt (auch: tiefer als 10.000 Ebenen), kein `hook_event_name` bei mehreren Ereignissen (`error_class: input`) | ja; der `fail_mode` des Gates entscheidet; ist das Gate nicht bestimmbar (fehlt, unbekannt, Flag und Umschlag widersprechen sich, Registry unlesbar), gilt es als Hinweis-Gate: `allow` |
| 1 | kein Urteil fällig: Flag nicht lesbar, überzähliges Argument, `--list`/`--version` ohne lesbare Registry, stdout nicht schreibbar | nein |

Ein Gate, das bei einem Ereignis aufgerufen wird, das es nicht bedient (z. B. `done` an SubagentStop),
nimmt für seinen Fail-Modus, was irgendeines seiner Ereignisse kann.

**Wrapper.** Ein einzeiliger Wrapper wie heute (`command -v imprint-dev >/dev/null 2>&1 || exit 0; exec
imprint-dev judge …`) reicht 0 und 1 durch; beide blockieren in Claude Code nicht (Hooks-Doku hier
**nicht geprüft**). Für ein kritisches Gate passt das `|| exit 0` nicht: fehlt `imprint-dev`, wäre das
Gate offen. Was ein solcher Wrapper dann ausgibt, ist offen (siehe unten).

## Fail-Modus

Entscheid der Person vom 2026-10-02 (löst die offene Frage in `jev-kern.md` §6/§10 auf):

| Gate | Aufruf gescheitert, Person fragbar | … niemand fragbar (`--no-ui`, oder Ereignis kann nicht fragen) | … Ereignis kann weder fragen noch blocken |
|---|---|---|---|
| Hinweis-Gate (`fail_mode: open`) | `allow` | `allow` | `allow` |
| kritisches Gate (`fail_mode: closed`) | `ask` | `block` | `warn` |

Gescheitert heißt: kein Schlüssel, Timeout, Netzfehler, HTTP ≠ 200 (auch eine Umleitung; ihr wird nie
gefolgt), Antwort kein JSON, eine Frage ohne Antwort, oder ein interner Fehler im Gate-Code. Ein
kritisches Verdikt nach einem Fehler trägt den Grund `core_failed`.

In Stufe 1 ist kein Gate kritisch. Die Tests prüfen den kritischen Pfad mit einer Test-Registry.
Nicht in diesem Binary geregelt: was passiert, wenn `imprint-dev` fehlt oder der Host es tötet. Die
heutigen Wrapper (`hooks/*.sh`) beenden sich dann mit 0, also offen.

**Offene Frage an die Person:** Ein kritisches Gate am Stop-Ereignis blockt nach einem Fehler, weil
Stop nicht fragen kann. `block` bei Stop heißt aber: der Agent arbeitet weiter, statt dass eine Person
gefragt wird. Ob das gewollt ist oder ein kritisches Gate an Stop nach einem Fehler nur warnen soll,
entscheidet die Person; heute gilt `block`.

## Gates in Stufe 1

| Gate | Ereignis | Fragen (ein Batch) | Regel | Herkunft |
|---|---|---|---|---|
| `done` | Stop | `claim`, `backed` | `claim>=0.7 && (!local_evidence \|\| backed<=0.5)` → `warn` | 1:1 aus `ts-done-check` (typesafe-dev 8b80f48): Fragen, Schwellen, `CLAIM_RE`, `local_evidence`, letzte 15 Aufrufe, Bash-Befehl ≤ 200 Zeichen, Nachricht: letzte 4000 Zeichen. Einziger Unterschied: `warn` statt `block` |
| `foreign_return` | PostToolUse (`tool_response`), SubagentStop (`last_assistant_message`) | `instruction_to_agent`, `exfil_request` | je ≥ 0.35 → `warn` | Fragen aus Konzept B §4.2; Schwelle 0.35 laut Konzept B aus dem llm_guardrails-Cookbook, hier nicht nachgelesen; Text ≥ 15 Zeichen; über 8000 Zeichen gehen Anfang und Ende (je knapp 4000, getrennt durch `[…]`) |

Grenze von `foreign_return`: Die String-Blätter eines Objekts werden nach Schlüssel sortiert
verbunden. Was nur in der Mitte eines Textes über 8000 Zeichen steht, sieht Jev nicht
(`TestForeignReturnSeesTheTail` prüft das Ende hinter 10 kB Füllung).

**Kappen und Maskieren.** Jedes Textfeld wird zuerst mit 1024 Zeichen Rand gekappt, dann maskiert,
dann genau gekappt. So wächst die Arbeit nicht mit der Eingabe, und ein Geheimnis, durch das der erste
Schnitt läuft, ist maskiert, bevor der zweite Schnitt den Rand abwirft (gilt für Geheimnisse bis 1024
Zeichen). `done` maskiert nur die Befehle der letzten 15 Aufrufe. Die kompilierten Namensmuster
(`TYPESAFE_NAMES_FILE`) bleiben je Prozess im Speicher, bis sich die Datei ändert. Fragen gehen in der
Reihenfolge der Registry hinaus (`done`: `claim`, dann `backed`, wie `ts-done-check`).

Kalibrierung, wie in `gates.json` vermerkt: `done` auf 8 synthetischen Fällen am 2026-09-29 mit dem
Alias `jev-latest` (welche Version er damals war: nicht festgehalten), nicht neu für `jev-1.13.0`.
`foreign_return`: nicht kalibriert.

## Registry

`tools/imprint-dev/judge/gates.json` ist der eine Ort für Fragen, Schwellen, Kappungen, Deadlines und
Fail-Modus. Sie wird per `go:embed` ins Binary gebaut; Binary und Registry laufen also nicht auseinander.
Beim Laden wird geprüft (ein Verstoß verwirft die Registry): Modell gepinnt (kein `*-latest`), jedes
Gate hat Go-Code, jedes Ereignis kann `allow` und `warn`, Deadline je Ereignis 1…9000 ms, jedes
Textfeld des States hat `cap_chars` 64…32000 und `keep` `head`, `tail` oder `head_tail`, jede Liste
`max_items` 1…100, `min_chars` höchstens `cap_chars`, kein State-Feld, das das Gate nicht liest;
`stage` ist `advisory` oder `enforcing`, und ein `advisory`-Gate hat `fail_mode: open` und Regeln bis
höchstens `warn`; jede Regel parst und nennt nur Fragen und Code-Flags ihres Gates, jede Regel hat
einen Hinweistext; keine Frage doppelt. Fragen sind statisch; dynamischer Text steht nur im State. Fragen nehmen nur Noul.

Regeln (`rules[].if`) kennen Score-IDs mit Vergleich (`>=`, `<=`, `>`, `<`, `==`, `!=`), Code-Flags,
`&&`, `||`, `!` und Klammern. Was Code rechnen kann, ist ein Code-Flag, nie eine Jev-Frage.

## Log

Pfad: `$IMPRINT_JUDGE_LOG`, sonst `${XDG_STATE_HOME:-~/.local/state}/imprint/judge.jsonl`. Eine Zeile je
Aufruf mit Verdikt (auch wenn Code allein entschied oder der Aufruf scheiterte). Felder: `ts` (UTC),
`source`, `host`, `gate`, `event`, `session` (SHA-256 der Sitzungs-ID, 16 Hex-Zeichen),
`registry_version`, `core_version`, `model`, `verdict`, `reasons`, `scores`, `code_flags`, `prefilter`,
`mask_counts`, `latency_ms`, `latency_parts_ms`, `fail_mode`, `failed`, `error_class`, `no_ui`.
Nie im Log: State, Prompt, Dateiinhalt, Nachricht, Befehl, Schlüssel. `mask_counts` zählt die
Platzhalter im gesendeten Request (`<email>`, `<redacted>` …), nicht was eine Kappung verworfen hat;
ohne Request ist es 0. Auch Aufruf- und Eingabefehler schreiben eine Zeile.

## Version

`core_version` ist `<version>+<commit, 7 Zeichen>[.dirty]`. `<version>` ist leer bei `go build`
im Checkout und erscheint dann als `devel`; ein Build kann sie setzen:
`go build -ldflags "-X main.version=0.10.0" ./tools/imprint-dev`. `go run` und `go test` betten keine
VCS-Daten ein; dann steht nur `devel`. Gemessen 2026-10-02 mit go1.27.1: `go build` im Checkout ergibt
Modulversion `(devel)` und `vcs.revision`, `vcs.time`, `vcs.modified`.

## Nicht gebaut (Stufe 1)

| Was | Warum nicht |
|---|---|
| Hook-Verdrahtung (`hooks/hooks.json`) | außerhalb dieser Stufe; ohne sie läuft `judge` nur von Hand |
| `--emit hook` (Host-JSON) | Host-Felder für PreToolUse `ask`/`deny` nicht nachgelesen; ohne Verdrahtung ungetestet |
| `--replay`, `--shadow`, `imprint-dev mask` | Spezifikation nennt sie; nicht Teil dieser Stufe |
| Code-Gates aus Konzept B §9 (Roster-Datum, Alter-Liste, SessionStart-Zustand, Pfadpräfix) | Pfadliste und Ablöse-Marker-Format sind nicht festgelegt |
| CL-001…004 als Registry-Gates | die Hooks senden `jev-latest` und einen Text-State; ein Umzug änderte ihr Verhalten |
| Choice-Fragen | erst mit `skill_suggestion` oder `method` nötig |
| Check in `imprint-dev check` (jede Frage hat Kriterien; `calibration.model` = `model`, mit Datum) | die heutige Registry fiele auf beiden Gates durch: keine Kriterien, `done` auf `jev-latest` kalibriert, `foreign_return` gar nicht; ob Warnung oder Verstoß, ist offen |

| Wrapper für kritische Gates | was er ausgibt, wenn `imprint-dev` fehlt, ist offen; `\|\| exit 0` wäre offen |

Re-check by 2027-01-02: ob `jev-1.13.0` noch angeboten wird und die Schwellen noch tragen.

## Was das durchsetzt

| Regel | Durchsetzung |
|---|---|
| Fragen und Schwellen gibt es genau einmal | `go:embed` von `gates.json`; geprüft von `TestEmbeddedRegistry` |
| alles an Jev ist maskiert | `PostState` maskiert jedes String-Blatt; geprüft von `TestJudgeMasksEverythingSent` |
| Modell gepinnt | `loadRegistry` lehnt `*-latest` ab; geprüft von `TestRegistryPinningAndVersion` |
| ein Gate setzt eine Regel durch | **nichts**: kein Hook ruft `judge` auf |
