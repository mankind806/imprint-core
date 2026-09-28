# Antigravity (agy) als dritter Host für Imprint

**Untersuchungs- und Integrationsbericht für Pakete `imprint-AG-002` und `imprint-core-AG-003`**  
**Datum:** 2026-09-28  
**Host & Version:** Antigravity CLI `1.2.12` (Linux x86_64, Executable `/home/linuxbrew/.linuxbrew/Caskroom/antigravity-cli-linux/1.2.12,5784551402897408/antigravity`)  
**Modell:** Gemini 3.8 Flash (High)  
**Referenz-Dokumentation:** `~/.gemini/antigravity-cli/builtin/skills/agy-customizations/docs/` (`plugins.md`, `rules.md`, `skills.md`, `hooks.md`, `mcp_servers.md`, `json_configs.md`)

---

## 1. Capability-Matrix

| Komponente | Claude Code (Referenz) | Codex (Host 2) | Antigravity `agy` (Host 3) | Technische Umsetzung in agy |
| :--- | :--- | :--- | :--- | :--- |
| **Plugin-Format** | `plugin.json` in `.claude-plugin/` | Konfiguration in Repo / CLI | `plugin.json` | Manifest unter `.claude-plugin/plugin.json` wird bei `agy plugin import claude` oder `agy plugin install` automatisch erkannt. Ein direktes `agy plugin validate .` prüft auf `./plugin.json`; das importierte Plugin unter `~/.gemini/config/plugins/imprint` validiert fehlerfrei mit 4 Skills, 1 Agent, 1 Hook. |
| **Kernkarte / Rules** | `SessionStart`-Hook via `session-start.json` | `instructions`-Kanal / Repo | `rules/AGENTS.md` | **Declarative Rules:** Automatisch beim Laden des Plugins gemerged. Fällt in das dedizierte 20.000-Token-Rules-Budget (`defaultRulesBudget`). Kanonische Quelle bleibt `hooks/kernkarte.md`; `rules/AGENTS.md` ist eine erzeugte bytegleiche Kopie (Entscheid D1, CL-063). |
| **Skills** | `skills/<name>/SKILL.md` | `skills/<name>/SKILL.md` | `skills/<name>/SKILL.md` | **100% identisch:** YAML-Frontmatter (`name`, `description`). Progressive Disclosure: Metadaten im Systemprompt, Volltext on-demand über `view_file`. Alle 4 Skills werden nativ bereitgestellt. |
| **Hooks** | `SessionStart`, `SubagentStart`, `SubagentStop`, `PreToolUse`, `PostToolUse`, `Stop` | CLI-Wrapper / Events | `PreToolUse`, `PostToolUse`, `PreInvocation`, `PostInvocation`, `Stop` | Shell-Hooks in `hooks.json` für SessionStart werden von Antigravity ignoriert (wird stattdessen deklarativ über `rules/AGENTS.md` gelöst). Subagent-Logging und Update-Watch werden in Paket AG-004 über tool-spezifische Hooks adaptiert. |
| **Subagents** | `subagent` CLI / API | Subagenten / Multi-Turn | `invoke_subagent`, `manage_subagents` | Nativ über Agent-Tools (`invoke_subagent`, `define_subagent`), asynchrones Polling entfällt dank reaktivem Wakeup. Transkripte unter `<appDataDir>/brain/<id>/.system_generated/logs/transcript.jsonl`. |
| **MCP-Server** | `mcp.json` / `.claude.json` | `codex mcp` / Config | `mcp_config.json` | JSON mit `mcpServers`-Objekt (Stdio & SSE). Automatische Tool-Discovery und Registrierung. |

---

## 2. Gemessene Befunde & Nachweise

1. **Plugin-Validierung (`agy plugin validate`)**:
   - `agy plugin validate ~/.gemini/config/plugins/imprint` liefert:
     `[ok] skills: 4 processed, agents: 1 processed, hooks: 1 processed`.
   - `agy plugin validate .` im Arbeitsbaum scheitert, da Antigravity ein `plugin.json` im Wurzelverzeichnis statt `.claude-plugin/plugin.json` erwartet. Dies bestätigt Entscheid D2: Kein zweites Manifest einchecken; der Standardinstallationsweg über Import/Install erzeugt die Verknüpfung automatisch.

2. **Kernkarten-Ankunft (Unterschied zu Claude Code)**:
   - In Claude Code injiziert der `SessionStart`-Hook die Kernkarte als temporären Reminder-Text (`session-start.json`).
   - In Antigravity existiert kein `SessionStart`-Hook-Event. Die native Methode ist **`rules/AGENTS.md`**. Antigravity liest `rules/` des aktiven Plugins ein und stellt die Kernkarte im Systemprompt bereit.
   - Gemessen am 2026-09-28: `tools/arrival-test-agy.sh` belegt die zeilengenaue Übereinstimmung der Modellantwort mit `hooks/kernkarte.md` und die Erkennung aller 4 Skills im Modellreport der Wurzel-Sitzung. Subagenten-Vererbung, Laufzeitgrenzen und Budget-Demotion (das 20.000-Token-Rules-Budget demotiert überzählige Regeln zu Dateiverweisen) bleiben ungeprüft.

3. **Integritätsprüfung (Check `k` in `imprint-dev`)**:
   - Prüfung `k` (`rules-agents-in-sync`) prüft Existenz und Bytegleichheit von `rules/AGENTS.md` mit `hooks/kernkarte.md`.
   - `imprint-dev gen` generiert `rules/AGENTS.md` synchron mit den JSON-Payloads.

---

## 3. Umgesetzte Architektur (Paket `imprint-core-AG-003`)

1. **`rules/AGENTS.md`**:
   - Bytegleiche Kopie von `hooks/kernkarte.md`.
2. **`tools/imprint-dev`**:
   - Check `k` (`rules-agents-in-sync`) in `checks.go`.
   - `gen.go` erweitert um `rules/AGENTS.md`.
   - Tests `checks_test.go`, `gen_test.go`, `cli_test.go`, `helpers_test.go`.
3. **Arrival-Tests**:
   - `tools/arrival-test-agy.sh`: Live-Probe gegen Antigravity CLI mit Modell-Report-Verifikation.
   - `tools/test-arrival-agy.sh`: Offline-Fixture- und Stub-Test (in CI eingebunden via `.github/workflows/check.yml`).
4. **Folgepaket AG-004**:
   - Native Hooks für Antigravity (`PostToolUse` für `invoke_subagent`, `PreInvocation`/`Stop` für Update-Watch) mit R-HOST v3.
