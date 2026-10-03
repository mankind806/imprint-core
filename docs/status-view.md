# Status view: a template for live status pages

| What | How | State |
|---|---|---|
| Scope | one HTML file a script writes and a person keeps open in a browser | template only |
| Colour | grey base, one structural accent, colour only for status | behaviour rule |
| Status | symbol + text + colour, never colour alone | behaviour rule |
| Contrast | text 4.5 : 1, UI and graphics 3 : 1, computed with the WCAG formula | computed once, see below; nothing here enforces it |
| State across refresh | filters, open sections, scroll position and a pause switch kept in `localStorage` | behaviour rule |
| Shipped | no CDN, CSS and vanilla JS inline, written atomically | behaviour rule |

```
+-------------------------------------------------------------+
| [Skip to content]                                           |
| h1 Title          Last updated <time> · refresh every 60 s  |
|                   [Pause refresh]                           |
| [warn] Source <source> is stale (as of <date>)              |
| [note] Data as of <date> | Source <source> | Caveat <...>   |
|                                                             |
| h2 Status per action                                        |
| <action>  [=========.....]  12 of 16 done [✓ done 12][✕ 1]  |
| Action | done | failed | ... | pending | last update        |
| Status filter [all v]                                       |
|                                                             |
| h2 Category A (n groups)  ======= (accent underline)        |
| | > <group> | key: value | ...  [✓ done 4][✕ failed 1]      |  <- details, closed
|   (opened: grey meta lines, CSV button, detail table        |
|    <target> | number right-aligned | ... | [badge][badge])  |
+-------------------------------------------------------------+
```

This is a design template, not a rule: it records why a status page that people called clear
and calm looks the way it does, so the next one looks the same. It was derived from an internal
status page generator; every value below was read from that code or computed from it, and no
data from that page is in here.

## Palette

Contrast ratios computed on 2026-10-03 with the WCAG relative-luminance formula; the source
generator ships a test that checks every pair against its threshold.

| Role | Value | Pair | Contrast |
|---|---|---|---|
| Background | `#ffffff` | text `#1f1f1f` | 16.48 |
| Surface (table head, notes) | `#f3f3f3` | text / muted `#595959` | 14.85 / 6.31 |
| Muted text | `#595959` | on background | 7.00 |
| UI edge (inputs, buttons) | `#767676` | on background | 4.54 |
| Decorative edge (table grid) | `#c8c8c8` | decorative | no requirement |
| Accent (h2 underline, group edge) | `#2f5d7c` | on background | 7.05 |
| Focus ring | `#0b57a4` | background / surface | 7.21 / 6.50 |

## Status scheme

Each status has a tone (text, border, bar segment) on a light tint (badge background). The
tones are close to the Okabe-Ito set, darkened until text on tint reaches 4.5 : 1.

| Key | Symbol | Label | Tone | Tint | Tone on tint | Tone on background |
|---|---|---|---|---|---|---|
| `done` | ✓ | done | `#00704f` | `#e3f2ec` | 5.30 | 6.12 |
| `failed` | ✕ | failed | `#a83800` | `#fbe7dc` | 5.44 | 6.50 |
| `unreachable` | ⊘ | unreachable | `#4f4f4f` | `#e9e9e9` | 6.75 | 8.19 |
| `running` | ↻ | running | `#005a9c` | `#e1edf7` | 6.00 | 7.14 |
| `stopped` | ■ | stopped | `#8f2f6f` | `#f5e4ef` | 6.14 | 7.49 |
| `deferred` | ‖ | deferred | `#7d5200` | `#faf0d7` | 6.01 | 6.82 |
| `pending` | ○ | pending | `#3d3d3d` | `#ffffff` | 10.86 | 10.86 |
| `not-installed` | – | not installed | `#595959` | `#f3f3f3` | 6.31 | 7.00 |

- `pending` is the default when no source reports anything; its badge gets a dashed UI-edge
  border instead of white on white.
- A `running` entry whose log has been silent for 15 minutes is shown as `stopped`.
- Per (target, action) the newest entry wins; at equal time the executing source beats the
  measurement.
- Every badge carries `data-st="<key>"` for the filter and a `title` of
  `<action>: <reason> <HH:MM:SS> <source>`; with no entry the reason reads "no source".

## Sizes, focus, motion

```css
html { font-size: 100%; }
body { font-family: "Segoe UI", Arial, sans-serif; font-size: .9375rem; /* 15 px */
       line-height: 1.5; margin: 0 1rem 2rem; }
h1 { font-size: 1.5rem; }  h2 { font-size: 1.25rem; border-bottom: 2px solid var(--accent); }
.bd { display: inline-block; padding: 0 .375rem; border: 1px solid; border-radius: 4px;
      font-size: .8125rem; white-space: nowrap; }          /* status badge */
.bd .sy { font-weight: 700; margin-right: .25em; }         /* symbol, aria-hidden */
.num { text-align: right; font-variant-numeric: tabular-nums; }
:focus-visible { outline: 3px solid var(--focus); outline-offset: 2px; }
@media (prefers-reduced-motion: reduce) { * { animation: none !important; transition: none !important; } }
```

- Sizes in `rem`, so browser zoom and the user's font size apply.
- A "Skip to content" link is the first element and appears only on focus.
- Print hides the controls, opens every `<details>` and forces badge colours to print.

## Markup

```html
<!DOCTYPE html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<meta name="view-refresh" content="60"><noscript><meta http-equiv="refresh" content="60"></noscript>
<title><title></title><style>/* palette, scheme and rules above */</style></head><body>
<a class="skip" href="#content">Skip to content</a>
<header><h1><title></h1>
  <p id="stand" aria-live="polite">Last updated: <b><time></b> · <span id="mode">refresh every 60 s</span></p>
  <button type="button" id="pause" aria-pressed="false">Pause refresh</button></header>
<main id="content" tabindex="-1">
  <h2>Status per action</h2>
  <div class="bar" role="img" aria-label="<action>: 12 of 16 done, 1 failed, 3 pending">
    <span data-st="done" style="width:75%"></span><span data-st="failed" style="width:6.25%"></span></div>
  <div><b>12 of 16 done</b>
    <span class="bd" data-st="done"><span class="sy" aria-hidden="true">✓</span>done 12</span></div>
  <table><caption>Status per action</caption>
    <tr><th>Action</th><th>done</th><th>failed</th><th>last update</th></tr>
    <tr><td><action></td><td class="num">12</td><td class="num">1</td><td><time></td></tr></table>
  <section data-cat="<category>"><h2><category> (<n> groups)</h2>
    <details class="g"><summary><b><group></b> <span class="muted">| key: value</span> <!-- badges --></summary>
      <!-- meta lines, CSV button, detail table --></details></section>
</main><script>/* state and refresh, below */</script></body></html>
```

- Headings without jumps, `<caption>` on tables, `<th>` in header rows, `lang` set.
- The progress bar is a graphic with an `aria-label` that says the numbers; the same numbers
  stand next to it as visible text, with badges as the legend.
- Every value is HTML-escaped, quotes included.

## State and refresh

- Filters, open `<details>` and scroll position are saved per page in `localStorage` and
  restored after each refresh.
- The refresh runs in JS rather than a meta refresh, so the pause switch
  (`<button aria-pressed>`) can skip it; without JS the `<noscript>` meta refresh applies.
- No reload while an input or select has focus.
- The file is written to a temporary name and then renamed over the old one, so a refresh
  never loads half a file; the data sits next to it as a JSON twin.

## Pattern: work-status page

A second page of the same make, for ongoing work instead of target states. Sections in a fixed
order:

| # | Section | Holds |
|---|---|---|
| 1 | Waiting on you | decisions and approvals only the person can give, recommendation first |
| 2 | In progress now | work packages with state, progress bar, next step |
| 3 | Background runs | running agents and scripts with start, last message, status badge |
| 4 | Threads | parallel lines of work with branch or location and status |
| 5 | Done | finished packages with their evidence |
| 6 | Next steps | what comes next, in order |

The data lives in one state file (JSON) next to the page. Each work package updates its own
entry through a small command-line call, one call per status change, and the page is
regenerated from the file atomically. The page is a view, never the source.
