package main

import (
	"strings"
	"testing"
)

func TestFrontmatterDescription(t *testing.T) {
	tests := []struct {
		name     string
		file     string
		want     string
		wantLine int
		wantErr  string
	}{
		{
			name:     "double-quoted with escapes",
			file:     "---\nname: x\ndescription: \"Say \\\"hi\\\" \\\\ now\\tthen\"\n---\nbody\n",
			want:     "Say \"hi\" \\ now\tthen",
			wantLine: 3,
		},
		{
			name:     "double-quoted unicode escape",
			file:     "---\ndescription: \"a\\u00e4b\\x41\"\n---\n",
			want:     "a\xc3\xa4bA",
			wantLine: 2,
		},
		{
			name:     "double-quoted folded over lines",
			file:     "---\ndescription: \"first\n  second\n\n  third\"\n---\n",
			want:     "first second\nthird",
			wantLine: 2,
		},
		{
			name:     "double-quoted escaped line break",
			file:     "---\ndescription: \"joined\\\n  word\"\n---\n",
			want:     "joinedword",
			wantLine: 2,
		},
		{
			name:     "single-quoted",
			file:     "---\ndescription: 'it''s \"quoted\"'\n---\n",
			want:     "it's \"quoted\"",
			wantLine: 2,
		},
		{
			name:     "plain with comment and continuation",
			file:     "---\ndescription: plain words # a comment\n  more words\nname: x\n---\n",
			want:     "plain words more words",
			wantLine: 2,
		},
		{
			name:     "plain starting on the next line",
			file:     "---\ndescription:\n  on the next line\n---\n",
			want:     "on the next line",
			wantLine: 2,
		},
		{
			name:     "literal block",
			file:     "---\ndescription: |\n  line one\n  line two\nname: x\n---\n",
			want:     "line one\nline two\n",
			wantLine: 2,
		},
		{
			name:     "folded block, stripped",
			file:     "---\ndescription: >-\n  line one\n  line two\n\n  para\n---\n",
			want:     "line one line two\npara",
			wantLine: 2,
		},
		{
			name:     "folded block, kept",
			file:     "---\ndescription: >+\n  a\n\n---\n",
			want:     "a\n\n",
			wantLine: 2,
		},
		{
			name:     "CRLF and byte order mark",
			file:     "\xef\xbb\xbf---\r\nname: x\r\ndescription: \"crlf\"\r\n---\r\n",
			want:     "crlf",
			wantLine: 3,
		},
		{
			name:     "nested description key is not the top-level one",
			file:     "---\nmeta:\n  description: \"nested\"\ndescription: \"top\"\n---\n",
			want:     "top",
			wantLine: 4,
		},
		{name: "no frontmatter", file: "# just a heading\n", wantErr: "no frontmatter"},
		{name: "unclosed frontmatter", file: "---\ndescription: \"x\"\n", wantErr: "never closed"},
		{name: "no description", file: "---\nname: x\n---\n", wantErr: "no description"},
		{name: "unclosed quote", file: "---\ndescription: \"open\n---\n", wantErr: "never closed"},
		{name: "unknown escape", file: "---\ndescription: \"bad \\q\"\n---\n", wantErr: "unknown escape"},
		{name: "key without space", file: "---\ndescription:\"x\"\n---\n", wantErr: "no description"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, line, err := frontmatterDescription([]byte(tc.file))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want || line != tc.wantLine {
				t.Fatalf("got %q on line %d, want %q on line %d", got, line, tc.want, tc.wantLine)
			}
		})
	}
}
