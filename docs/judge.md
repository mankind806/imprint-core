# `imprint-dev judge`: der Jev-Kern

**Stand:** 2026-10-02 · Stufe 1 (Hinweis) · Spezifikation: `jev-kern.md` und Konzept B §4, §5, §9
(Repository jev-mistral, Stand 2026-10-01).

| Was | Wie | Stand |
|---|---|---|
| Aufruf | ein Ereignis als JSON auf stdin, genau ein Verdikt als JSON auf stdout | gebaut; `judge_test.go` |
| Gates | `done`, `foreign_return`, beide Hinweis (`warn`), `fail_mode: open` | gebaut; **in keinem Hook verdrahtet** |
| Fragen, Schwellen, Fail-Modus | eine Datei, `tools/imprint-dev/judge/gates.json`, ins Binary eingebettet | gebaut; `TestEmbeddedRegistry` |
| Modell | gepinnt `jev-1.13.0`, nie `jev-latest` | gebaut; `TestRegistryPinningAndVersion` |
| Maskierung | jedes String-Blatt von State und Fragen, mit `MaskDetail` | gebaut; `TestJudgeMasksEverythingSent` |
| Log | eine JSONL-Zeile je Aufruf, ohne Texte | gebaut; `TestJudgeLogOneLinePerCall` |
| Durchsetzung | keine: kein Hook ruft `judge` | **nichts setzt hier etwas durch** |

```
 Hook-JSON oder Umschlag ──stdin──▶ imprint-dev judge --gate G
                                     1 Gate-Code: Vorfilter, Code-Flags, State (maskiert, gekappt)
                                       └─ reicht Code? → allow, kein Jev-Aufruf
                                     2 Schlüssel (TYPESAFE_API_KEY / secret-tool), innerhalb der Deadline
                                     3 EIN Batch-POST: jedes String-Blatt maskiert, model = jev-1.13.0 ──▶ api.typesafe.ai
                                     4 Regeln aus gates.json → allow | warn | ask | block,
                                       herabgestuft auf das, was der Host bei diesem Ereignis kann
                                     5 eine Logzeile
                         stdout ◀── Verdikt-JSON (Exit 0, auch wenn der Aufruf scheiterte)
```

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
| `--gate`, `--host` | optional, wenn der Umschlag sie nennt; nennen beide etwas anderes: Exit 2 |
| `--deadline-ms` | Gesamt-Deadline inkl. Schlüsselsuche; ohne: die des Ereignisses aus `gates.json` |
| `--no-ui` | niemand kann gefragt werden: ein kritisches Gate blockt nach einem Fehler, statt zu fragen |
| `--source` | Feld `source` im Log; Tests und Messläufe setzen `test` bzw. `bench` |
| `--endpoint` | wie bei `hook-typesafe-check`: nur `api.typesafe.ai` oder Loopback |
| `--registry` | andere Registry-Datei, nur für Tests und Kalibrierung |
| `--emit` | nur `verdict`; `hook` ist nicht gebaut (Exit 2) |

**stdin** ist maßgeblich und hat eine von zwei Formen:

- Umschlag: `{"host": "claude", "gate": "done", "payload": {…rohes Hook-JSON…}}`
- rohes Hook-JSON des Hosts (so, wie ein einzeiliger Hook-Wrapper es durchreicht); dann ist `--gate` Pflicht.

Das Ereignis kommt aus `payload.hook_event_name`. Fehlt es und bedient das Gate nur ein Ereignis, gilt dieses.

## Ausgabe

```json
{"verdict": "warn", "gate": "done", "event": "Stop", "reasons": ["unbacked_claim"],
 "message": "TypeSafe: Erfolgsbehauptung ohne sichtbaren Beleg – …",
 "scores": {"backed": 0.2, "claim": 0.9}, "code_flags": {"claim_re": true, "local_evidence": false, "stop_hook_active": false},
 "model": "jev-1.13.0", "registry_version": "2026-10-02.1", "core_version": "devel+1d28c21",
 "latency_ms": 335, "latency_parts_ms": {"key": 0, "post": 330},
 "fail_mode": "open", "failed": false, "error_class": ""}
```

| Feld | Bedeutung |
|---|---|
| `verdict` | das einzige Feld, nach dem ein Adapter handelt |
| `reasons`, `message` | Regel-Gründe aus `gates.json` und ihre Hinweistexte; bei `allow` leer |
| `scores`, `code_flags` | Jev-Antworten (Noul 0..1) und die Ergebnisse der Code-Prüfungen |
| `prefilter` | nur wenn Code allein entschied (z. B. `no_claim`, `too_short`): dann kein Jev-Aufruf |
| `model_resolved` | nur wenn die API ein Feld `model` zurückgibt |
| `latency_ms`, `latency_parts_ms` | Gesamtzeit; `key` (Schlüsselsuche) und `post` (Anfrage) |
| `failed`, `error_class` | `no_key`, `timeout`, `network`, `http_status`, `parse`, `missing_answer`, `internal`; dann entscheidet `fail_mode` |

**Exit-Codes:** 0, sobald das Verdikt geschrieben ist, auch bei `failed: true`. 2 nur bei einem
Aufruf-Fehler (Flag, stdin kein JSON-Objekt, unbekanntes Gate, Ereignis, das das Gate nicht bedient,
Widerspruch zwischen Flag und Umschlag); dann gibt es weder Verdikt noch Logzeile.

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

## Gates in Stufe 1

| Gate | Ereignis | Fragen (ein Batch) | Regel | Herkunft |
|---|---|---|---|---|
| `done` | Stop | `claim`, `backed` | `claim>=0.7 && (!local_evidence \|\| backed<=0.5)` → `warn` | 1:1 aus `ts-done-check` (typesafe-dev 8b80f48): Fragen, Schwellen, `CLAIM_RE`, `local_evidence`, letzte 15 Aufrufe, Bash-Befehl ≤ 200 Zeichen, Nachricht: letzte 4000 Zeichen. Einziger Unterschied: `warn` statt `block` |
| `foreign_return` | PostToolUse (`tool_response`), SubagentStop (`last_assistant_message`) | `instruction_to_agent`, `exfil_request` | je ≥ 0.35 → `warn` | Fragen aus Konzept B §4.2; Schwelle 0.35 laut Konzept B aus dem llm_guardrails-Cookbook, hier nicht nachgelesen; Text ≥ 15 Zeichen, gekappt auf 8000 |

Kalibrierung, wie in `gates.json` vermerkt: `done` auf 8 synthetischen Fällen am 2026-09-29 mit dem
Alias `jev-latest` (welche Version er damals war: nicht festgehalten), nicht neu für `jev-1.13.0`.
`foreign_return`: nicht kalibriert.

## Registry

`tools/imprint-dev/judge/gates.json` ist der eine Ort für Fragen, Schwellen, Kappungen, Deadlines und
Fail-Modus. Sie wird per `go:embed` ins Binary gebaut; Binary und Registry laufen also nicht auseinander.
Beim Laden wird geprüft: Modell gepinnt (kein `*-latest`), jedes Gate hat Go-Code, jedes Ereignis kann
`allow` und `warn`, jede Regel parst und nennt nur Fragen und Code-Flags ihres Gates, jede Regel hat
einen Hinweistext. Fragen sind statisch; dynamischer Text steht nur im State. Fragen nehmen nur Noul.

Regeln (`rules[].if`) kennen Score-IDs mit Vergleich (`>=`, `<=`, `>`, `<`, `==`, `!=`), Code-Flags,
`&&`, `||`, `!` und Klammern. Was Code rechnen kann, ist ein Code-Flag, nie eine Jev-Frage.

## Log

Pfad: `$IMPRINT_JUDGE_LOG`, sonst `${XDG_STATE_HOME:-~/.local/state}/imprint/judge.jsonl`. Eine Zeile je
Aufruf mit Verdikt (auch wenn Code allein entschied oder der Aufruf scheiterte). Felder: `ts` (UTC),
`source`, `host`, `gate`, `event`, `session` (SHA-256 der Sitzungs-ID, 16 Hex-Zeichen),
`registry_version`, `core_version`, `model`, `verdict`, `reasons`, `scores`, `code_flags`, `prefilter`,
`mask_counts`, `latency_ms`, `latency_parts_ms`, `fail_mode`, `failed`, `error_class`, `no_ui`.
Nie im Log: State, Prompt, Dateiinhalt, Nachricht, Befehl, Schlüssel.

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

Re-check by 2027-01-02: ob `jev-1.13.0` noch angeboten wird und die Schwellen noch tragen.

## Was das durchsetzt

| Regel | Durchsetzung |
|---|---|
| Fragen und Schwellen gibt es genau einmal | `go:embed` von `gates.json`; geprüft von `TestEmbeddedRegistry` |
| alles an Jev ist maskiert | `PostState` maskiert jedes String-Blatt; geprüft von `TestJudgeMasksEverythingSent` |
| Modell gepinnt | `loadRegistry` lehnt `*-latest` ab; geprüft von `TestRegistryPinningAndVersion` |
| ein Gate setzt eine Regel durch | **nichts**: kein Hook ruft `judge` auf |
