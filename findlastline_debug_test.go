package pdf

import (
	"bytes"
	"testing"
)

// Test findLastLine function with various edge cases
func TestFindLastLineEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		buf      string
		search   string
		expected int
	}{
		{
			name:     "normal case",
			buf:      "some text\nstartxref\n123\n",
			search:   "startxref",
			expected: 10,
		},
		{
			name:     "startxref at end with one newline",
			buf:      "some text\nstartxref\n",
			search:   "startxref",
			expected: 10,
		},
		{
			name:     "startxref near end",
			buf:      "trailer\n<< /Size 10 >>\nstartxref\n100\n%%EOF",
			search:   "startxref",
			expected: 23, // Corrected: "trailer\n<< /Size 10 >>\n" is 23 chars
		},
		{
			name:     "multiple occurrences with proper newlines",
			buf:      "old\nstartxref\n123\nnew\nstartxref\n456\n",
			search:   "startxref",
			expected: 22, // should find the last one at position 22
		},
		{
			name:     "multiple occurrences without newlines",
			buf:      "old startxref\n123\nnew startxref\n456\n",
			search:   "startxref",
			expected: -1, // both have space before, not newline
		},
		{
			name:     "startxref with spaces before newline",
			buf:      "trailer\nstartxref  \n100\n",
			search:   "startxref",
			expected: -1, // should fail - space after keyword
		},
		{
			name:     "startxref exactly at buffer end minus one",
			buf:      "x\nstartxref\n",
			search:   "startxref",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := findLastLine([]byte(tt.buf), tt.search)
			if result != tt.expected {
				t.Errorf("findLastLine() = %d, want %d\nBuffer: %q\nSearch: %q",
					result, tt.expected, tt.buf, tt.search)

				// Debug: print buffer analysis
				buf := []byte(tt.buf)
				bs := []byte(tt.search)
				i := bytes.LastIndex(buf, bs)
				t.Logf("LastIndex found at: %d", i)
				if i >= 0 {
					t.Logf("Buffer length: %d", len(buf))
					t.Logf("Search length: %d", len(bs))
					t.Logf("i + len(bs): %d", i+len(bs))
					if i > 0 {
						t.Logf("Character before: %q (0x%02x)", buf[i-1], buf[i-1])
					}
					if i+len(bs) < len(buf) {
						t.Logf("Character after: %q (0x%02x)", buf[i+len(bs)], buf[i+len(bs)])
					}
					t.Logf("Condition i+len(bs) >= len(buf): %v", i+len(bs) >= len(buf))
				}
			}
		})
	}
}

// Test with actual PDF-like content
func TestFindLastLineRealisticPDF(t *testing.T) {
	pdfEnd := `trailer
<< /Size 10 /Root 1 0 R >>
startxref
12345
%%EOF`

	result := findLastLine([]byte(pdfEnd), "startxref")
	if result < 0 {
		t.Errorf("findLastLine failed on realistic PDF content")
		t.Logf("PDF content:\n%s", pdfEnd)

		// Debug
		buf := []byte(pdfEnd)
		i := bytes.LastIndex(buf, []byte("startxref"))
		t.Logf("LastIndex: %d, len(buf): %d, i+9: %d", i, len(buf), i+9)
		if i >= 0 && i+9 < len(buf) {
			t.Logf("Char after startxref: %q", buf[i+9])
		}
	}
}
