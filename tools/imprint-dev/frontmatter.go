package main

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// frontmatterDescription returns the decoded value of the top-level
// "description" key in a SKILL.md's YAML frontmatter, and the 1-based line the
// key is on. It understands the scalar forms a description is written in:
// double-quoted, single-quoted, plain (also over several lines) and the block
// forms | and >. It is not a general YAML parser.
func frontmatterDescription(raw []byte) (value string, line int, err error) {
	text := strings.TrimPrefix(string(raw), "\ufeff")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if strings.TrimRight(lines[0], " \t") != "---" {
		return "", 0, errors.New("no frontmatter: the file does not start with a --- line")
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if t := strings.TrimRight(lines[i], " \t"); t == "---" || t == "..." {
			end = i
			break
		}
	}
	if end < 0 {
		return "", 0, errors.New("the frontmatter is never closed by a --- line")
	}
	fm := lines[1:end]
	for i, l := range fm {
		rest, ok := strings.CutPrefix(l, "description:")
		if !ok || (rest != "" && rest[0] != ' ' && rest[0] != '\t') {
			continue
		}
		line = i + 2 // line 1 is the opening ---
		value, err = decodeScalar(strings.TrimLeft(rest, " \t"), fm[i+1:])
		if err != nil {
			return "", line, fmt.Errorf("description cannot be read: %w", err)
		}
		return value, line, nil
	}
	return "", 0, errors.New("the frontmatter has no description")
}

func decodeScalar(first string, rest []string) (string, error) {
	switch {
	case strings.HasPrefix(first, `"`):
		return decodeDoubleQuoted(joinLines(first[1:], rest))
	case strings.HasPrefix(first, `'`):
		return decodeSingleQuoted(joinLines(first[1:], rest))
	case strings.HasPrefix(first, "|"), strings.HasPrefix(first, ">"):
		return decodeBlock(first, rest)
	default:
		return decodePlain(first, rest), nil
	}
}

func joinLines(first string, rest []string) string {
	return strings.Join(append([]string{first}, rest...), "\n")
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

// fold handles an unescaped line break inside a quoted scalar at src[i]. It
// strips the white space before the break (down to protected, which marks the
// end of the last escaped character), skips the indentation of the following
// lines and writes one space, or one newline per empty line in between.
func fold(src string, i int, buf []byte, protected int) (int, []byte) {
	for len(buf) > protected && isBlank(buf[len(buf)-1]) {
		buf = buf[:len(buf)-1]
	}
	i++ // the line break
	empties := 0
	for {
		j := i
		for j < len(src) && isBlank(src[j]) {
			j++
		}
		if j < len(src) && src[j] == '\n' {
			empties++
			i = j + 1
			continue
		}
		i = j
		break
	}
	if empties == 0 {
		return i, append(buf, ' ')
	}
	return i, append(buf, strings.Repeat("\n", empties)...)
}

func decodeDoubleQuoted(src string) (string, error) {
	var buf []byte
	protected := 0
	for i := 0; i < len(src); {
		switch c := src[i]; c {
		case '"':
			return string(buf), nil
		case '\n':
			i, buf = fold(src, i, buf, protected)
		case '\\':
			if i+1 >= len(src) {
				return "", errors.New("the double-quoted value ends in a backslash")
			}
			if src[i+1] == '\n' { // escaped line break: join with nothing in between
				i += 2
				for i < len(src) && isBlank(src[i]) {
					i++
				}
				continue
			}
			s, n, err := yamlEscape(src[i+1:])
			if err != nil {
				return "", err
			}
			buf = append(buf, s...)
			protected = len(buf)
			i += 1 + n
		default:
			buf = append(buf, c)
			i++
		}
	}
	return "", errors.New("the double-quoted value is never closed")
}

// yamlEscape decodes the escape that starts at s[0], the character after the
// backslash, and returns how many bytes of s it used.
func yamlEscape(s string) (string, int, error) {
	simple := map[byte]string{
		'0': "\x00", 'a': "\a", 'b': "\b", 't': "\t", '\t': "\t", 'n': "\n",
		'v': "\v", 'f': "\f", 'r': "\r", 'e': "\x1b", ' ': " ", '"': `"`,
		'/': "/", '\\': `\`, 'N': "\u0085", '_': "\u00a0", 'L': "\u2028", 'P': "\u2029",
	}
	if v, ok := simple[s[0]]; ok {
		return v, 1, nil
	}
	width := map[byte]int{'x': 2, 'u': 4, 'U': 8}[s[0]]
	if width == 0 {
		return "", 0, fmt.Errorf("unknown escape \\%c", s[0])
	}
	if len(s) < 1+width {
		return "", 0, fmt.Errorf("short escape \\%s", s)
	}
	v, err := strconv.ParseUint(s[1:1+width], 16, 32)
	// v is checked against the constant utf8.MaxRune before it is narrowed to
	// rune (int32). ParseUint's 32-bit result can exceed math.MaxInt32, and
	// narrowing such a value straight to rune would wrap it into a negative
	// number; the previous single-line check relied on utf8.ValidRune to
	// reject that wrapped value (which it always did, since ValidRune never
	// accepts a negative rune), but a bound check against a constant makes
	// the narrowing provably in range rather than merely caught downstream.
	if err != nil || v > utf8.MaxRune {
		return "", 0, fmt.Errorf("bad escape \\%s", s[:1+width])
	}
	r := rune(v)
	if !utf8.ValidRune(r) {
		return "", 0, fmt.Errorf("bad escape \\%s", s[:1+width])
	}
	return string(r), 1 + width, nil
}

func decodeSingleQuoted(src string) (string, error) {
	var buf []byte
	for i := 0; i < len(src); {
		switch c := src[i]; c {
		case '\'':
			if i+1 < len(src) && src[i+1] == '\'' {
				buf = append(buf, '\'')
				i += 2
				continue
			}
			return string(buf), nil
		case '\n':
			i, buf = fold(src, i, buf, 0)
		default:
			buf = append(buf, c)
			i++
		}
	}
	return "", errors.New("the single-quoted value is never closed")
}

// stripComment trims a plain line and drops a trailing # comment.
func stripComment(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "#") {
		return ""
	}
	for _, sep := range []string{" #", "\t#"} {
		if i := strings.Index(s, sep); i >= 0 {
			s = s[:i]
		}
	}
	return strings.TrimSpace(s)
}

// decodePlain reads an unquoted value. Indented lines that follow continue it
// and are folded into it with a space; an empty line becomes a newline.
func decodePlain(first string, rest []string) string {
	parts := []string{stripComment(first)}
	for _, l := range rest {
		if strings.TrimSpace(l) == "" {
			parts = append(parts, "")
			continue
		}
		if !isBlank(l[0]) || strings.HasPrefix(strings.TrimSpace(l), "#") {
			break
		}
		parts = append(parts, stripComment(l))
	}
	var b strings.Builder
	started, empties := false, 0
	for _, p := range parts {
		if p == "" {
			if started {
				empties++
			}
			continue
		}
		if started {
			if empties > 0 {
				b.WriteString(strings.Repeat("\n", empties))
			} else {
				b.WriteByte(' ')
			}
		}
		b.WriteString(p)
		started, empties = true, 0
	}
	return b.String()
}

// decodeBlock reads a | (literal) or > (folded) value with its optional
// chomping (- or +) and indentation indicators.
func decodeBlock(header string, rest []string) (string, error) {
	h := stripComment(header)
	style := h[0]
	var chomp byte
	indent := 0
	for i := 1; i < len(h); i++ {
		switch c := h[i]; {
		case (c == '-' || c == '+') && chomp == 0:
			chomp = c
		case c >= '1' && c <= '9' && indent == 0:
			indent = int(c - '0')
		default:
			return "", fmt.Errorf("unsupported block header %q", h)
		}
	}
	var body []string
	for _, l := range rest {
		if strings.TrimSpace(l) == "" {
			body = append(body, "")
			continue
		}
		ind := len(l) - len(strings.TrimLeft(l, " "))
		if ind == 0 {
			break
		}
		if indent == 0 {
			indent = ind
		}
		if ind < indent {
			break
		}
		body = append(body, l[indent:])
	}
	end := len(body)
	for end > 0 && body[end-1] == "" {
		end--
	}
	content, trailing := body[:end], len(body)-end
	var text string
	if style == '|' {
		text = strings.Join(content, "\n")
	} else {
		text = foldBlock(content)
	}
	switch {
	case chomp == '-' || len(content) == 0 && chomp != '+':
		return text, nil
	case chomp == '+':
		if len(content) == 0 {
			return strings.Repeat("\n", trailing), nil
		}
		return text + "\n" + strings.Repeat("\n", trailing), nil
	default:
		return text + "\n", nil
	}
}

// foldBlock joins the lines of a > block: neighbouring plain lines with a
// space, around empty lines with one newline per empty line, and more-indented
// lines keep their line breaks.
func foldBlock(lines []string) string {
	var b strings.Builder
	started, prevMore, empties := false, false, 0
	for _, l := range lines {
		if l == "" {
			empties++
			continue
		}
		more := isBlank(l[0])
		switch {
		case !started:
			b.WriteString(strings.Repeat("\n", empties))
		case empties > 0 && (more || prevMore):
			b.WriteString(strings.Repeat("\n", empties+1))
		case empties > 0:
			b.WriteString(strings.Repeat("\n", empties))
		case more || prevMore:
			b.WriteByte('\n')
		default:
			b.WriteByte(' ')
		}
		b.WriteString(l)
		started, prevMore, empties = true, more, 0
	}
	return b.String()
}
