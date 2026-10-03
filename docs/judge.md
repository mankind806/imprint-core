# `imprint-dev judge`: der Jev-Kern

**Stand:** 2026-10-02 · Stufe 1 (Hinweis) · Spezifikation: `jev-kern.md` und Konzept B §4, §5, §9
(Repository jev-mistral, Stand 2026-10-01).

| Was | Wie | Stand |
|---|---|---|
| Aufruf | ein Ereignis als JSON auf stdin, genau ein Verdikt als JSON auf stdout; nie Exit 2 | gebaut; `judge_test.go`, `TestJudgeNeverExits2` |
| Gates | `done`, `foreign_return`, beide Hinweis (`warn`), `fail_mode: open` | gebaut; **in keinem Hook verdrahtet** |
| Fragen, Schwellen, Fail-Modus | eine Datei, `tools/imprint-dev/judge/gates.json`, ins Binary eingebettet | gebaut; `TestEmbeddedRegistry` |
| Modell | gepinnt `jev-1.13.0`, nie `jev-latest` | gebaut; `TestRegistryPinningAndVersion`; live 2026-10-02: API nennt `model: jev-1.13.0` |
| Maskierung | jedes Textfeld ganz maskieren, dann kappen (nie mitten in einem Platzhalter); höchstens so viel Text je Aufruf, wie im ungünstigsten Fall in der halben Deadline maskiert ist (3 s: 7,2 MB, 6 s: 14,4 MB), sonst `too_large` und nichts gesendet; `PostState` maskiert jedes String-Blatt noch einmal | gebaut; `TestJudgeMasksEverythingSent`, `TestPostStateMasksWhatTheBuilderLeft`, `TestCutMaskedProperty` (50 × 200 Läufe), `TestMaskCappedIsCutOfWholeMask`, `TestJudgeMaskBudget` |
| Maskier-Geschwindigkeit | dieselben Treffer wie vorher, aus Kandidaten statt NFA über jedes Byte; linear auch auf abgelehnten Treffern | gebaut; `TestMaskDifferential` (alt gegen neu, Byte für Byte), `TestMaskLinearOnRejectedRuns` |
| Deadline | zählt ab Start von `judge`: stdin, Transkript, Maskierung, Schlüssel, Anfrage; Maskieren hört am Ende der Deadline auf zu warten | gebaut; `TestJudgeDeadlineCoversBuild` (dichteste Adressen in Budgetgröße, 15 MiB, 10.000 Aufrufe), `TestJudgeMaskingStopsAtTheDeadline` |
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
| `done` | Stop | `claim`, `backed` | `claim>=0.7 && (!local_evidence \|\| backed<=0.5)` → `warn` | 1:1 aus `ts-done-check` (typesafe-dev 8b80f48): Fragen, Schwellen, `CLAIM_RE`, `local_evidence`, letzte 15 Aufrufe, Bash-Befehl ≤ 200 Zeichen, Nachricht: letzte 4000 Zeichen, jeweils erst ganz maskiert, dann gekappt wie in Python. Unterschiede: `warn` statt `block`; ein Schnitt mitten in einem Platzhalter rückt an dessen Rand |
| `foreign_return` | PostToolUse (`tool_response`), SubagentStop (`last_assistant_message`) | `instruction_to_agent`, `exfil_request` | je ≥ 0.35 → `warn` | Fragen aus Konzept B §4.2; Schwelle 0.35 laut Konzept B aus dem llm_guardrails-Cookbook, hier nicht nachgelesen; Text ≥ 15 Zeichen; über 8000 Zeichen gehen Anfang und Ende (je knapp 4000, getrennt durch `[…]`) |

**Grenze von `foreign_return` in Stufe 1:** Die String-Blätter eines Objekts werden nach Schlüssel
sortiert verbunden. Was nur in der Mitte eines Textes über 8000 Zeichen steht, sieht Jev nicht
(`TestForeignReturnSeesTheTail` prüft das Ende hinter 10 kB Füllung). Bewusst nicht umgebaut.

**Maskieren und Kappen.** Jedes Textfeld wird ganz maskiert (`MaskDetail`) und erst dann auf seine
Kappung geschnitten: Anfang, Ende oder beides. Was hinausgeht, ist damit immer ein Anfang, ein Ende
oder Anfang + `[…]` + Ende des ganz maskierten Textes; ein Geheimnis wird nie zerschnitten, bevor es
maskiert ist, gleich wie lang es ist. Fiele ein Schnitt mitten in einen Platzhalter (`<redacted>`,
`<email>` …), rückt er an dessen Rand; andere `<` bleiben unberührt. Geprüft:
`TestCutMaskedProperty` (50 Seeds × 200 Texte aus JWT-, base64-, UUID-, Lockfile-, Mehrbyte- und
Prosa-Stücken mit Geheimnissen dazwischen; Orakel: Präfix/Suffix des ganz maskierten Textes, gültiges
UTF-8, kein geteilter Platzhalter, eigene Suche nach Platzhaltern, jede Seite höchstens 9 Zeichen unter
ihrem Anteil an der Kappung; fängt die Mutationen „Rückblick 8“, „leer“ und „Byte-Schnitt“),
`TestMaskCappedIsCutOfWholeMask`, die Regressionen `TestForeignReturnKeepsInjectionBesideBlob`,
`TestDoneKeepsMessageBesideBlob`, `TestMaskCappedLongMatch`. `done` maskiert nur die Befehle der letzten
15 Aufrufe; Werkzeugnamen sind Blätter wie andere: maskiert, auf 100 Zeichen gekappt, im Budget
(`TestDoneCapsToolNames`); `local_evidence` entscheidet über die Namen, wie das Transkript sie hat
(`TestDoneEvidenceUsesRawToolNames`).

**Maskier-Geschwindigkeit.** Gos `regexp` hat keinen DFA: eine Suche ohne Anker läuft mit dem NFA über
jedes Byte. Die Muster sind deshalb unverändert, gesucht wird aber anders (`tools/imprint-dev/mask_fast.go`):
ein Treffer kann nur an bestimmten Stellen anfangen (Anfangsbuchstaben eines Schlüsselworts, ein `@`,
zwei Großbuchstaben und zwei Ziffern, Straßen-Endungen vor `\s\d` …); dort wird das Muster mit Anker
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
Angström-Zeichen …): dann wird Name für Name gesucht.

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
sind dichte Straßenadressen: 2,1 s für 16 MB, rund 127 ns je Byte. Jedes Muster der Namensdatei kostet
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
Bekannte Unterschiede zu Python, nicht im Korpus: Python setzt das türkische İ/ı mit i gleich, Go nicht;
Python ersetzt alle Namen in einer Alternation, Go Name für Name, längster zuerst (gleich, solange sich
Namen nicht überlappen); Python verwirft bei einem Dekodierfehler die ganze Namensdatei, Go liest die Zeile
als Latin-1 (siehe Namensdatei).

Gleichheit: `TestMaskDifferential` lässt die alte Fassung (`mask_reference_test.go`, nur in Tests) und
die neue auf dieselben Texte los, mit 5 Namensdateien (keine, 2 Namen, 9 Namen mit Überlappungen und
Platzhalter-Namen wie `name`, 50 Namen, griechische und kyrillische Namen mit Angström); Ausgabe und
Zählung müssen Byte für Byte gleich sein. Der Namens-Schritt der alten Fassung ist seit der Wortgrenzen-
Korrektur eine eigene, schlichte Fassung der neuen Regel (`refNamesStep`); alle anderen Schritte sind
die alten. Im normalen Lauf 3.247 Texte, 3,0 MB, 16.235 Vergleiche;
einmal mit `IMPRINT_MASK_DIFF_SEEDS=400`: 41.122 Texte, 39 MB, 205.610 Vergleiche, alle gleich. Die Python-Parität (`mask-test.py`, 13/13) läuft
unverändert.

**Maskier-Budget.** Ein Aufruf darf für das Maskieren im ungünstigsten Fall höchstens die Hälfte der
verbleibenden Deadline brauchen. Jedes Feld kostet seine Länge mal den ungünstigsten Preis je Byte auf
dem Weg, den es nimmt (`maskNsPerByte`):

| Feld | Preis je Byte | gemessen höchstens |
|---|---|---|
| gültiges UTF-8 ohne Sonderzeichen (schneller Weg) | 200 ns + 1,5 ns je Namensmuster | 127 ns (bei Last 156, einmal 232) + 0,9 ns |
| mit K, ſ, ẞ, Ohm-, Angström-Zeichen … | 900 ns + 20 ns je Namensmuster | 705 ns + 15,7 ns |
| ungültiges UTF-8 | 900 ns + 20 ns je Namensmuster | 705 ns + 15,7 ns |

Für reinen Text heißt das mit 2 Namen: 3 s (PostToolUse, SubagentStop) → 7,2 MB, ungünstigster Fall
gemessen 0,90–0,95 s (31 %); 6 s (Stop) → 14,4 MB, 1,79 s (30 %) (`maskBudgetBytes`). Mit 127 ns statt
200 ns wäre das Budget für 3 s etwa 11 MB; 200 ns lassen Spielraum für Last. Mit einem ſ davor gemessen:
1,3 MB in 0,46–0,92 s geurteilt, 6,5 MB sofort `too_large` (`TestJudgeBudgetCoversFallbackRunes`); 50
griechische Namen auf griechischem Text: 5,9 MB in 0,3 s, mit Ohm-Zeichen 3,6 MB sofort `too_large`
(`TestJudgeBudgetCoversNamesWithoutASCII`); nie `timeout`. Mehr endet mit `error_class: too_large`, ohne
Anfrage, und der Fail-Modus entscheidet; bei einem Hinweis-Gate heißt das `allow`. **Grenze, nicht
behoben:** Wer einen fremden Text über das Budget aufbläht, entgeht `foreign_return`. Rohen Text vor dem
Maskieren zu kürzen, kommt nicht in Frage (ein Schnitt kann Passphrasen, IBANs und Adressen teilen).

Jedes Maskieren (auch kurze Felder und das zweite Maskieren in `PostState`) wartet höchstens bis zum Ende
der Deadline; läuft es länger (langsamerer Rechner), antwortet `judge` mit `timeout`, und die
Maskier-Goroutine läuft im Hintergrund aus, bis der Prozess endet (ein Prozess je Aufruf;
`TestJudgeMaskingStopsAtTheDeadline`, `TestMaskingStopsAtTheDeadlineOnSmallFields`).

**Namensdatei.** Eine Zeile, die kein gültiges UTF-8 ist (eine Datei in Latin-1 gespeichert), wird als
Latin-1 gelesen, was immer gelingt; ihre Namen maskieren also weiter. Vorher ließ ein solcher Name jedes
Maskieren abbrechen. Die Logzeile zählt diese Zeilen in `names_latin1`, die Namen selbst stehen nie im
Log. Übersprungen werden nur leere Zeilen und Zeilen nur aus Steuerzeichen (`TestNamesFileLatin1`).
Groß-/Kleinschreibung: zwei Zeichen gelten als gleich, wenn sie in derselben Klasse von
`unicode.SimpleFold` liegen; die kanonische Kopie nimmt das Minimum der ganzen Klasse, für jedes Zeichen
geprüft (`TestFoldCanonExhaustive`, mit „Laΐs“/„LaΐS“ und „Steﬅn“/„Steﬆn“).

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
`mask_counts`, `latency_ms`, `latency_parts_ms`, `fail_mode`, `failed`, `error_class`, `no_ui`, und nur
wenn gesetzt `unknown_gate`, `unknown_event`, `unknown_host`, `exit_code`, `names_latin1`.
Nie im Log: State, Prompt, Dateiinhalt, Nachricht, Befehl, Schlüssel. `gate`, `event` und `host` stehen
in Log und Verdikt nur, wenn die Registry bzw. `judge` sie kennt; ein unbekannter Name wird leer
geschrieben und `unknown_gate`/`unknown_event`/`unknown_host` gesetzt (der Name selbst könnte alles
sein, auch personenbezogen). `mask_counts` zählt die Platzhalter im gesendeten Request (`<email>`,
`<redacted>` …), nicht was eine Kappung verworfen hat; ohne Request ist es 0. Steht ein Platzhalter
schon wörtlich in der Eingabe, zählt er mit (nicht behoben). Auch Aufruf- und Eingabefehler schreiben
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
