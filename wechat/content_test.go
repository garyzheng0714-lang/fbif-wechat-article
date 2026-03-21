package wechat

import (
	"testing"
)

func TestCleanToPlainText(t *testing.T) {
	tests := []struct {
		name  string
		input string
		check func(string) bool
	}{
		{
			name:  "strips markdown images",
			input: "![alt text](https://example.com/img.jpg) some text",
			check: func(s string) bool { return !contains(s, "![") },
		},
		{
			name:  "strips markdown links but keeps text",
			input: "[click here](https://example.com) after link",
			check: func(s string) bool { return contains(s, "click here") && !contains(s, "[") },
		},
		{
			name:  "strips bold markers",
			input: "**bold text** normal",
			check: func(s string) bool { return contains(s, "bold text") && !contains(s, "**") },
		},
		{
			name:  "strips italic markers",
			input: "*italic* normal",
			check: func(s string) bool { return contains(s, "italic") && !contains(s, "*normal") },
		},
		{
			name:  "strips HTML tags",
			input: "<p>paragraph</p> <b>bold</b>",
			check: func(s string) bool { return !contains(s, "<") && !contains(s, ">") },
		},
		{
			name:  "decodes HTML entities",
			input: "Hello &amp; world &lt;3 &gt;",
			check: func(s string) bool { return contains(s, "& world") && !contains(s, "&amp;") },
		},
		{
			name:  "removes heading markers",
			input: "# Heading 1\n## Heading 2",
			check: func(s string) bool { return !contains(s, "#") },
		},
		{
			name:  "collapses excessive blank lines",
			input: "line1\n\n\n\n\nline2",
			check: func(s string) bool { return countOccurrences(s, "\n\n\n") == 0 },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cleanToPlainText(tt.input)
			if !tt.check(got) {
				t.Errorf("cleanToPlainText(%q) = %q, check failed", tt.input, got)
			}
		})
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func countOccurrences(s, substr string) int {
	count := 0
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			count++
		}
	}
	return count
}
