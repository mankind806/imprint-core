package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The core card has exactly one canonical place. Both hook payloads are
// generated from it and are never edited by hand; check c holds them to that.
const (
	cardPath          = "hooks/kernkarte.md"
	sessionStartPath  = "hooks/session-start.json"
	subagentStartPath = "hooks/subagent-start.json"
)

type hookTarget struct {
	Event string
	Path  string
}

var hookTargets = []hookTarget{
	{Event: "SessionStart", Path: sessionStartPath},
	{Event: "SubagentStart", Path: subagentStartPath},
}

// A struct, not a map: encoding/json sorts map keys, and the payload's key
// order is part of what gen promises.
type hookPayload struct {
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

type hookSpecificOutput struct {
	HookEventName     string `json:"hookEventName"`
	AdditionalContext string `json:"additionalContext"`
}

// cardText turns the card file into the context text. It drops a byte order
// mark, turns CRLF into LF and trims trailing white space, so that a checkout
// with other line endings still generates the same bytes.
func cardText(raw []byte) string {
	s := strings.TrimPrefix(string(raw), "\ufeff")
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.TrimRight(s, " \t\r\n")
}

// renderHook returns one compact JSON payload plus a trailing newline, with
// HTML escaping off.
func renderHook(event, card string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	err := enc.Encode(hookPayload{HookSpecificOutput: hookSpecificOutput{
		HookEventName:     event,
		AdditionalContext: card,
	}})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type generatedFile struct {
	Path string // slash-separated, relative to the root
	Data []byte
}

// generate returns what gen would write. found is false when the card is
// missing; err is reserved for the card being unreadable.
func generate(root string) (files []generatedFile, found bool, err error) {
	raw, found, err := readRel(root, cardPath)
	if err != nil || !found {
		return nil, found, err
	}
	card := cardText(raw)
	for _, t := range hookTargets {
		data, err := renderHook(t.Event, card)
		if err != nil {
			return nil, true, err
		}
		files = append(files, generatedFile{Path: t.Path, Data: data})
	}
	return files, true, nil
}

// writeGenerated writes the hook payloads and returns their paths.
func writeGenerated(root string) ([]string, error) {
	files, found, err := generate(root)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("%s not found under %s", cardPath, root)
	}
	var written []string
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(f.Path)), f.Data, 0o644); err != nil {
			return written, err
		}
		written = append(written, f.Path)
	}
	return written, nil
}
