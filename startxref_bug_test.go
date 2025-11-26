package pdf

import (
	"bytes"
	"testing"
)

// TestStartxrefBugReproduction reproduces the bug causing "missing startxref" errors
func TestStartxrefBugReproduction(t *testing.T) {
	// Simulate what happens in NewReaderEncrypted
	// A typical PDF end looks like:
	// "trailer\n<< ... >>\nstartxref\n12345\n%%EOF\n"

	testCases := []struct {
		name        string
		pdfEnd      string
		shouldFind  bool
		description string
	}{
		{
			name:        "normal PDF with newlines",
			pdfEnd:      "trailer\n<< /Size 10 >>\nstartxref\n12345\n%%EOF\n\n\n",
			shouldFind:  true,
			description: "Normal PDF with trailing newlines",
		},
		{
			name:        "PDF after TrimRight",
			pdfEnd:      "trailer\n<< /Size 10 >>\nstartxref\n12345\n%%EOF",
			shouldFind:  true,
			description: "After TrimRight removes trailing whitespace",
		},
		{
			name:        "startxref at very end",
			pdfEnd:      "trailer\n<< /Size 10 >>\nstartxref",
			shouldFind:  true, // Fixed! Now works
			description: "startxref at buffer end (no newline after)",
		},
		{
			name:        "startxref with one char after",
			pdfEnd:      "trailer\n<< /Size 10 >>\nstartxref\n",
			shouldFind:  true,
			description: "startxref with exactly one newline after",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Simulate the TrimRight operation from NewReaderEncrypted
			buf := []byte(tc.pdfEnd)
			buf = bytes.TrimRight(buf, "\r\n\t ")

			t.Logf("After TrimRight: len=%d, content=%q", len(buf), string(buf))

			// Now try to find startxref
			i := findLastLine(buf, "startxref")

			found := i >= 0
			if found != tc.shouldFind {
				t.Errorf("Expected shouldFind=%v, got found=%v (i=%d)", tc.shouldFind, found, i)
				t.Logf("Description: %s", tc.description)

				// Debug information
				if idx := bytes.LastIndex(buf, []byte("startxref")); idx >= 0 {
					t.Logf("bytes.LastIndex found at: %d", idx)
					t.Logf("len(buf): %d", len(buf))
					t.Logf("idx + 9: %d", idx+9)
					t.Logf("Failing condition: idx+9 >= len(buf) = %v", idx+9 >= len(buf))

					if idx > 0 {
						t.Logf("Char before: %q", buf[idx-1])
					}
					if idx+9 < len(buf) {
						t.Logf("Char after: %q", buf[idx+9])
					} else {
						t.Logf("No char after - this is the BUG!")
					}
				}
			}
		})
	}
}

// TestRealPDFEndScenario tests the exact scenario from a real PDF
func TestRealPDFEndScenario(t *testing.T) {
	// This simulates reading the last 1024 bytes of a real PDF
	realPDFEnd := `0 10
0000000000 65535 f 
0000000009 00000 n 
0000000074 00000 n 
0000000120 00000 n 
0000000179 00000 n 
0000000322 00000 n 
0000000415 00000 n 
0000000508 00000 n 
0000000640 00000 n 
0000000723 00000 n 
trailer
<< /Size 10 /Root 1 0 R >>
startxref
116
%%EOF`

	// Step 1: Read into buffer
	buf := []byte(realPDFEnd)
	t.Logf("Initial buffer length: %d", len(buf))

	// Step 2: Trim trailing whitespace (as done in NewReaderEncrypted)
	buf = bytes.TrimRight(buf, "\r\n\t ")
	t.Logf("After TrimRight length: %d", len(buf))
	t.Logf("Buffer ends with: %q", string(buf[len(buf)-10:]))

	// Step 3: Find %%EOF
	eofIdx := bytes.LastIndex(buf, []byte("%%EOF"))
	t.Logf("%%EOF found at: %d", eofIdx)

	if eofIdx >= 0 {
		// Step 4: Truncate to %%EOF
		buf = buf[:eofIdx+5]
		t.Logf("After EOF truncation length: %d", len(buf))
	}

	// Step 5: Trim again
	buf = bytes.TrimRight(buf, "\r\n\t ")
	t.Logf("After second TrimRight length: %d", len(buf))
	t.Logf("Final buffer: %q", string(buf))

	// Step 6: Try to find startxref
	i := findLastLine(buf, "startxref")
	t.Logf("findLastLine result: %d", i)

	if i < 0 {
		t.Error("Failed to find startxref - THIS IS THE BUG")

		// Show why it failed
		idx := bytes.LastIndex(buf, []byte("startxref"))
		if idx >= 0 {
			t.Logf("bytes.LastIndex found startxref at: %d", idx)
			t.Logf("Buffer length: %d", len(buf))
			t.Logf("idx + 9: %d", idx+9)
			t.Logf("Bug condition (idx+9 >= len(buf)): %v", idx+9 >= len(buf))
		}
	}
}
