# `imprint-dev judge`: der Jev-Kern

**Stand:** 2026-10-02 · Stufe 1 (Hinweis) · Spezifikation: `jev-kern.md` und Konzept B §4, §5, §9
(Repository jev-mistral, Stand 2026-10-01).

| Was | Wie | Stand |
|---|---|---|
| Aufruf | ein Ereignis als JSON auf stdin, genau ein Verdikt als JSON auf stdout; nie Exit 2 | gebaut; `judge_test.go`, `TestJudgeNeverExits2` |
| Gates | `done`, `foreign_return`, beide Hinweis (`warn`), `fail_mode: open` | gebaut; **in keinem Hook verdrahtet** |
| Fragen, Schwellen, Fail-Modus | eine Datei, `tools/imprint-dev/judge/gates.json`, ins Binary eingebettet | gebaut; `TestEmbeddedRegistry` |
| Modell | gepinnt `jev-1.13.0`, nie `jev-latest` | gebaut; `TestRegistryPinningAndVersion`; live 2026-10-02: API nennt `model: jev-1.13.0` |
| Maskierung | jedes Textfeld ganz maskieren, dann kappen (nie mitten in einem Platzhalter; *seit 2026-10-02 auch nie direkt hinter dem Vorsatz eines Geheimnisses, dessen Wert der Schnitt abschneidet, siehe „Maskieren und Kappen“*); höchstens so viel Text je Aufruf, wie im ungünstigsten Fall in der halben Deadline maskiert ist (3 s: 7,2 MB, 6 s: 14,4 MB), sonst `too_large` und nichts gesendet (*ergänzt am 2026-10-03: mit Pythons Klassen im Schlüssel=Wert-Schritt (mask-parity-unicode c1dd445, in dieser Stufe enthalten) galt „ungünstigster Fall“ für „token=<redacted>“ ohne Leerraum nicht, der Schritt war dort quadratisch; seit dem linearen Schritt gilt es für diese Form wieder (256 KiB 29 ms). Nicht für alles: ein einziger langer Wert kostet den Schritt vorher wie nachher rund 270–300 ns je Byte, über den 200 des Budgets; nur bis 1 MiB gemessen, auf `main` nicht geprüft; beides in „Bekannte Grenzen“, Zeile „Schlüssel = Wert: lange Läufe ohne Leerraum“*); `PostState` maskiert jedes String-Blatt noch einmal; Regeln gleich mit `ts_common.py` (siehe „Parität mit ts_common.py“; *seit 2026-10-03 gemessen: bis auf IBAN-Prüfziffern in anderer Schrift (Go folgt Python ab Stufe 2, bis dahin offen) und die Namensdatei (Nachtrag A7)*) | gebaut; `TestJudgeMasksEverythingSent`, `TestPostStateMasksWhatTheBuilderLeft`, `TestCutMaskedProperty` (50 × 200 Läufe), `TestCutMaskedDanglingSecretLead`, `TestMaskCappedIsCutOfWholeMask`, `TestJudgeMaskBudget`, `TestMaskGolden` |
| Maskier-Geschwindigkeit | dieselben Treffer wie vorher, aus Kandidaten statt NFA über jedes Byte; linear auch auf abgelehnten Treffern (*Adressen seit 2026-10-02 absichtlich anders: Vereinigung beider Grammatiken über Automaten, siehe „Adressen“; Opaque seit 2026-10-03 absichtlich anders: Pythons Regel, siehe „Opaque: Pythons Regel“*) | gebaut; `TestMaskDifferential` (alt gegen neu, Byte für Byte), `TestMaskLinearOnRejectedRuns`, `TestAddressAutomata` |
| Deadline | zählt ab Start von `judge`: stdin, Transkript, Maskierung, Schlüssel, Anfrage; Maskieren hört am Ende der Deadline auf zu warten | gebaut; `TestJudgeDeadlineCoversBuild` (dichteste Adressen in Budgetgröße, 15 MiB, 10.000 Aufrufe; *überholt am 2026-10-02: seit der Adress-Vereinigung ist dichtes `schluessel=wert` der langsamste Text, der Test nimmt es: 6,5 MB in 0,52–0,54 s geurteilt, mit Adressen waren es 0,28 s*; *überholt am 2026-10-02 durch die NFC-Vereinigung (aa9d2d4): der langsamste Text ist jetzt `nfdanchors` (dichte NFD-Adressen und -Mails, nicht NFC) mit 116 ns je Byte bei 2 MiB und 109 bei 16 MiB, dichtes `schluessel=wert` 75–84 (`TestMaskProfile`, `GOMAXPROCS=2`, Zahlen aus der Commit-Nachricht von aa9d2d4, hier nicht nachgemessen); auch gemessen am Preis, den `maskNsPerByte` erlaubt (208,3 und 209 ns), ist `nfdanchors` langsamer (0,52–0,56 gegen 0,36–0,40). Seit 2026-10-03 nimmt der Test beide, je in Budgetgröße und mit 15 MiB; gemessen 2026-10-03, 10 Läufe, ohne `GOMAXPROCS`, Last um 12 bei 20 Kernen: 6,5 MB `schluessel=wert` in 0,52–0,93 s geurteilt, `nfdanchors` in 0,79–1,41 s (8 von 10 bis 0,85 s; der langsamste Lauf rund 218 ns je Byte gegen 208,3 ns erlaubt, Lastschwankung wie der Lauf mit 232 ns in `typesafe_state.go`, siehe Entscheid „Budget bleibt bei 200 ns je Byte“ unten in „Parität mit ts_common.py“); 15 MiB beider in 56–129 ms `too_large`*; *bis 2026-10-02 wackelig: der gekappte Text war manchmal 1–7 Zeichen über 8000, behoben durch die Vorsatz-Regel beim Kappen, seither 10 von 10 Läufen grün*), `TestJudgeMaskingStopsAtTheDeadline` |
| Log | eine JSONL-Zeile je Aufruf, ohne Texte und ohne unbekannte Namen | gebaut; `TestJudgeLogOneLinePerCall`, `TestJudgeNeverEchoesUnknownNames` |
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
| `--gate`, `--host` | `--gate` soll ein Adapter **immer** mitgeben (siehe Fail-Modus); nennen Flag und Umschlag etwas anderes: Verdikt mit `error_class: call` |
| `--deadline-ms` | Gesamt-Deadline ab Start von `judge`, inkl. stdin, Gate-Code und Schlüsselsuche; ohne: die des Ereignisses aus `gates.json` |
| `--no-ui` | niemand kann gefragt werden: ein kritisches Gate blockt nach einem Fehler, statt zu fragen |
| `--source` | Feld `source` im Log; Tests und Messläufe setzen `test` bzw. `bench` |
| `--endpoint` | wie bei `hook-typesafe-check`: nur `api.typesafe.ai` oder Loopback |
| `--registry` | andere Registry-Datei, nur für Tests und Kalibrierung |
| `--emit` | nur `verdict`; `hook` ist nicht gebaut (Verdikt mit `error_class: call`) |

**stdin** ist maßgeblich und hat eine von zwei Formen:

- Umschlag: `{"host": "claude", "gate": "done", "payload": {…rohes Hook-JSON…}}`
- rohes Hook-JSON des Hosts (so, wie ein einzeiliger Hook-Wrapper es durchreicht); dann ist `--gate` Pflicht.

Höchstens 16 MB; mehr ist ein Eingabefehler (Verdikt, sofort). Maskiert und gesendet wird davon nur,
was ins Maskier-Budget passt (siehe Gates). Speicher, gemessen 2026-10-02 mit
`/usr/bin/time` (go1.27.1, Linux): 16 MB Fließtext als `tool_response` ≈ 62 MB RSS, 0,05 s; 16 MB aus
920.000 kleinen JSON-Objekten ≈ 507 MB RSS, 0,7 s (`map[string]any` je Objekt). Mehr Struktur auf
gleich vielen Bytes braucht mehr Speicher.

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
| `failed`, `error_class` | `no_key`, `timeout`, `network`, `http_status`, `parse`, `missing_answer`, `internal`, `call`, `input`, `too_large`; dann entscheidet `fail_mode` |

**Exit-Codes.** `judge` endet nie mit 2: Claude Code liest 2 als blockierend, und ein Hinweis-Gate darf
nicht blockieren.

| Exit | Wann | Verdikt, Logzeile |
|---|---|---|
| 0 | jedes Urteil, auch ein gescheitertes (`failed: true`) | ja |
| 0 | Aufruf- oder Eingabefehler: Gate, Host, Ereignis, `--emit`, `--source`, `--endpoint`, `--deadline-ms` (`error_class: call`); stdin unlesbar, über 16 MB, kein JSON-Objekt (auch: tiefer als 10.000 Ebenen), kein `hook_event_name` bei mehreren Ereignissen (`error_class: input`) | ja; welches Gate entscheidet, steht unter Fail-Modus |
| 1 | kein Urteil fällig: Flag nicht lesbar, überzähliges Argument, `--list`/`--version` ohne lesbare Registry, stdout nicht schreibbar | kein Verdikt; eine minimale Logzeile (`error_class: usage`, `exit_code: 1`, kein Feld aus dem Payload), wo das Log schreibbar ist |

Ein Gate, das bei einem Ereignis aufgerufen wird, das es nicht bedient (z. B. `done` an SubagentStop),
nimmt für seinen Fail-Modus, was irgendeines seiner Ereignisse kann.

**Wrapper und Adapter.** Ein Adapter gibt immer `--gate` mit: nur dann entscheidet bei jedem Fehler
der Fail-Modus des gemeinten Gates. Ein einzeiliger Wrapper wie heute (`command -v imprint-dev
>/dev/null 2>&1 || exit 0; exec imprint-dev judge --gate …`) reicht 0 und 1 durch; beide blockieren in
Claude Code nicht (Hooks-Doku hier **nicht geprüft**). Das passt für Hinweis-Gates. Ein Wrapper für ein
**kritisches** Gate muss jeden Exit ungleich 0, ein fehlendes oder unlesbares Verdikt und ein fehlendes
`imprint-dev` selbst als Fehlschlag behandeln und dann tun, was die Tabelle unter Fail-Modus für ein
kritisches Gate sagt (fragen, ohne Person blocken, wo das Ereignis fragen kann; sonst warnen);
`|| exit 0` wäre dort offen. Einen solchen Wrapper gibt es noch nicht.

## Fail-Modus

Entscheide der Person vom 2026-10-02 (der erste löst die offene Frage in `jev-kern.md` §6/§10; der
zweite die Frage, was ein kritisches Gate an Stop tut):

| Gate, Aufruf gescheitert | Ereignis kann fragen, eine Person ist da (PreToolUse) | Ereignis kann fragen, niemand da (`--no-ui`) | Ereignis kann nicht fragen (Stop, SubagentStop, PostToolUse, UserPromptSubmit) |
|---|---|---|---|
| Hinweis-Gate (`fail_mode: open`) | `allow` | `allow` | `allow` |
| kritisches Gate (`fail_mode: closed`) | `ask` | `block` (blocken hält dort die Aktion an) | `warn`: „Jev-Kern ausgefallen, nicht geprüft“ |

Warum `warn` an Stop: `block` an Stop hieße, der Agent arbeitet weiter, ohne dass eine Person prüft; der
Hinweis sagt stattdessen, dass nicht geprüft wurde. Kann ein Ereignis fragen, aber nicht blocken, gilt
ohne Person ebenfalls `warn`.

Wo gefragt werden kann, steht fest im Code als Erlaubnisliste (`hostCanAsk`), nicht in der Registry: nur
PreToolUse, das einzige solche Ereignis, das die Hooks dieses Repositorys kennen (Claude Code; für Codex
angenommen, nicht gemessen). An jedem anderen Ereignis (Stop, SubagentStop, PostToolUse,
UserPromptSubmit, auch ein später hinzukommendes wie PostToolUseFailure) kann niemand gefragt werden:
nennt die Registry dort `ask`, gilt es nicht; ein Fehlschlag ergibt `warn`, eine Regel mit `ask` wird
herabgestuft (`TestHostCannotAskOverridesRegistry`, `TestHostCanAskAllowlist`). Ist das Ereignis
unbekannt (stdin unlesbar), zählt jedes Ereignis des Gates nach derselben Liste. PermissionRequest steht
nicht auf der Liste: das Repository kennt es nicht, die Hooks-Doku ist hier **nicht geprüft**.

Gescheitert heißt: kein Schlüssel, Timeout, Netzfehler, HTTP ≠ 200 (auch eine Umleitung; ihr wird nie
gefolgt), Antwort kein JSON, eine Frage ohne Antwort, ein interner Fehler im Gate-Code, oder ein
Aufruf- bzw. Eingabefehler. Ein kritisches Verdikt nach einem Fehler trägt den Grund `core_failed` und
eine Meldung, die mit „Jev-Kern ausgefallen, nicht geprüft“ beginnt.

**Welches Gate entscheidet** bei einem Aufruf- oder Eingabefehler:

| Lage | Fail-Modus von |
|---|---|
| `--gate` nennt ein Gate der Registry | diesem Gate, immer (auch wenn der Umschlag etwas anderes sagt) |
| kein bekanntes `--gate`, aber ein bekanntes Gate im Umschlag, ohne Widerspruch | diesem Gate |
| Gate nicht bestimmbar, die Registry hat kritische Gates | der Regel für kritische Gates, über das, was das Ereignis bei den kritischen Gates erlaubt (die es bedienen, sonst alle): fragen kann es → `ask`, ohne Person `block`; sonst `warn`; nie `allow` |
| Gate nicht bestimmbar, kein kritisches Gate in der Registry (heute so) | offen: `allow` |

In Stufe 1 ist kein Gate kritisch. Die Tests prüfen den kritischen Pfad mit einer Test-Registry
(`TestJudgeFailModeTable`, `TestJudgeFailModeWhenTheGateIsUnclear`).
Nicht in diesem Binary geregelt: was passiert, wenn `imprint-dev` fehlt oder der Host es tötet. Die
heutigen Wrapper (`hooks/*.sh`) beenden sich dann mit 0, also offen.

## Gates in Stufe 1

| Gate | Ereignis | Fragen (ein Batch) | Regel | Herkunft |
|---|---|---|---|---|
| `done` | Stop | `claim`, `backed` | `claim>=0.7 && (!local_evidence \|\| backed<=0.5)` → `warn` | 1:1 aus `ts-done-check` (typesafe-dev 8b80f48): Fragen, Schwellen, `CLAIM_RE`, `local_evidence`, letzte 15 Aufrufe, Bash-Befehl ≤ 200 Zeichen, Nachricht: letzte 4000 Zeichen, jeweils erst ganz maskiert, dann gekappt wie in Python. Unterschiede: `warn` statt `block`; ein Schnitt mitten in einem Platzhalter rückt an dessen Rand (*ergänzt am 2026-10-02, Paket G5: dritter Unterschied, ein Anfang endet nie auf dem Vorsatz eines Geheimnisses, dessen Wert der Schnitt abschneidet, siehe „Maskieren und Kappen“; in `done` betrifft das Befehl und Werkzeugname, die ihren Anfang behalten, nicht die Nachricht, die ihr Ende behält (`gates.json`)*) |
| `foreign_return` | PostToolUse (`tool_response`), SubagentStop (`last_assistant_message`) | `instruction_to_agent`, `exfil_request` | je ≥ 0.35 → `warn` | Fragen aus Konzept B §4.2; Schwelle 0.35 laut Konzept B aus dem llm_guardrails-Cookbook, hier nicht nachgelesen; Text ≥ 15 Zeichen; über 8000 Zeichen gehen Anfang und Ende (je knapp 4000, getrennt durch `[…]`) |

**Grenze von `foreign_return` in Stufe 1:** Die String-Blätter eines Objekts werden nach Schlüssel
sortiert verbunden. Was nur in der Mitte eines Textes über 8000 Zeichen steht, sieht Jev nicht
(`TestForeignReturnSeesTheTail` prüft das Ende hinter 10 kB Füllung). Bewusst nicht umgebaut.

**Maskieren und Kappen.** Jedes Textfeld wird ganz maskiert (`MaskDetail`) und erst dann auf seine
Kappung geschnitten: Anfang, Ende oder beides. Was hinausgeht, ist damit immer ein Anfang, ein Ende
oder Anfang + `[…]` + Ende des ganz maskierten Textes; ein Geheimnis wird nie zerschnitten, bevor es
maskiert ist, gleich wie lang es ist. Fiele ein Schnitt mitten in einen Platzhalter (`<redacted>`,
`<email>` …), rückt er an dessen Rand; andere `<` bleiben unberührt. Seit 2026-10-02 (Paket G5 der
Maskier-Parität, Entscheid der Lead-Sitzung, Option a): endete der Anfang danach auf dem Vorsatz eines
Geheimnisses, dessen Wert der Schnitt abschneidet (`token=`, `auth: `, `api_key="`, `Bearer `), rückt
der Schnitt vor diesen Vorsatz; es geht weniger hinaus, nie mehr. Grund: `PostState` maskiert jedes
Blatt noch einmal und nahm dabei das `[…]` des Trenners als Wert; `<redacted>` machte das Feld bis zu
7 Zeichen länger als die Kappung (`TestJudgeDeadlineCoversBuild` scheiterte in Paket G4 in 6 von 10
Läufen, in G5 ohne die Regel in 5 von 10, mit ihr in keinem von 10). Für das Ende gibt es den Fall
nicht: im Trenner beginnt kein Treffer, und die Werte, die `[…]` nehmen könnten, enden an seinem
Zeilenumbruch. Ein anderer Weg, auf dem das zweite Maskieren ein Feld wachsen lässt, ist nicht behoben
(siehe „Parität mit ts_common.py“, Bekannte Grenzen). Geprüft:
`TestCutMaskedProperty` (50 Seeds × 200 Texte aus JWT-, base64-, UUID-, Lockfile-, Mehrbyte- und
Prosa-Stücken mit Geheimnissen dazwischen; Orakel: Präfix/Suffix des ganz maskierten Textes, gültiges
UTF-8, kein geteilter Platzhalter, eigene Suche nach Platzhaltern, jede Seite höchstens 9 Zeichen unter
ihrem Anteil an der Kappung (*überholt am 2026-10-02 durch die Vorsatz-Regel: der Anfang darf zusätzlich
um die Vorsätze kürzer sein, die er abschneidet, und endet nie auf einem; das Orakel baut sie aus den
Referenzmustern, nicht aus denen von `cutMasked`*); fängt die Mutationen „Rückblick 8“, „leer“ und
„Byte-Schnitt“, seit 2026-10-02 auch „Vorsatz-Regel aus“), `TestCutMaskedDanglingSecretLead` (12
Vorsätze, darunter beide Anführungszeichen, ein langer Schlüssel-Rest, U+0130, Bearer und Basic, und
„xBearer “, das stehen bleiben muss; jeder Schnitt vom Ende des Vorsatzes bis ins Innere des
Platzhalters; das zweite Maskieren lässt den Schnitt unverändert und in der Kappung),
`TestMaskCappedIsCutOfWholeMask`, die Regressionen `TestForeignReturnKeepsInjectionBesideBlob`,
`TestDoneKeepsMessageBesideBlob`, `TestMaskCappedLongMatch`. `done` maskiert nur die Befehle der letzten
15 Aufrufe; Werkzeugnamen sind Blätter wie andere: maskiert, auf 100 Zeichen gekappt, im Budget
(`TestDoneCapsToolNames`); `local_evidence` entscheidet über die Namen, wie das Transkript sie hat
(`TestDoneEvidenceUsesRawToolNames`).

**Maskier-Geschwindigkeit.** Gos `regexp` hat keinen DFA: eine Suche ohne Anker läuft mit dem NFA über
jedes Byte. Die Muster sind deshalb unverändert, gesucht wird aber anders (`tools/imprint-dev/mask_fast.go`):
ein Treffer kann nur an bestimmten Stellen anfangen (Anfangsbuchstaben eines Schlüsselworts, ein `@`,
zwei Großbuchstaben und zwei Ziffern, Straßen-Endungen vor `\s\d` … (*überholt am 2026-10-02 für Straßen und
Postleitzahlen: Automaten statt Muster, siehe „Adressen“*)); dort wird das Muster mit Anker
versucht, mit dem Zeichen davor als Kontext für `\b`, auf kleinen Fenstern, damit Gos Backtracker statt
des NFA läuft. Kandidaten, deren Zeichen davor ein Lookbehind ablehnt, werden übersprungen statt gefunden,
verworfen und ein Zeichen weiter neu gesucht: das war quadratisch. Namen werden weiter einzeln nacheinander
ersetzt (eine gemeinsame Alternation ersetzte anders, z. B. bei „Hans Max“ und „Max Mustermann“), aber über
`strings.Index` auf einer faltungs-kanonischen Kopie des Textes: jedes Zeichen wird durch das kleinste
Zeichen seiner Groß-/Kleinschreibungs-Klasse ersetzt, so wie `(?i)` vergleicht, in jeder Schrift
(Griechisch, Kyrillisch …). Die Kopie wird einmal gebildet und mit jeder Ersetzung mitgeführt; jeder
Fundort wird exakt nachgeprüft (Faltung und `\b`). Wo das nicht exakt ginge, läuft der alte Weg:
ungültiges UTF-8; für die Schritte ohne Groß-/Kleinschreibung Text mit K (U+212A), ſ (U+017F) oder ẞ
(U+1E9E), den einzigen Zeichen, die Go auf ASCII-Buchstaben bzw. ß faltet; für die Namen außerdem Text mit
einem Zeichen, dessen kanonisches Zeichen eine andere Byte-Länge hat (dieselben drei, das Ohm- und das
Angström-Zeichen …): dann wird Name für Name gesucht. Seit 2026-10-02 (Maskier-Parität, unten) gehören
İ (U+0130) und ı (U+0131) in beide Listen: Python faltet sie mit i, Go nicht von selbst; Bearer/Basic,
Schlüsselwörter und Straßen laufen mit ihnen den alten Weg (mit den Mustern von jetzt), Namen Name für
Name. Türkischer Text verliert damit den schnellen Weg (gemessen unten). (*Überholt am 2026-10-02 für
Straßen und Postleitzahlen: der Adress-Schritt hat keinen alten Weg mehr, seine Automaten laufen auf jedem
Text, mit diesen Zeichen und mit ungültigem UTF-8; siehe „Adressen“.*)

Gemessen 2026-10-02, go1.27.1, `GOMAXPROCS=2`, dieser Rechner, Namensdatei mit 2 Namen,
`TestMaskProfile` (Laufanleitung im Test), alt = Stand 301db1c:

| 16 MB | alt | neu | langsamster Schritt neu |
|---|---|---|---|
| Fließtext | 16,6 s | 0,64 s | `secret_kw` 0,20 s |
| Großbuchstaben + Ziffern | 25,7 s | 0,95 s | `iban` 0,25 s |
| UUIDs, Hashes | 15,7 s | 0,85 s | `opaque` 0,16 s |
| Personendaten dicht (ohne Namensdatei) | 10,0 s | 1,13 s | `secret_kw` 0,25 s |
| Straßenadressen dicht (2 MB) | 1,52 s | 0,26 s | `street` 0,21 s |
| `schluessel=wert` dicht (2 MB) | 0,91 s | 0,15 s | `secret_kw` 0,11 s |
| `a` + 32 KiB Nullen | 26,7 s | 0,6 ms | – |
| 64 KiB `aeyJ` / `XAKIA` | 18,7 s / 13,6 s | 2,7 ms / 2,3 ms | – |

Alt mit Namensdatei auf 16 MB Personendaten: nicht gemessen (Lauf abgebrochen). Der ungünstigste Fall neu
sind dichte Straßenadressen: 2,1 s für 16 MB, rund 127 ns je Byte (*überholt am 2026-10-02: ein Korpus aus
Ketten großgeschriebener Wörter vor einer Hausnummer, an dem Tag erst gemessen, brauchte mit diesem Stand
15,7 s für 16 MB, rund 935 ns je Byte, über beiden Preisen des Budgets; seit der Adress-Vereinigung
0,49 s, siehe „Adressen“*). Jedes Muster der Namensdatei kostet
dazu, je Byte: auf der kanonischen Kopie bis 0,9 ns (50 griechische und kyrillische Namen auf Text ihrer
Schrift; vorher 15 ns mit der Suche Zeichen für Zeichen), Name für Name (Text mit einem solchen Zeichen
oder ungültigem UTF-8) bis 15,7 ns.

**Namen: Wortgrenzen wie in Python (seit 2026-10-02, absichtlich anders als vorher).** Bis dahin nutzte
der Namens-Schritt Gos `\b`, das nur ASCII-Buchstaben als Wortzeichen kennt: ein Name, der mit einem
Buchstaben außerhalb von ASCII beginnt oder endet, wurde nur neben einem ASCII-Wortzeichen maskiert.
„Herr Özil kommt“, „Frau Strauß“, „Herr Weiß“, „Σωκράτης“ und „Владимир“ gingen unmaskiert hinaus, aus
„Jürgen Weiß“ wurde „<name> Weiß“. Jetzt ist ein Wortzeichen, was Pythons `\w` in `ts_common.py` ist: ein
Buchstabe, eine Zahl (jeder Schrift) oder `_`; Kombinationszeichen nicht. Ein Name in einem längeren Wort
(„Weißbier“, „Владимирович“, „x_Weiß“) bleibt wie in Python unmaskiert. Das ist eine Datenschutz-Korrektur
und gilt für alle, die `MaskDetail` nutzen, also auch `hook-typesafe-check` und den Skill-Vorschlag.
Geprüft gegen Python selbst: `TestNamePythonParity` lässt `ts_common.mask_detail` (python3) und
`MaskDetail` auf 57 Texte mit deutschen Umlauten und ß, Griechisch (auch Schluss-Sigma), Kyrillisch,
Namen am Anfang und Ende, neben Satzzeichen und in längeren Wörtern los: Ausgabe und Namenszahl gleich.
Bekannte Unterschiede zu Python, nicht im Korpus: Python setzt das türkische İ/ı mit i gleich, Go nicht
(*überholt am 2026-10-02 durch die Maskier-Parität, unten: Go setzt I, i, İ und ı jetzt wie Python
gleich*);
Python ersetzt alle Namen in einer Alternation, Go Name für Name, längster zuerst (gleich, solange sich
Namen nicht überlappen); Python verwirft bei einem Dekodierfehler die ganze Namensdatei, Go liest die Zeile
als Latin-1 (siehe Namensdatei). (*Der ganze Absatz „Bekannte Unterschiede zu Python“ ist überholt am
2026-10-02 durch „Parität mit ts_common.py“ unten: die Regeln sind abgeglichen und werden bei jedem Lauf
gegen den Goldkorpus geprüft; die Unterschiede, die bleiben (überlappende Namen, die Namensdatei laut
Nachtrag A7), werden dort unter „Bekannte Grenzen“ geführt.*)

**Maskier-Parität mit Python (seit 2026-10-02, absichtlich anders als vorher).** Nach der Parity-Spezifikation
der Lead-Sitzung vom 2026-10-02 (Abschnitte 0, 1a, 1b, 3; Kriterium des Nutzers: das sicherere Verhalten
maskiert mehr, ohne Wörter zu zerteilen) gelten die Zeichenklassen von Pythons `re` auf `str`: Leerraum S ist `unicode.IsSpace` plus
U+001C–U+001F, ein Wortzeichen ein Buchstabe, eine Zahl oder `_`, eine Ziffer Unicode Nd; jedes `(?i)`
(Schlüsselwörter, Bearer/Basic, Straßen-Endungen, Namen) setzt I, i, İ und ı gleich.

| Schritt | jetzt | vorher |
|---|---|---|
| Bearer/Basic | Auslöser nach keinem ASCII-Wortzeichen („äBearer x“ löst aus), S+, Wert bis zum nächsten Tab, LF, FF, CR oder Leerzeichen, Anführungszeichen und Kommas eingeschlossen; ein Wert `<redacted>` zählt mit | Wert endete an `"`, `'`, `,`, `;`; `"Authorization": "Bearer abc"` ließ `"abc"` stehen |
| Schlüssel=Wert | Schlüssel-Rest mit Unicode-`\w` („tokenä=abc“), S um `=`/`:`; „authorization: Bearer abc“ → „authorization: <redacted> <redacted>“; ein Wert, der schon `<redacted>` ist (Groß-/Kleinschreibung egal), wird wie in Pythons Lookahead übergangen und die Suche geht weiter (*überholt am 2026-10-02 durch Nachtrag A3: nur der genaue Platzhalter `<redacted>` wird übergangen, in beiden Sprachen; „token=<REDACTED>“ und `token="<Redacted>"` werden maskiert*) | ASCII; Werte „Bearer“/„Basic“ blieben stehen; `<redacted>` als Treffer übersprungen: „token=<redacted>password=x“ ließ `x` stehen, „token=<REDACTED>“ wurde ersetzt |
| Telefon | Ziffern jeder Schrift nach der 0; eine Zahl jeder Schrift davor („²“, „Ⅻ“) verhindert den Treffer | ASCII-Ziffern; „²“ und „Ⅻ“ davor maskierten mitten im Wort |

Geprüft: `TestMaskSpecExamples` (47 Fälle; die Erwartung ist die Ausgabe von `ts_common.mask_detail` des
Python-Zweigs `mask-parity-unicode`, typesafe-dev 3fa4dfe, ohne Namensdatei), `TestMaskClasses` (S auf jedem
Zeichen), `TestNamesTurkishI`, `TestNamePythonParity` (jetzt 71 Texte, mit türkischen Namen). Dazu einmal am
2026-10-02 gegen denselben Python-Zweig: 8.926 Texte aus Fragmenten des Differentialtests, Zählungen von
`secret_kw`, E-Mail, IBAN und Telefon (alle vor dem Adress-Schritt) in allen gleich; volle Ausgabe auf 4.934
Texten ohne Adresse auf beiden Seiten gleich bis auf 6. **Unterschied, nicht behoben (Abschnitt 7 der
Spezifikation sagt „unverändert, gemessen gleich“):** alle 6 liegen im Opaque-Schritt; Pythons `RX_OPAQUE`
sucht die Ziffer mit Unicode-`\d` im Lookahead, ein Lauf von 24 oder mehr Token-Zeichen direkt vor einer
Ziffer einer anderen Schrift („…abcd١“) wird dort maskiert, in Go nicht (*überholt am 2026-10-03 durch
Nachtrag A9: Go übernimmt Pythons Regel, der Unterschied ist behoben; siehe „Opaque: Pythons Regel“*).
Adressen (Vereinigung beider
Grammatiken) und Namen mit NFC folgen in eigenen Schritten; hier ist an ihnen nur die türkische Faltung neu.
(*Überholt am 2026-10-02 für Adressen: sie folgen im nächsten Absatz; für Namen: „Namen in NFC“ weiter
unten.*)

**Opaque: Pythons Regel (seit 2026-10-03, absichtlich anders als vorher).** Nachtrag A9 der
Parity-Spezifikation (Lead-Sitzung, 2026-10-03; ersetzt Nachtrag A4, dessen Prämisse widerlegt ist: auch
typesafe-dev master ersetzt `RX_OPAQUE` durch einen linearen Matcher, der dieselbe Regel nachbildet). Nach
dem Kriterium der Person (maskiert mehr) gilt Pythons Regel: Ein längster Lauf R aus `[A-Za-z0-9_+/-]`
(danach bis zu zwei `=` als Polster) wird `<redacted>`, wenn R mindestens 24 Zeichen hat, einen
ASCII-Buchstaben enthält und entweder eine ASCII-Ziffer enthält oder direkt hinter R, vor jedem `=`, eine
Dezimalziffer irgendeiner Schrift (Unicode Nd) steht. So liest `RX_OPAQUE` mit Unicode-`\d` im Lookahead
(typesafe-dev 752698b als Regex, master 9a30e8d als `_LinearOpaqueMatcher`; beide gleich); Go verlangte bis
dahin eine ASCII-Ziffer im Lauf (`maskOpaque`, `opaqueRunQualifies`).

| Text (R: 24 Token-Zeichen mit Buchstaben, ohne Ziffer) | jetzt | vorher |
|---|---|---|
| R + „١“ (ebenso U+06F3, U+0966, U+FF13, U+1D7CF) | `<redacted>١` | unverändert |
| R + „١=“ | `<redacted>١=` | unverändert |
| R + „=١“, R + „==١“, R + „ ١“, „١“ + R, 23 Zeichen + „١“, R + „²“ oder „Ⅻ“ | unverändert | unverändert |
| R + ungültiges UTF-8 (etwa das erste Byte von „١“ allein) | unverändert: keine Ziffer, Python sieht dort U+FFFD oder ein Surrogat | unverändert |
| R + „-“ + Postleitzahl in arabisch-indischen Ziffern + „ Berlin“ | R + `-<address>`: der Adress-Schritt nimmt die Ziffer vorher weg | ebenso |

Geprüft: `TestMaskOpaqueExamples` (36 Fälle; die Erwartung von 33 ist die Ausgabe von
`ts_common.mask_detail` beider Python-Fassungen ohne Namensdatei, 2026-10-03; 3 nur in Go: ungültiges UTF-8
und U+11DE0), `TestMaskDifferential` gegen `refOpaqueStep`, `RX_OPAQUE` Stelle für Stelle mit beiden
Lookaheads ausgeschrieben, mit A9-Fragmenten im Korpus. Fünf Mutanten (Ziffer hinter dem Lauf übergangen,
Ziffer erst hinter dem Polster gesucht, Referenz nur mit ASCII-Ziffern, drei `=` Polster, eine Ziffer vor
dem Lauf zählt) lassen beide Tests rot werden. `MaskDetail` gegen beide Python-Fassungen, Ausgabe und alle 7
Zähler, gemessen 2026-10-03 (die Korpora liegen nur in der Sitzung):

| Korpus | Texte | verschieden vorher | jetzt |
|---|---|---|---|
| gezielt: Lauflängen 23, 24, 25, 40, mit und ohne ASCII-Ziffer, 0 bis 3 `=`, davor und dahinter ASCII-Ziffer, U+0661, U+06F3, U+0966, U+FF13 (dahinter auch U+1D7CF), Buchstaben, Leerzeichen; Ränder an Platzhaltern früherer Schritte; 16 Namen | 19.712 | 764, alle `opaque` | 0 |
| Mischkorpus der Prüfung vom 2026-10-02, 13 Namen | 24.295 | 0 | 0 |
| Texte des Goldkorpus, seine 16 Namen | 6.686 | 0 | 0 |

Der Goldkorpus schließt die Form noch aus (Ausschluss A4 im Generator von typesafe-dev), bis er neu erzeugt
ist; die A4-Zeile in `tools/typesafe/README.md` bleibt bis zum nächsten Abgleich mit typesafe-dev stehen
(hier nicht geändert). (*überholt am 2026-10-03: mit typesafe-dev b64a949 abgeglichen, A9 ersetzt A4 im
Generator, der Goldkorpus hat jetzt 7.269 Fälle (sha256 7b52aae2…); die A4-Zeile in
`tools/typesafe/README.md` ist weg aus „Bekannte Folgen und Grenzen“, der alte Text steht jetzt mit Grund in
„Überholte Regeltexte“, eine neue Zeile „Token (opaque)“ trägt A9; siehe auch die A4-Zeile (jetzt mit dem
A9-Hinweis) weiter unten in „Bekannte Grenzen“ und „Goldkorpus neu erzeugen“.*) Grenze: Ziffern, die erst
Unicode 17 vergibt, siehe „Bekannte Grenzen“.

**Kosten** (`TestMaskProfile`, `GOMAXPROCS=2`, Namensdatei mit 2 Namen, vorher = c9ed39a, je zwei Läufe
abwechselnd in einer Sitzung am 2026-10-03, Last 8,5 bis 4,3):

| 16 MiB | gesamt vorher → jetzt | Opaque-Schritt vorher → jetzt |
|---|---|---|
| UUIDs, Hashes (`uuidhash`) | 865–868 → 794–802 ms | 163–169 → 92–93 ms |
| Großbuchstaben + Ziffern (`capsdigits`) | 889–894 → 876–890 ms | 135–138 → 94–96 ms |
| Ziffernläufe (`digitrun`) | 401–407 → 396–398 ms | 25 → 21 ms |
| Personendaten dicht (`pii`) | 1.134–1.145 → 1.126–1.131 ms | 29–30 → 31–32 ms |
| `schluessel=wert` dicht | 1.260–1.262 → 1.243–1.247 ms | 45–46 → 48–49 ms |
| Token-Läufe ohne Ziffer, oft vor einer Ziffer anderer Schrift (Korpus nur in der Sitzung) | 867–875 → 823–830 ms | 144 → 92–99 ms |

Der Opaque-Schritt ist schneller: ein Lauf, der nicht zählt, geht nicht mehr als Treffer durch die
Ersetzung (die alte las jede Rune und gab ihn unverändert zurück), und die Suche nach Buchstabe und Ziffer
endet, sobald beide gefunden sind. Ungünstigster Fall des schnellen Wegs bleibt dichtes `schluessel=wert`,
74 ns je Byte (vorher 75); keine Konstante geändert.

**Adressen: Vereinigung beider Grammatiken (seit 2026-10-02, absichtlich anders als vorher).** Entscheid
der Person vom 2026-10-02 (Parity-Spezifikation Abschnitt 4, zweimal als Auswahlfrage bestätigt; Nachtrag
A5): eine Straße mit Hausnummer oder eine Postleitzahl mit Ort ist, was Pythons Grammatik
(`ts_common.py`) oder Gos bisherige erkennt. Leerraum S, Ziffer D und die Wortgrenze sind die von Python
(Unicode); die übrigen Klassen bleiben wörtlich, jedes `(?i)` setzt I, i, İ und ı gleich. Ein Treffer
braucht eine Wortgrenze an beiden Enden; es gewinnt der früheste Anfang, dort das längste Ende. Erst laufen
die Straßen über den ganzen Text, dann die Postleitzahlen über das Ergebnis. Alt gegen neu, gemessen am
2026-10-02 mit `MaskDetail` beider Stände (4b4487b und dieser), ohne Namensdatei:

| Text (PLZ: fünf Ziffern) | jetzt | vorher |
|---|---|---|
| PLZ + „Bad Homburg“, PLZ + „Halle/Saale“ | `<address>` | `<address> Homburg`, `<address>/Saale` |
| „PLZ ist“ + PLZ + „Berlin im Brief.“ | `PLZ ist <address>.` | `PLZ ist <address> im Brief.` (*überholt: so stand es in `TestMaskAddresses`*) |
| „Hauptstraße 12 b“, „Weg 3 / 4 c“ | `<address>` | `<address> b`, nicht maskiert |
| „string 3“, „during 2“ (Pythons Zweig in Kleinbuchstaben) | `<address>` | nicht maskiert |
| „Kirchstieg 3“, „Gässchen 4“, „Alter Markt 5“ (Endungen nur in Python) | `<address>` | nicht maskiert |
| „Musterstraße“ + U+00A0 + „12“, „Hauptweg ١٢“, PLZ in arabisch-indischen Ziffern + „Berlin“ | `<address>` | nicht maskiert |
| „Ku'damm 3“ | `Ku'<address>` | nicht maskiert |
| PLZ + „München1“, „äMusterstraße 12“, „Musterstraße 12ä“ | nicht maskiert: jede Grenze teilte ein Wort | `<address>nchen1`, `ä<address>`, `<address>ä` |
| „Im Jahr 2024 wurde“, „Am Montag 12 Uhr“, „Im PR 41 gefixt“ | `<address> wurde`, `<address> Uhr`, `<address> gefixt` | ebenso |

**Bekannte Folgen.** Gos Präpositions-Zweig bleibt (Nachtrag A5, Entscheid der Person, obwohl er Fließtext
maskiert): „Im Jahr 2024 wurde“ wird `<address> wurde`; in Go war das schon so, in Python ist es neu.
Pythons Zweig in Kleinbuchstaben maskiert Wörter, die auf eine Endung ausgehen, vor einer Zahl („string 3“,
„during 2“). In NFD (zerlegte Umlaute) ist ein Kombinationszeichen kein Wortzeichen, davor liegt also eine
Wortgrenze: aus PLZ + „Mu“ + U+0308 + „nchen“ wird `<address>` + U+0308 + „nchen“, der Ortsname bleibt halb
stehen, in Python ebenso (Grenze, nicht behoben; Abschnitt 4 definiert die Grenze über W) (*überholt am
2026-10-02 durch die NFC-Vereinigung, siehe „Parität mit ts_common.py“: Straße und PLZ + Ort suchen auch auf
einer eigenen NFC-Kopie, PLZ + „Mu“ + U+0308 + „nchen“ wird `<address>`, in beiden Sprachen; offen bleibt
eine Marke nach dem letzten Buchstaben des Orts, dort unter „Bekannte Grenzen“*). „Gäßchen 4“
allein bleibt unmaskiert: Gos Endung „gäßchen“ verlangt einen Großbuchstaben davor, Pythons heißt
„gässchen“ (wörtlich nach Spezifikation, in beiden Sprachen gleich, vorher in Go ebenso).

**Suche** (`tools/imprint-dev/mask_address.go`, nach der Idee des Python-Zweigs, typesafe-dev 3fa4dfe): jede
Straßen-Grammatik ist Vorsatz, S+, Hausnummer; kein Vorsatz enthält S vor D oder endet auf S, jede
Hausnummer beginnt mit D. Die Hausnummer eines Treffers beginnt also am ersten S-Lauf vor einem D nach
seinem Anfang (Anker). Je Anker läuft ein Automat der umgekehrten Vorsätze zurück bis zum D des Ankers
davor, einer der Hausnummern vorwärts; Postleitzahlen beginnen mit fünf D und S nach einem
Nicht-Wortzeichen. Die Automaten sind deterministisch, werden aus den mit `regexp/syntax` übersetzten
Grammatiken (dieselben Zeichenklassen wie Gos `regexp`) erst bei Bedarf gebaut und von allen Aufrufen eines
Prozesses geteilt; ganz gebaut haben sie 237, 20 und 80 Zustände. Der Aufwand ist linear; einen alten Weg
mit `regexp` gibt es für Adressen nicht mehr, auch nicht bei ſ, K, ẞ, İ, ı oder ungültigem UTF-8.
`judge` ist ein Prozess je Aufruf, die Automaten fangen also jedes Mal leer an: der erste `MaskDetail`-Aufruf
eines Prozesses kostet 0,44–0,65 ms mehr (Grundgerüst 0,35–0,52 ms), alle Zustände zu bauen 4,0–5,6 ms
(gemessen 2026-10-02, `GOMAXPROCS=2`, 5 Läufe). Dieser feste Betrag steht außerhalb des Preises je Byte im
Budget; er ist kleiner als 0,2 % der kürzesten Deadline (3 s).

**Geprüft:** `TestMaskAddressExamples` (36 Fälle; die Erwartung ist die Ausgabe von `ts_common.mask_detail`
des Python-Zweigs, typesafe-dev 6fc6326, dessen Adress-Code der von 3fa4dfe ist, ohne Namensdatei;
*überholt am 2026-10-02 durch die NFC-Vereinigung für die zwei NFD-Fälle, PLZ + „Mu“ + U+0308 + „nchen“
und „Mu“ + U+0308 + „hlenweg 3“: sie erwarten seit aa9d2d4 `<address>`; am 2026-10-02 gegen
`ts_common.mask_detail` von typesafe-dev b340863 nachgeprüft, ohne Namensdatei: gleich, ebenso alle Fälle
von `TestMaskNFCUnion`*);
`TestMaskAddresses` (die Zeile „im Brief“ als überholt markiert, die alte Erwartung im Kommentar);
`TestMaskDifferential` gegen `refAddressStep` (*seit der NFC-Vereinigung, 2026-10-02, `refAddressSpans`
unter `refUnionStep`*), die Regel der Spezifikation wörtlich (jeder Anfang mit
Wortgrenze von links, dort das größte Ende mit Wortgrenze, gefunden über die längsten Treffer von `regexp`
auf immer kürzeren Texten), mit den Beispielen als Fragmenten und vier Adress-Korpora zu 16 KiB;
`TestAddressAutomata` (alle Zustände gebaut, Schranke 400/40/160, sonst rot; die Zeichenklassen gegen
`MatchRune` auf jedem Zeichen); `TestAddressAutomataConcurrent` (geteilte Automaten ab leerem Zustand, viele
Goroutinen, jedes Ergebnis wie allein; der Test fing einen Fehler in der ersten Fassung, den der
Race-Detector und der Differentialtest nicht fingen: ein Übergang zu einem Zustand, den die eigene Sicht
noch nicht kannte). Einmal am 2026-10-02 gegen Python (typesafe-dev 6fc6326, `python3 -B`, Namensdatei
nicht vorhanden): 13.070 adressreiche Texte (alle Beispiele mit Kontext, Fließtext mit Präpositionen,
Straßen und Postleitzahlen lateinisch, mit Umlaut, in NFD, groß und klein, Ziffern und Leerraum anderer
Schriften, Ketten bis 300 Wörter, Suppe), 8.467 Adress-Treffer auf beiden Seiten, 0 Unterschiede im
Adress-Schritt allein und 0 in der ganzen Maskierung samt Zählung; die Grammatik-Konstanten in `typesafe.go`
stimmen zurückübersetzt (`pySpaceClass` → `\s`, `\p{Nd}` → `\d`, `turkishI` → `i`) Zeichen für Zeichen mit
`ts_common.py` überein (seit 2026-10-02 bei jedem Lauf mit python3 geprüft: `TestGrammarSyncWithTsCommon`,
siehe „Parität mit ts_common.py“).

**Kosten** (`TestMaskProfile`, `GOMAXPROCS=2`, 2 und 16 MiB, vorher = Stand 4b4487b mit denselben Korpora,
in einer Sitzung am 2026-10-02 gemessen; neue Korpora `streetchain`, `anchors`, `lowerrun`,
`unicodeaddr`, `maskAddressKinds`):

| 16 MiB, ohne Namen | vorher | jetzt | davon Adress-Schritte jetzt |
|---|---|---|---|
| Straßenadressen dicht | 2,08 s (124 ns je Byte) | 0,62 s (37 ns) | 0,25 s |
| Wortketten vor Hausnummer (`streetchain`) | 15,7 s (934 ns) | 0,47 s (28 ns) | 0,20 s |
| Zahl alle paar Bytes (`anchors`) | 1,19 s (71 ns) | 0,72 s (43 ns) | 0,27 s |
| lange Kleinbuchstaben-Läufe (`lowerrun`) | 0,67 s (40 ns) | 0,55 s (33 ns) | 0,27 s |
| S und D anderer Schriften (`unicodeaddr`) | 0,59 s (35 ns) | 0,64 s (38 ns) | 0,34 s |
| Personendaten dicht | 1,14 s (68 ns) | 1,03 s (61 ns) | 0,10 s |
| `schluessel=wert` dicht | 1,24 s (74 ns) | 1,22 s (73 ns) | 0,05 s |

Ungünstigster Fall des schnellen Wegs jetzt dichte `schluessel=wert` mit 77 ns je Byte (mit Namensdatei;
vorher dichte Straßenadressen mit 130 ns, und die erst jetzt gemessenen Wortketten mit 937 ns) (*überholt
am 2026-10-02 durch die NFC-Vereinigung (aa9d2d4): Text, der nicht NFC ist und „@“ oder Ziffern hat, ist
jetzt langsamer, `nfdanchors` 116 ns je Byte bei 2 MiB; dichte `schluessel=wert` bleibt der langsamste
Text in NFC, 79 ns, in einer zweiten Sitzung 84; siehe „Bekannte Grenzen“, Zeile „NFC-Vereinigung
kostet“*). Mit ſ oder ı
davor höchstens 266 ns je Byte (`schluessel=wert`; vorher 467 ns, dichte Straßenadressen). Keine Konstante
geändert: 200 ns und 900 ns je Byte decken beide Wege mit Abstand.

**Namen in NFC (seit 2026-10-02, absichtlich anders als vorher).** Parity-Spezifikation Abschnitt 5 mit
den Nachträgen A1, A2 und A8 der Lead-Sitzung vom 2026-10-02: Namen und Text werden in Unicode-NFC
verglichen, wie `ts_common._mask_names` es tut. Gemessen am 2026-10-02 mit `MaskDetail` beider Stände
(fa6fa18 und dieser), die Namensdatei jeweils nur mit dem genannten Namen:

| Namensdatei | Text | jetzt | vorher |
|---|---|---|---|
| „José“ | „Jose“ + U+0301 + „ kommt“ (zerlegt, NFD) | `<name> kommt` | nicht maskiert |
| „Jose“ + U+0301 (zerlegt gespeichert) | „José kommt“ | `<name> kommt` | nicht maskiert |
| „İlker“ | „I“ + U+0307 + „lker kommt“ | `<name> kommt` | nicht maskiert |
| „Ö“ allein | „Ö und O“ + U+0308 | nicht maskiert (A2: kein Name) | `<name> und O` + U+0308 |
| „Oz“ | „Oz“ + U+0F74 U+0F73 + „ kommt“ | `<name> kommt`: das Segment, das NFC ändert, geht ganz mit | `<name>` + U+0F74 U+0F73 + ` kommt` |
| „Oz“ | „Oz“ + U+0B3E + „ und Oz“ | `<name>` + U+0B3E + ` und <name>`: NFC lässt das Segment, wie es ist, nichts wird verbreitert (A8) | ebenso |
| „Klaus“ | „Hallo “ + Kelvin-Zeichen (U+212A) + „laus!“ | `Hallo<name>!` | `Hallo <name>!` |
| „Max Mustermann“ | „Herr Mu“ + U+0308 + „ller und Max Mustermann“ | `Herr Mu` + U+0308 + `ller und <name>`, der Rest Byte für Byte | ebenso |

- Namensdatei: jede Zeile wird in NFC gebracht, dann bleibt ein Begriff mit mindestens zwei Codepunkten
  (A2; vorher zählte Go Bytes, ein einzelnes „Ö“ war ein Name). Benannte Ausnahme vom Kriterium „maskiert
  mehr“: ein einzelner Buchstabe identifiziert niemanden und maskierte jedes einzelne „Ö“.
- Text in NFC (aller ASCII-Text und fast jeder andere): der bisherige Weg, unverändert.
- Anderer Text (`tools/imprint-dev/mask_nfc.go`): ein Segment beginnt vor jedem Zeichen mit kanonischer
  Kombinationsklasse 0 und NFC_Quick_Check Yes (A1, ICUs „boundary before“; über so ein Zeichen hinweg
  wird nichts umgeordnet oder zusammengesetzt); das Finden ist linear. Die Kopie ist die NFC jedes
  Segments; auf ihr laufen die Namen wie bisher (Name für Name, längster zuerst, faltungs-kanonisch,
  Wortgrenzen auf der Kopie). Zurück ins Original: in einem Segment, das NFC nicht ändert, Byte für Byte;
  eine Trefferkante in einem Segment, das NFC ändert, rückt an dessen Rand (A8, nur dort; so hängt das
  Ergebnis einer Stelle nicht davon ab, ob anderswo im Text etwas nicht NFC ist). Überlappende Spannen
  werden ein `<name>`, jeder Treffer zählt. Außerhalb der Spannen bleibt das Original Byte für Byte.
- Ungültiges UTF-8: keine NFC, wie bisher.

**Bekannte Folgen.** Ein Zeichen, das NFC durch ein anderes ersetzt (Kelvin-, Angström-, Ohm-Zeichen), ist
keine Segmentgrenze; das Zeichen davor gehört zu seinem Segment und geht mit: „Hallo “ + Kelvin-Zeichen +
„laus“ wird `Hallo<name>` (maskiert mehr, zerteilt kein Wort; A8 lässt das bewusst stehen). Ein Name, der
in einem früheren Platzhalter trifft (`name` in `<name>`), verhält sich wie auf NFC-Text (`<<name>>`).
Adressen werden nicht normalisiert (Abschnitt 4; siehe „Adressen“, NFD) (*überholt am 2026-10-02 durch die
NFC-Vereinigung, siehe „Parität mit ts_common.py“: E-Mail, Straße und PLZ + Ort suchen zusätzlich auf je
einer eigenen NFC-Kopie, mit denselben Segmenten und derselben Verbreiterung wie hier; anders als bei Namen
gelten auch die Treffer im Text selbst, und jede verschmolzene Spanne zählt einmal*).

**Geprüft:** `TestNamesNFC` (26 Texte; die Erwartung ist die Ausgabe von `ts_common.mask_detail` des
Python-Zweigs, typesafe-dev 752698b, mit derselben Namensdatei; die Referenz stimmt überein, und die
aufgezeichneten Ersetzungen ergeben die Namenszahl), `TestNamesNFCLoad`, `TestNamesNFCKeepsOriginal`
(zerlegter Text und ungültiges UTF-8 außerhalb der Spannen Byte für Byte), `TestRefQCMaybeStarters` (die
Liste der Referenz, aus `ts_common.py` kopiert, ist auf jedem Codepunkt die der Tabellen; die Segmentregel
der Referenz ist `nfcBoundary`). `TestMaskDifferential` gegen `refNamesStep`, eine schlichte eigene Fassung
(Segmente Zeichen für Zeichen, `nfc` je Segment, Herkunft je Byte), die den allgemeinen Weg auf jeden
gültigen Text anwendet, NFC-Text eingeschlossen: damit ist auch geprüft, dass der schnelle Weg dort genau
der allgemeine ist (A8); dazu eine sechste Namensdatei (zerlegt gespeicherte Namen, „Ö“ und „O“ + U+0308
allein, `name`, „Mustermann-“, Hangul als Jamo), Fragmente (NFD, Quick-Check-Maybe, Jamo, Kelvin-,
Angström-, Ohm-Zeichen, „I“ + U+0307, Marken außer Ordnung, „a“ + U+0F73, `>` + U+0338 hinter einem
Platzhalter) und fünf Korpora zu 16 KiB (`maskNFCKinds`). Einmal am 2026-10-02 gegen Python (typesafe-dev
752698b, `python3 -B`): der Goldkorpus `tests/mask-golden.json` (6.686 Fälle mit seiner Namensliste,
sha256 2c7d83da…) und `tests/mask-parity-cases.json` (96 Fälle) durch `MaskDetail`: 0 Unterschiede in
Ausgabe und Zählung (vorher 571 und 8: 72 Fälle mit anderer `secret_kw`-Zahl aus A3, 378 mit anderer
Namenszahl, 121 nur in der Ausgabe; seit 2026-10-02 läuft der Goldkorpus bei jedem `go test` mit:
`TestMaskGolden`, siehe „Parität mit ts_common.py“). (*überholt am 2026-10-03: Goldkorpus seither bei
typesafe-dev b64a949, 7.269 Fälle, sha256 7b52aae2…; diese Messung bleibt bei ihrem Stand 752698b/6.686.*)
Mutationsprüfung: 12 Mutanten (immer verbreitern; schon bei
Berührung zusammenlegen; Bytes statt Codepunkte; `<redacted>` ohne Groß-/Kleinschreibung; Segmentgrenze
ohne Quick-Check; Budget nur aus dem Text; Log mit ASCII-`\s`; Log ohne zweites `<redacted>`; ein
angeschnittener Platzhalter nicht gekürzt; Suche Name für Name ohne Aufzeichnung; Kopie statt Original
außerhalb der Spannen; Namen ohne NFC beim Laden), alle gefangen; umgekehrt besteht der allgemeine Weg
auf jedem gültigen Text, ohne den schnellen, alle Namens- und Differentialtests.

**Kosten** (`TestMaskProfile`, `GOMAXPROCS=2`, Namensdatei mit 2 Namen, vorher = fa6fa18 mit denselben
Korpora, zwei Sitzungen am 2026-10-02 mit je vorher und nachher; Zahlen der ruhigeren zweiten):

| 16 MiB, nicht NFC | vorher | jetzt | Namens-Schritt vorher → jetzt |
|---|---|---|---|
| Deutsch zerlegt (`nfdgerman`) | 0,80 s (48 ns je Byte) | 1,03 s (61 ns) | 0,23 → 0,47 s |
| Hangul als Jamo (`jamo`) | 0,60 s (36 ns) | 0,73 s (43 ns) | 0,20 → 0,33 s |
| lange Markenläufe außer Ordnung (`markrun`) | 0,64 s (38 ns) | 1,12 s (67 ns) | 0,24 → 0,73 s |
| „a“ + U+0F73 × bis 4000 (`tibetan`) | 0,44 s (27 ns) | 1,02 s (61 ns) | 0,11 → 0,69 s |
| ein einziges Segment „a“ + U+0F73 × 5,6 Mio. (`f73one`) | 0,44 s (26 ns) | 1,03 s (61 ns) | 0,10 → 0,68 s |

Linear: der Namens-Schritt auf `f73one` kostet je Byte bei 256 KiB, 1, 4 und 16 MiB 34, 43, 42 und 41 ns
(ein Segment mit 87.000 bis 5,6 Millionen U+0F73; Pythons alte Segmentregel brauchte nach P1 35 s für
16.000). Mit „I“ + U+0307 vor jedem Korpus (der ganze Text nicht NFC) höchstens 79 ns je Byte (vorher 77,
dichte `schluessel=wert`). Text in NFC: unverändert, höchstens 77 ns je Byte (vorher 76), der
Namens-Schritt kostet die
Schnellprüfung, unter 1 ns je Byte. Mit ı davor (alter Weg der anderen Schritte) höchstens 221 ns (vorher
199); in der ersten, belasteten Sitzung (Last bis 8,9) einmal 424 ns, derselbe Text in der zweiten 105 ns.
Keine Konstante geändert; neu ist im Budget, dass die Namen über die Kopie laufen (siehe „Maskier-Budget“).

Gleichheit: `TestMaskDifferential` lässt die alte Fassung (`mask_reference_test.go`, nur in Tests) und
die neue auf dieselben Texte los, mit 5 Namensdateien (keine, 2 Namen, 9 Namen mit Überlappungen und
Platzhalter-Namen wie `name`, 50 Namen, griechische und kyrillische Namen mit Angström); Ausgabe und
Zählung müssen Byte für Byte gleich sein. Der Namens-Schritt der alten Fassung ist seit der Wortgrenzen-
Korrektur eine eigene, schlichte Fassung der neuen Regel (`refNamesStep`); alle anderen Schritte sind
die alten (*überholt am 2026-10-02: seit der Maskier-Parität sind auch Bearer/Basic (`refBearerStep`),
Schlüssel=Wert (`refSecretKWStep`, ein Matcher, der Pythons Rückverfolgung samt Lookahead Schritt für
Schritt nachgeht; das Produktionsmuster drückt den Lookahead als reguläre Menge aus), Telefon und die
türkische Faltung eigene, schlichte Fassungen*). Im normalen Lauf 3.247 Texte, 3,0 MB, 16.235 Vergleiche;
einmal mit `IMPRINT_MASK_DIFF_SEEDS=400`: 41.122 Texte, 39 MB, 205.610 Vergleiche, alle gleich (*überholt am
2026-10-02: bis dahin las der Test das Feld `input` statt `text` aus `mask-parity-cases.json`, die 40
Paritätsfälle gingen als leere Texte ein; jetzt scheitert er, wenn keiner geladen wird. Mit ihnen und den
Fragmenten der Maskier-Parität im normalen Lauf 3.517 Texte, 3,0 MB, 17.585 Vergleiche; mit
`IMPRINT_MASK_DIFF_SEEDS=400` 41.392 Texte, 39 MB, 206.960 Vergleiche, alle gleich*) (*überholt am
2026-10-02 durch die Adress-Vereinigung: der Adress-Schritt der alten Fassung ist jetzt `refAddressStep`,
die Regel der Spezifikation wörtlich; mit den Adress-Fragmenten und vier Adress-Korpora zu 16 KiB im
normalen Lauf 3.659 Texte, 3,1 MB, 18.295 Vergleiche; mit `IMPRINT_MASK_DIFF_SEEDS=400` 41.534 Texte,
39 MB, 207.670 Vergleiche, alle gleich*) (*überholt am 2026-10-02 durch die Namen in NFC und Nachtrag A3:
`refNamesStep` wendet den allgemeinen NFC-Weg auf jeden gültigen Text an, `refLoadNames` lädt die Namen
selbst, die Lookaheads von `refSecretKWStep` vergleichen genau; mit 6 Namensdateien, den NFC-Fragmenten
und fünf NFC-Korpora im normalen Lauf 3.793 Texte, 3,2 MB, 22.758 Vergleiche; mit
`IMPRINT_MASK_DIFF_SEEDS=400` 41.668 Texte, 39 MB, 250.008 Vergleiche, alle gleich*) (*überholt am
2026-10-02 durch die NFC-Vereinigung: E-Mail, Straße und PLZ laufen in der alten Fassung durch
`refUnionStep` (`refEmailSpans` und `refAddressSpans` auf dem Text und auf `refNFCCopy`, die Herkunft je
Byte wie in `refNamesStep`), wie dort der allgemeine Weg auf jedem gültigen Text; mit den NFD-Fragmenten
und den Korpora `nfdpii` und `nfdanchors` im normalen Lauf 3.917 Texte, 3,2 MB, 23.502 Vergleiche; mit
`IMPRINT_MASK_DIFF_SEEDS=400` 41.792 Texte, 39 MB, 250.752 Vergleiche, alle gleich, gemessen nach 5e77bad*) (*überholt am 2026-10-03 durch Nachtrag A9: der Opaque-Schritt der alten
Fassung ist `refOpaqueStep`, `RX_OPAQUE` Stelle für Stelle; mit den A9-Fragmenten im normalen Lauf 3.888
Texte, 3,2 MB, 23.328 Vergleiche; mit `IMPRINT_MASK_DIFF_SEEDS=400` 41.763 Texte, 39 MB, 250.578 Vergleiche,
alle gleich, 38,5 s, vorher 44,2 s*) (*Stand 2026-10-03 auf dem Zweig `mask/stufe1-parity-kv-nfd`, A9 und
NFC-Vereinigung zusammen, mit den Fragmenten zum Schlüssel=Wert-Rest: im normalen Lauf 3.974 Texte
(99 Parity-Fälle), 3,2 MB, 23.844 Vergleiche, alle gleich*). Die Python-Parität
(`mask-test.py`, 13/13) läuft
unverändert.

**Maskier-Budget.** Ein Aufruf darf für das Maskieren im ungünstigsten Fall höchstens die Hälfte der
verbleibenden Deadline brauchen. Jedes Feld kostet seine Länge mal den ungünstigsten Preis je Byte auf
dem Weg, den es nimmt (`maskNsPerByte`):

| Feld | Preis je Byte | gemessen höchstens |
|---|---|---|
| gültiges UTF-8 ohne Sonderzeichen (schneller Weg) | 200 ns + 1,5 ns je Namensmuster | 127 ns (bei Last 156, einmal 232) + 0,9 ns (*überholt am 2026-10-02: Wortketten vor einer Hausnummer kosteten 937 ns, siehe „Adressen“; seit der Adress-Vereinigung höchstens 77 ns*) |
| mit K, ſ, ẞ, İ, ı, Ohm-, Angström-Zeichen … | 900 ns + 20 ns je Namensmuster | 705 ns + 15,7 ns (*seit der Adress-Vereinigung, 2026-10-02, mit ſ oder ı davor höchstens 266 ns*) |
| ungültiges UTF-8 | 900 ns + 20 ns je Namensmuster | 705 ns + 15,7 ns |
| gültiges UTF-8, nicht in NFC, mit Namen (seit 2026-10-02) | wie oben, nur der Preis je Namensmuster mal Länge der NFC-Kopie durch Länge des Textes; 20 ns statt 1,5, wenn die Kopie ein Zeichen wie İ enthält, das der Text nicht hat („I“ + U+0307) | 79 ns (dichte `schluessel=wert` hinter „I“ + U+0307; die fünf NFC-Korpora 69 ns; Kopie bauen und zurückbilden eingeschlossen) (*überholt am 2026-10-02 durch die NFC-Vereinigung: dichte NFD-Adressen und -Mails (`nfdanchors`) 116 ns bei 2 MiB, 109 ns bei 16 MiB; Preis unverändert, erlaubt sind dort 208 ns; Zahlen aus der Commit-Nachricht von aa9d2d4, siehe „Parität mit ts_common.py“, „Bekannte Grenzen“*) |

Für Text, der nicht in NFC ist, baut `maskNsPerByte` die Kopie selbst (einmal mehr `nfc`, außerhalb des
Budgets: gemessen 2026-10-02 höchstens 32 ns je Byte, 0,5 s für 15 MiB „a“ + U+0F73 oder Markenläufe;
`maskCapped` lehnt deshalb vorher ab, was schon zum niedrigsten Preis, 200 ns, über dem Budget liegt: 15 MiB
solchen Texts bei 3 s in 56–116 ms `too_large` statt in 0,24–0,62 s, 6 MiB in 0,43–0,68 s geurteilt; im
Namens-Schritt kostet derselbe Aufbau mit dem Zurückbilden höchstens 46 ns je Byte): Kopien sind bis
doppelt so lang („a“ + U+0F73), und auf einer Kopie mit İ
werden die Namen einzeln gesucht (`TestJudgeBudgetCoversNFCNames`: zerlegtes Türkisch mit 72
Namensmustern bei 3 s mit 4,4 MB sofort `too_large`, mit 0,9 MB in 0,13 s geurteilt). Für reinen Text
heißt das mit 2 Namen: 3 s (PostToolUse, SubagentStop) → 7,2 MB, ungünstigster Fall
gemessen 0,90–0,95 s (31 %); 6 s (Stop) → 14,4 MB, 1,79 s (30 %) (`maskBudgetBytes`). Mit 127 ns statt
200 ns wäre das Budget für 3 s etwa 11 MB; 200 ns lassen Spielraum für Last. Mit einem ſ davor gemessen:
1,3 MB in 0,46–0,92 s geurteilt, 6,5 MB sofort `too_large` (`TestJudgeBudgetCoversFallbackRunes`); 50
griechische Namen auf griechischem Text: 5,9 MB in 0,3 s, mit Ohm-Zeichen 3,6 MB sofort `too_large`
(`TestJudgeBudgetCoversNamesWithoutASCII`); nie `timeout`. Nachgemessen am 2026-10-02 nach der
Maskier-Parität (`TestMaskProfile` mit `IMPRINT_MASK_PROFILE_PREFIX` und `_KINDS`, 2 und 16 MiB, vorher und
nachher in einer Sitzung): schneller Weg höchstens 125 ns je Byte (vorher 127), mit ſ oder ı davor höchstens
459 ns (vorher 450; dichte `schluessel=wert` 287 ns, vorher 194), Namen auf türkischem Text Name für Name
6,1 ns je Muster; keine Konstante geändert. Türkischer Text mit İ oder ı wird damit zum Preis des alten
Wegs gerechnet: bei 3 s rechnerisch etwa 1,6 MB statt 7,2 MB, `TestJudgeBudgetCoversFallbackRunes` prüft İ und ı
mit (6,5 MB sofort `too_large`, 1,3 MB in 0,6 s geurteilt). Nachgemessen am 2026-10-02 nach der
Adress-Vereinigung (Tabelle unter „Adressen“): schneller Weg höchstens 77 ns je Byte, mit ſ oder ı davor
höchstens 266 ns; keine Konstante geändert, die Preise bleiben 200 und 900 ns. Dazu kommt je Prozess einmal
der Bau der Adress-Automaten, höchstens 5,6 ms gemessen, außerhalb des Preises je Byte. Seitdem nehmen
`TestJudgeDeadlineCoversBuild` und `TestJudgeBudgetCoversFallbackRunes` dichtes `schluessel=wert` statt
dichter Straßenadressen als langsamsten Text (gemessen 2026-10-02, je 3 Läufe: 6,5 MB in 0,52–0,54 s
geurteilt, vorher mit Adressen 0,28 s; nach ſ, K, İ oder ı 1,3 MB in 0,35–0,39 s, vorher 0,24–0,28 s;
6,5 MB nach einem solchen Zeichen sofort `too_large`) (*für `TestJudgeDeadlineCoversBuild` überholt am
2026-10-02 durch die NFC-Vereinigung: dort ist `nfdanchors` jetzt langsamer, der Test nimmt seit
2026-10-03 beide, siehe Zeile „Deadline“ oben; für `TestJudgeBudgetCoversFallbackRunes` gilt es weiter:
mit ſ davor dichte `schluessel=wert` 272 und 270 ns je Byte bei 2 und 16 MiB, `nfdanchors` 254 und 250,
erlaubt sind etwa 1.020 und 1.010; nur mit ſ gemessen, nicht mit K, ẞ, İ oder ı; Zahlen aus der
Commit-Nachricht von aa9d2d4*). Nachgemessen am 2026-10-03 nach Nachtrag A9
(Tabelle unter „Opaque: Pythons Regel“): schneller Weg höchstens 74 ns je Byte (vorher 75); keine Konstante
geändert. Mehr endet mit `error_class: too_large`, ohne
Anfrage, und der Fail-Modus entscheidet; bei einem Hinweis-Gate heißt das `allow`. **Grenze, nicht
behoben:** Wer einen fremden Text über das Budget aufbläht, entgeht `foreign_return`. Rohen Text vor dem
Maskieren zu kürzen, kommt nicht in Frage (ein Schnitt kann Passphrasen, IBANs und Adressen teilen).

Jedes Maskieren (auch kurze Felder und das zweite Maskieren in `PostState`) wartet höchstens bis zum Ende
der Deadline; läuft es länger (langsamerer Rechner), antwortet `judge` mit `timeout`, und die
Maskier-Goroutine läuft im Hintergrund aus, bis der Prozess endet (ein Prozess je Aufruf;
`TestJudgeMaskingStopsAtTheDeadline`, `TestMaskingStopsAtTheDeadlineOnSmallFields`). Beide Tests setzen
eine langsame `MaskDetail` ein und stellten sie am Testende zurück, während die Hintergrund-Goroutine sie
schon gelesen hatte, ohne Ordnung dazwischen: `go test -race` meldete das in beiden (gemessen
2026-10-02, schon in fa6fa18). Seitdem wartet jeder Test auf den Start jedes Maskier-Aufrufs, den er
erwartet (`slowMaskDetail`), bevor er zurückstellt; nur Testcode, `maskWithin` ist unverändert.

**Namensdatei.** Eine Zeile, die kein gültiges UTF-8 ist (eine Datei in Latin-1 gespeichert), wird als
Latin-1 gelesen, was immer gelingt; ihre Namen maskieren also weiter. Vorher ließ ein solcher Name jedes
Maskieren abbrechen. Die Logzeile zählt diese Zeilen in `names_latin1`, die Namen selbst stehen nie im
Log. Übersprungen werden nur leere Zeilen und Zeilen nur aus Steuerzeichen (`TestNamesFileLatin1`). Seit
2026-10-02 (Nachtrag A2) wird jede Zeile in NFC gebracht und ein Begriff erst ab zwei Codepunkten
behalten; vorher zählte Go Bytes (`TestNamesNFCLoad`; siehe „Namen in NFC“).
Groß-/Kleinschreibung: zwei Zeichen gelten als gleich, wenn sie in derselben Klasse von
`unicode.SimpleFold` liegen; die kanonische Kopie nimmt das Minimum der ganzen Klasse, für jedes Zeichen
geprüft (`TestFoldCanonExhaustive`, mit „Laΐs“/„LaΐS“ und „Steﬅn“/„Steﬆn“).

Kalibrierung, wie in `gates.json` vermerkt: `done` auf 8 synthetischen Fällen am 2026-09-29 mit dem
Alias `jev-latest` (welche Version er damals war: nicht festgehalten), nicht neu für `jev-1.13.0`.
`foreign_return`: nicht kalibriert.

## Parität mit ts_common.py (2026-10-02)

| Entscheid der Person | Was er festlegt | Wann, wie |
|---|---|---|
| Adressen: Vereinigung beider Grammatiken | Straße mit Hausnummer oder PLZ + Ort ist, was Pythons oder Gos bisherige Grammatik erkennt | 2026-10-02, Auswahlfrage in der Lead-Sitzung („Union of both“) |
| Vereinigung bleibt, obwohl sie Prosa maskiert | Gos Präpositions-Zweig bleibt: „Im Jahr 2024 wurde“, „Am Montag 12 Uhr“, „Im PR 41 gefixt“ werden `<address>` (in Go schon vorher, in Python neu; Paket P1 maß auf Pythons Mischkorpus 2.670 → 3.187 Adress-Treffer, 403 der +519 aus diesem Zweig) | 2026-10-02, zweite Auswahlfrage (Nachtrag A5) |
| NFC in Go aus Pythons Tabellen | `tools/imprint-dev/gen_nfc_tables.py` erzeugt `nfc_tables.go` aus `unicodedata` 16.0.0; `go.mod` bleibt ohne Abhängigkeit | 2026-10-02, Auswahlfrage („Tables from Python“) |
| E-Mail und Adressen: NFC-Vereinigung | Je Durchgang (E-Mail, Straße, PLZ + Ort) die Spannen auf dem Text vereinigt mit den Spannen desselben Suchers auf einer eigenen NFC-Kopie dieses Textes (Segmente und Verbreiterung wie bei Namen), verschmolzen nur bei echter Überlappung (angrenzende bleiben getrennt), ersetzt im Original, eine Zählung je verschmolzener Spanne; Text in NFC ergibt genau die Spannen im Text. Behebt Mail-Adressen, Straßen und Orte in NFD, ohne irgendwo weniger zu maskieren (nur auf der Kopie zu suchen hätte in 53 Goldfällen weniger maskiert: Prototyp-Messung der Lead-Sitzung, hier nicht nachgemessen). Regeltext: Zeile „NFC-Vereinigung (E-Mail, Straße, PLZ + Ort)“ im README; Go aa9d2d4, Python typesafe-dev 85fce61, 0f7f16a und b340863, hier übernommen in 5e77bad | 2026-10-02, Auswahlfrage in der Lead-Sitzung („Union“) |
| Budget bleibt bei 200 ns je Byte | `maskWorstNsPerByte` bleibt 200; der langsamste von 10 Läufen von `TestJudgeDeadlineCoversBuild` auf `nfdanchors` erreichte rund 218 ns je Byte gegen 208,3 erlaubte (1,41 s für 6,5 MB, ganzer Aufruf, nicht wörtlich in ns/Byte gemessen; Last etwa 12 auf 20 Kernen, 2026-10-03; die 3-s-Frist hielt, 8 von 10 Läufen 0,79–0,85 s für 6,5 MB) gilt als Lastschwankung, wie zuvor schon der Lauf mit 232 ns (Kommentar in `typesafe_state.go`); Quelle der Messung: Commit 41deba4 | 2026-10-03, Auswahlfrage in der Lead-Sitzung („Keep 200 ns/B“) |
| IBAN-Prüfziffern: Go folgt Python | Go liest die zwei Prüfziffern einer IBAN wie Pythons `\d` als jede Unicode-Dezimalziffer (Nd), nicht nur ASCII; umgesetzt in Stufe 2 der Masken-Arbeit, bis dahin bleibt die Abweichung offen (Zeile in „Bekannte Grenzen“) | 2026-10-03, Entscheid der Person, übermittelt mit dem Review der Stufe 1 |
| alles Übrige | das sicherere Verhalten maskiert mehr, ohne Wörter zu zerteilen | 2026-10-02, Kriterium der Person |

NFC-Vereinigung und Budget bleibt bei 200 ns je Byte: Einträge im Entscheidungsregister
`imprint/docs/umbau-2026-09-25/entscheide.md` stehen aus (Schreiber: Antigravity, CL-116; Stand 2026-10-02,
für „Budget bleibt bei 200 ns je Byte“ seit 2026-10-03).

`MaskDetail` (Go) und `ts_common.mask_detail` (Python, Kopie in `tools/typesafe/`) folgen denselben Regeln; diese stehen nur in der Tabelle „Datenschutz & Maskierung“ von [`tools/typesafe/README.md`](../tools/typesafe/README.md).

*Stand 2026-10-03 (Zweig `mask/stufe1-parity-kv-nfd`):* `tools/typesafe` ist seit 2026-10-03 die kanonische
Heimat von `ts_common.py`, keine Kopie mehr. Die Abgleich-Commits, die die Notizen dieses Abschnitts nennen
(c3e051c, 5e77bad und der `tools/typesafe`-Teil von 7ac4dbc), kamen nicht nach `main`; an ihrer Stelle steht
`ts_common.py` aus typesafe-dev b64a949 (Parität, A9), c906a98 (Schlüssel=Wert linear), 2a3155d
(NFC-Vereinigung) und 195b0dd (eigene Namensdatei in `test_tools.py`), zusammengeführt auf typesafe-dev
088aee5. Gleich sind die Regeln bis auf zwei Abweichungen, beide in „Bekannte Grenzen“: die IBAN-Prüfziffern
(Go folgt Python ab Stufe 2, bis dahin offen) und die Namensdatei (Nachtrag A7: wie sie gelesen wird und wie
überlappende Namen ersetzt werden).

| Geprüft durch (Go) | Was | ohne python3 |
|---|---|---|
| `TestMaskGolden` | Goldkorpus `tools/typesafe/tests/mask-golden.json` (Kopie aus typesafe-dev 752698b, 6.686 Fälle, 16 synthetische Namen; *überholt am 2026-10-02 durch die NFC-Vereinigung: Kopie aus typesafe-dev b340863, übernommen in 5e77bad, 7.063 Fälle, dieselben 16 Namen*; *überholt am 2026-10-03: mit typesafe-dev b64a949 abgeglichen, jetzt 7.269 Fälle, sha256 7b52aae2…*; *überholt am 2026-10-03, Zweig `mask/stufe1-parity-kv-nfd`: erzeugt in `tools/typesafe` aus dem zusammengeführten `ts_common.py` (7.646 Fälle, byte-gleich mit dem zusammengeführten Goldkorpus), dazu die Fragmente `kv_tail_*` und nur reservierte Beispiel-Domains (`.example`, `.test`, `.invalid`): 7.762 Fälle, 1.946.373 Bytes, sha256 0607b797…*): je Fall genau die Ausgabe und alle 7 Zähler. Rot bei 0 Fällen, bei einer anderen Fallzahl als im Kopf, bei fehlenden Zählern und wenn `unidata_version` des Kopfs nicht `nfcUnicodeVersion` ist (beide 16.0.0; Gos Tabellen stammen aus derselben `unicodedata`, also müssen sie gleich sein); beide Versionen stehen im Log | läuft |
| `TestMaskParity` | `tools/typesafe/tests/mask-parity-cases.json` (96 benannte Fälle; *seit 2026-10-03 99, 59 mit genauem `masked`*); seit 2026-10-02 `masked` genau, wo der Fall es hat (56), und rot bei 0 Fällen | läuft |
| `TestGrammarSyncWithTsCommon` | Straßen- und PLZ-Grammatiken (Vorsatz und Hausnummer, beide Endungslisten), Schlüsselwortliste, Schlüssel=Wert-Vorsatz und Bearer/Basic in `typesafe.go` gegen ihre Quelle in `ts_common.py`, nach der Abbildung `\s` → S, `\d` → D, `\w` → W, jedes I und i eines `(?i)`-Teils → `turkishI` (`pyToGo`; was die Abbildung nicht festlegt, ist ein Fehler) (*ergänzt am 2026-10-03: `\w` in einem `(?i)`-Teil, allein oder in einer Klasse, → `(?-i:…)`, denn Pythons `(?i)` faltet `\w` nicht, Gos schon (U+0345); eine solche Klasse mit einem Zeichen, das Groß/klein kennt, ist ein Fehler; `TestPyToGo`*); beide Seiten müssen zum selben Ausdruck parsen | SKIP mit Meldung |

Gemessen 2026-10-02: `TestMaskGolden` 6.686 von 6.686 Fällen gleich (0,07 s) (*überholt am 2026-10-02 durch
die NFC-Vereinigung: nach 5e77bad 7.063 von 7.063 gleich (0,08 s); von aa9d2d4 bis dahin rot auf genau den
vier Fällen `g/email/mark_after`, `g/email_plus/mark_after`, `g/email_umlaut/mark_after` und
`g/email_cyrillic/mark_after`, deren Erwartung erst der neue Goldkorpus nachzog*); `TestGrammarSyncWithTsCommon`
11 Paare, alle derselbe Ausdruck, 10 auch derselbe Text (Pythons Schlüssel=Wert-Vorsatz schreibt die Klasse
der Anführungszeichen `[\"']`, Go `["']`). (*überholt am 2026-10-03, nach dem Abgleich mit typesafe-dev
b64a949: `TestMaskGolden` 7.269 von 7.269 Fällen gleich (0,08 s), Header `tests/gen_mask_golden.py,
2026-10-03, Python 3.14.6, unidata_version 16.0.0`; `TestGrammarSyncWithTsCommon` unverändert 11 Paare.*)
Mutanten gefangen: `turkishI` nur `[i]` (197 Goldfälle anders,
`TestMaskParity` rot); „markt“ aus `streetSuffixPy`, „pwd“ aus der Schlüsselwortliste, `\f` aus der
Bearer-Wertklasse, in Pythons `_SUF_GO` „steig“ geändert (jeweils `TestGrammarSyncWithTsCommon` rot).
(*Gemessen 2026-10-03 auf dem Zweig `mask/stufe1-parity-kv-nfd`: `TestMaskGolden` 7.762 von 7.762 Fällen gleich,
`TestMaskParity` 99 Fälle, `TestGrammarSyncWithTsCommon` 11 Paare, 10 auch derselbe Text. Mutant: der Rest des
Schlüssels wieder als gefaltete Klasse `[\p{L}\p{N}_.-]*` unter `(?i)` macht `TestSecretKWTailUnfolded`,
`TestGrammarSyncWithTsCommon`, `TestMaskDifferential` und `TestMaskGolden` (Fälle `g/kv_tail_*`) rot.*)

**Goldkorpus neu erzeugen.** (*Seit 2026-10-03 ohne typesafe-dev: in `tools/typesafe` selbst
`python3 tests/gen_mask_golden.py` und `python3 -m unittest tests.test_mask_golden`, dann Schritt (3); Schritte (1)
und (2) unten gelten nicht mehr, typesafe-dev ist seit 2026-10-03 eingefroren.*) (1) In typesafe-dev mit einer Python, deren `unicodedata` die Version von
`nfcUnicodeVersion` hat: `python3 tests/gen_mask_golden.py` (fester Seed; schreibt
`tests/mask-golden.json`; dieselbe Python schreibt dieselben Bytes; die Pre-Push-Formen meidet der
Generator selbst, siehe seinen Kopfkommentar), dann `python3 -m unittest tests.test_mask_golden` und
committen. (2) Hier den Änderungssatz übernehmen, wie Paket G5 es tat (c3e051c):
`git -C <typesafe-dev> diff --binary <alt> <neu> | git apply --directory=tools/typesafe`; so bleiben die
Zeilen von `tools/typesafe/README.md`, die es nur hier gibt, stehen. (3) In `tools/imprint-dev`:
`go test -run 'TestMaskGolden|TestMaskParity|TestGrammarSyncWithTsCommon|TestMaskDifferential'`, vor
dem Push die vier Pre-Push-Formen über die neuen Zeilen. Eine Python mit neuerer `unicodedata` verlangt
beides neu: `nfc_tables.go` (`go generate` mit `gen_nfc_tables.py`) und den Goldkorpus; sonst wird
`TestMaskGolden` rot.

**Bekannte Grenzen**

| Grenze | Folge | Stand |
|---|---|---|
| Unicode 17 in Go, 16 in Python | Go 1.27.1 kennt Unicode 17.0.0, Python 3.14.6 `unicodedata` 16.0.0: Codepunkte, die erst 17 vergibt, sind in Go Buchstaben (W), in Python nicht vergeben (Cn); Wortgrenzen an ihnen fallen verschieden aus. NFC ist in beiden 16.0.0 (*ergänzt am 2026-10-03, Nachtrag A9: auch Ziffern. U+11DE0–U+11DE9 sind in Go Nd, in Python nicht vergeben; ein Token-Lauf direkt davor wird in Go maskiert, in Python nicht, Go maskiert also mehr. Sonst ist Nd gleich: über alle Codepunkte 770 in Go, 760 in Python, der Unterschied sind genau diese 10*) | bekannt, nicht behoben; der Goldkorpus schließt Cn aus |
| Nachtrag A4: Token vor einer Ziffer anderer Schrift | 24 oder mehr Token-Zeichen ohne ASCII-Ziffer direkt vor z. B. „١“: Python maskiert (`RX_OPAQUE` sucht die Ziffer mit Unicode-`\d`), Go nicht; Ziel ist Gos Verhalten (ein Treffer dort teilte ein Wort) | bis der typesafe-dev-Branch `done-check-timeout` gemergt ist, der `RX_OPAQUE` ersetzt; nicht im Goldkorpus (*überholt am 2026-10-03 durch Nachtrag A9: die Prämisse ist widerlegt, auch der lineare Matcher von typesafe-dev master liest die Ziffer mit Unicode-`\d`; Ziel ist Pythons Regel, Go maskiert seitdem ebenso, siehe „Opaque: Pythons Regel“; der Goldkorpus schloss die Form aus, bis er neu erzeugt war, und ist inzwischen, noch am 2026-10-03, mit typesafe-dev b64a949 neu erzeugt (7.269 Fälle, sha256 7b52aae2…), die A9-Fälle sind jetzt enthalten*) |
| Nachtrag A6: türkisches i kostet | Text mit U+0130 oder U+0131 nimmt in Go für Bearer/Basic, Schlüsselwörter und Namen den alten Weg; das Budget rechnet ihn mit 900 + 20 ns je Namensmuster und Byte statt 200 + 1,5 (bei 3 s etwa 1,6 MB statt 7,2 MB); Ergebnis gleich | bekannte Kosten, nicht geändert; ein schneller Weg, der diese Faltung kennt, wäre ein Folgeschritt |
| Nachtrag A7: Namensdatei | Python und Go lesen verschieden: Zeilen mit zwei Leerzeichen hintereinander, U+001C–U+001F als Trenner, eine Datei, die nicht UTF-8 ist (Python verwirft sie ganz, Go liest die Zeile als Latin-1). Dazu ersetzt Python alle Namen in einer Alternation, Go Name für Name: gleich, solange sich Namen nicht überlappen | bekannt; die Namensliste des Goldkorpus meidet alle vier |
| NFD bei Adressen und E-Mail | Nur Namen laufen über eine NFC-Kopie. In NFD ist ein Kombinationszeichen kein Wortzeichen: PLZ + „Mu“ + U+0308 + „nchen“ wird `<address>` + U+0308 + „nchen“, der Ort bleibt halb stehen; bei einer Mail-Adresse mit zerlegtem Umlaut vor dem @ bleibt alles bis zum letzten Kombinationszeichen stehen, mit zerlegtem Umlaut im Domain-Teil wird gar nichts maskiert. In beiden Sprachen gleich (gemessen 2026-10-02) | nicht behoben; Kandidat für einen Folgeschritt (Adressen und E-Mail auf derselben NFC-Kopie wie Namen) (*Zeile überholt am 2026-10-02 durch die NFC-Vereinigung, Entscheid oben; Go aa9d2d4, typesafe-dev 85fce61, 0f7f16a und b340863, hier 5e77bad: behoben, alle drei Fälle werden ganz `<address>` oder `<email>`, in beiden Sprachen; „Nur Namen laufen über eine NFC-Kopie“ gilt nicht mehr; was bleibt, steht in den drei Zeilen darunter*) |
| NFC-Vereinigung: Marke nach dem letzten Buchstaben des Orts | Setzt NFC eine Marke hinter dem letzten Buchstaben eines Ortsnamens mit ihm zu einem Zeichen zusammen, das nicht in den Klassen der Grammatik liegt, endet auf der Kopie dort kein Ort. PLZ + „Bad Du“ + U+0308 + „rkheim“ + U+0301 („m“ + U+0301 wird U+1E3F): im Text endet der Treffer vor U+0308 (PLZ + „Bad Du“), auf der Kopie findet sich nur PLZ + „Bad“, das darin liegt; maskiert wird PLZ + „Bad Du“, U+0308 + „rkheim“ + U+0301 bleibt stehen, Adresse 1. Ohne die Marke am Ende wird alles `<address>`. In beiden Sprachen gleich | gemessen 2026-10-02 vom Python-Schreiber in typesafe-dev; hier am selben Tag nachgemessen mit `ts_common.mask_detail` (5e77bad) und `MaskDetail` (aa9d2d4), ohne Namensdatei: dieselbe Ausgabe und Zählung; nicht behoben |
| Lokaler Alarm ohne NFC-Vereinigung | Der lokale Alarm von `ts-commit-check` (`alarm_view` und `local_alarm` in `ts_common.py`; nur Python, Go hat keinen) sucht weiter mit `RX_EMAIL` im Text selbst: eine Mail-Adresse mit zerlegtem Umlaut im Domain-Teil löst ihn nicht aus, auch auf einer Domain, die nicht reserviert ist; mit zerlegtem Umlaut nur vor dem @ löst sie ihn aus wie bisher. Maskiert wird sie in beiden Fällen. Beide Aufrufe laufen noch mit Pythons Rückverfolgung über `RX_EMAIL` und sind quadratisch auf langen Läufen von Zeichen des lokalen Teils: `RX_EMAIL.search` auf 10.000 „a“ 0,22 s, auf 20.000 0,88 s. Linear ist seit typesafe-dev 85fce61 nur der E-Mail-Schritt der Maskierung | außerhalb der drei Durchgänge, nicht geändert; gemessen 2026-10-02 (Python 3.14.6; das Verhalten mit `local_alarm(alarm_view(…))` auf Beispielen aus Teilen, die Zeiten von `RX_EMAIL.search` allein mit `time.perf_counter`, je ein Lauf; der ganze Aufruf brauchte etwa doppelt so lang, 0,44 s und 1,76 s) |
| NFC-Vereinigung kostet | Text, der nicht NFC ist, baut je Durchgang eine eigene NFC-Kopie, bis zu drei mehr als vorher (Go überspringt die Kopie für E-Mail ohne „@“, für Straße und PLZ ohne Ziffer D). `TestMaskProfile` (`GOMAXPROCS=2`, ns je Byte aufgerundet, der schlechtere von zwei Läufen, vorher = c9ed39a mit denselben Korpora): `nfdanchors` (dichte NFD-Adressen und -Mails) 73 → 116 bei 2 MiB, 68 → 109 bei 16 MiB; `nfdpii` (NFD-Adressen in Fließtext) 66 → 92 und 63 → 88; Text in NFC unverändert (`keyvalues` 76 → 79 und 75 → 75); mit ſ davor `nfdanchors` 218 → 254 und 214 → 250. Das Budget erlaubt `nfdanchors` 208,3 und `nfdpii` 208,6 ns je Byte (6 Namensmuster), mit ſ davor etwa 1.010 | gemessen 2026-10-02, Zahlen aus der Commit-Nachricht von aa9d2d4, hier nicht nachgemessen; keine Konstante geändert |
| IBAN-Prüfziffern in anderer Schrift | Python liest die zwei Prüfziffern mit Unicode-`\d`, Go nur ASCII; Python maskiert solche IBANs, Go nicht. Einzelheiten in der Zeile „IBAN-Prüfziffern in anderer Schrift“ von [`tools/typesafe/README.md`](../tools/typesafe/README.md) | entschieden 2026-10-03 (Entscheid oben): Go folgt Python, umgesetzt in Stufe 2; bis dahin offen; gemessen 2026-10-03; nicht im Goldkorpus |
| Schlüssel = Wert: lange Läufe ohne Leerraum | Gos schneller Weg (`maskSecretKW`) sucht nach jedem Schlüsselwort bis zum Ende des Werts, das erst der nächste ASCII-Leerraum, ein Anführungszeichen, Komma oder Semikolon setzt (`secretKWWindow`): auf „token=<redacted>“ hintereinander quadratisch, `MaskDetail` 16 KB 19,5 ms, 64 KB 186 ms, 256 KB 2,8–3,4 s (3 Läufe, `GOMAXPROCS` nicht gesetzt, 20 Kerne, Last um 5), gegen etwa 51 ms, die das Budget mit 200 ns je Byte rechnet. Mit ı im Text (Regex-Weg) 61,5 ms; Python seit typesafe-dev c906a98 auf derselben Form 58,8 ms. Überzieht das Maskieren die Deadline, antwortet `judge` mit `timeout` und sendet nichts (`TestJudgeMaskingStopsAtTheDeadline`, dort mit künstlich langsamem Maskieren, nicht mit dieser Form) | gemessen 2026-10-03 mit einem Messtest, der nicht eingecheckt ist; offen, in dieser Stufe nicht behoben (*überholt am 2026-10-03: behoben, Entscheid der Person (Auswahlfrage: Gos Schlüssel=Wert-Weg linear wie Python seit typesafe-dev c906a98; dann: in Stufe 1, damit `main` nie quadratisch wird). `secretKWWindow` teilt die beiden Wert-Suchen (bis zum schließenden Anführungszeichen, bis zum Ende eines Werts ohne Anführungszeichen) über alle Kandidaten eines Aufrufs (`secretKWScans`): jedes Byte wird je Suche höchstens einmal gelesen, das Fenster ist byte-gleich mit dem alten. Laut Review der Stufe 1 entstand das quadratische Verhalten erst mit c1dd445 (bisektiert dort; `main` 64 KB 6,5 ms, nach c1dd445 200 ms; mit API-Schlüssel traf es auch `hook-typesafe-check`, `Mask` an der Grenze von 100 KB 10 ms → etwa 0,5 s), hier nicht nachgemessen. Gemessen hier am 2026-10-03, `MaskDetail` auf „token=<redacted>“ hintereinander, je 3 Läufe, zwei Runden abwechselnd, vorher 3f88d58, nachher dieser Commit allein (Quellen aus `git archive`), Last 1,7–2,3: 64 KiB 193–203 ms → 7,3–15,2 ms (die höheren Werte im ersten Lauf je Runde), 256 KiB 2,94–2,97 s → 28,8–29,2 ms (etwa 110 ns je Byte, Budget 200). Geprüft durch `TestSecretKWLinearDifferential` (alter Schritt als Orakel im Test: 22.040 Texte, Ausgabe, Treffer und das Fenster jeder Stelle gleich, aufsteigend und in zufälliger Reihenfolge gefragt) und `TestSecretKWLinearTime` (sieben Formen zu 1 MiB, Schranke 2 s; gemessen 70–300 ms). Nicht behoben, vorher und nachher gleich: ein einziger Wert über 1 MiB (Unicode-Leerzeichen statt ASCII-Leerraum zwischen den Teilen, etwa „token=a“ + U+00A0 wiederholt) kostet den regulären Ausdruck des Schritts 276–323 ms, rund 270–300 ns je Byte, über den 200 des Budgets (je 3 Läufe, gleiche Sitzung)*) |
| „Gäßchen 4“ allein | bleibt in beiden Grammatiken unmaskiert: Gos Endung „gäßchen“ braucht einen Großbuchstaben davor, Pythons heißt „gässchen“; „Am Gäßchen 4“ wird maskiert | wörtlich nach Spezifikation, in beiden Sprachen gleich (gemessen 2026-10-02) |
| Basis `jev-judge` ohne die Vereinigung | Der alte Straßen-Schritt kostete auf Ketten großgeschriebener Wörter vor einer Hausnummer 934–937 ns je Byte, über beiden Preisen des Budgets (200 und 900 ns): dort kann ein Text, der ins Budget passt, die Deadline reißen | gemessen 2026-10-02 (Paket G3, `TestMaskProfile`, Korpus `streetchain`) am Stand 4b4487b, dessen Straßen-Suche die von jev-judge 50babfc ist (dazwischen kam nur das türkische i in die Endungen); mit der Vereinigung 28 ns |
| Schnitt neben einem Wort, das nicht maskiert war | Das zweite Maskieren in `PostState` kann ein gekapptes Feld noch wachsen lassen, wenn der Schnitt einem Wort den Kontext nimmt, an dem es scheiterte: Anfang endet auf „Max“ aus „Maximilian“ (+3 Zeichen über der Kappung), „Str 1“ aus „Str 1ä“ (+4), PLZ + Ort aus zwei Buchstaben (+1); Ende beginnt mit „Bearer a“ hinter einem ASCII-Buchstaben (+9), mit einem Namensteil (+3). Auf gemischten Korpora (`genText`, `pii`, Fließtext; 18.603 Schnitte) kein Fall | gemessen 2026-10-02 (Paket G5, Messtest nur in der Sitzung); nicht behoben, anderer Weg als der behobene Vorsatz (siehe „Maskieren und Kappen“) |

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
`mask_counts`, `latency_ms`, `latency_parts_ms`, `fail_mode`, `failed`, `error_class`, `no_ui`, und nur
wenn gesetzt `unknown_gate`, `unknown_event`, `unknown_host`, `exit_code`, `names_latin1`.
Nie im Log: State, Prompt, Dateiinhalt, Nachricht, Befehl, Schlüssel. `gate`, `event` und `host` stehen
in Log und Verdikt nur, wenn die Registry bzw. `judge` sie kennt; ein unbekannter Name wird leer
geschrieben und `unknown_gate`/`unknown_event`/`unknown_host` gesetzt (der Name selbst könnte alles
sein, auch personenbezogen). `mask_counts` zählt die Platzhalter im gesendeten Request (`<email>`,
`<redacted>` …), nicht was eine Kappung verworfen hat; ohne Request ist es 0. Steht ein Platzhalter
schon wörtlich in der Eingabe, zählt er mit (nicht behoben). Ein `<redacted>` hinter einem Schlüsselwort
oder Bearer/Basic zählt als `secret_kw`, sonst als `opaque`; seit 2026-10-02 mit den Klassen von
`MaskDetail` (S um das Trennzeichen, auch als JSON-Escape wie `\u001c`, Unicode-`\w` im Schlüssel,
türkisches i) und mit dem zweiten `<redacted>` aus „authorization: Bearer x“; vorher zählten „tokenä=x“,
„credentİal=x“, „Bearer“ + U+00A0 + „x“ u. ä. als `opaque` (`TestCountPlaceholdersClasses`). Grenze, nur
im Log: ein Token hinter dem Wert eines Schlüssels und einem Leerzeichen zählt ebenso als `secret_kw`.
Auch Aufruf- und Eingabefehler schreiben
eine Zeile, Exit-1-Fälle eine minimale.

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
| Texte über dem Maskier-Budget urteilen | Budget folgt aus der Deadline (siehe oben); mehr Spielraum gäbe nur eine längere Deadline |
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
