// Copyright 2024 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package pdf

import (
	"bytes"
	"strings"
	"testing"
)

func TestCMapBasic(t *testing.T) {
	cm := NewCMap("TestCMap", CMapTypeToUnicode)
	if cm == nil {
		t.Fatal("NewCMap returned nil")
	}
	if cm.Name != "TestCMap" {
		t.Errorf("expected name 'TestCMap', got '%s'", cm.Name)
	}
	if cm.Type != CMapTypeToUnicode {
		t.Errorf("expected type CMapTypeToUnicode, got %d", cm.Type)
	}
}

func TestCMapCIDSystemInfo(t *testing.T) {
	cm := NewCMap("Adobe-GB1-5", CMapTypeCID)
	cm.SetCIDSystemInfo("Adobe", "GB1", 5)

	if cm.CIDSystemInfo.Registry != "Adobe" {
		t.Errorf("expected registry 'Adobe', got '%s'", cm.CIDSystemInfo.Registry)
	}
	if cm.CIDSystemInfo.Ordering != "GB1" {
		t.Errorf("expected ordering 'GB1', got '%s'", cm.CIDSystemInfo.Ordering)
	}
	if cm.CIDSystemInfo.Supplement != 5 {
		t.Errorf("expected supplement 5, got %d", cm.CIDSystemInfo.Supplement)
	}
}

func TestCMapCodeSpaceRange(t *testing.T) {
	cm := NewCMap("Test", CMapTypeToUnicode)
	cm.AddCodeSpaceRange([]byte{0x00}, []byte{0xFF})
	cm.AddCodeSpaceRange([]byte{0x00, 0x00}, []byte{0xFF, 0xFF})

	if len(cm.codeSpaceRanges) != 2 {
		t.Errorf("expected 2 code space ranges, got %d", len(cm.codeSpaceRanges))
	}
}

func TestCMapBFCharDecode(t *testing.T) {
	cm := NewCMap("Test", CMapTypeToUnicode)

	// Add single character mapping: 0x0001 -> 'A' (UTF-16BE: 0x0041)
	cm.AddBFChar("\x00\x01", "\x00A")
	// Add another mapping: 0x0002 -> '中' (UTF-16BE: 0x4E2D)
	cm.AddBFChar("\x00\x02", "\x4E\x2D")

	// Test decode
	result := cm.Decode("\x00\x01")
	if result != "A" {
		t.Errorf("expected 'A', got '%s' (%X)", result, []byte(result))
	}

	result = cm.Decode("\x00\x02")
	if result != "中" {
		t.Errorf("expected '中', got '%s' (%X)", result, []byte(result))
	}
}

func TestCMapBFRangeDecode(t *testing.T) {
	cm := NewCMap("Test", CMapTypeToUnicode)

	// Add range mapping: 0x0041-0x005A -> A-Z
	cm.bfRanges = append(cm.bfRanges, bfrange{
		lo:  "\x00\x41",
		hi:  "\x00\x5A",
		dst: Value{data: "\x00A"},
	})

	// Test decode
	result := cm.Decode("\x00\x41")
	if result != "A" {
		t.Errorf("expected 'A', got '%s'", result)
	}

	result = cm.Decode("\x00\x42")
	if result != "B" {
		t.Errorf("expected 'B', got '%s'", result)
	}

	result = cm.Decode("\x00\x5A")
	if result != "Z" {
		t.Errorf("expected 'Z', got '%s'", result)
	}
}

func TestCMapCIDLookup(t *testing.T) {
	cm := NewCMap("Test", CMapTypeCID)

	// Add single CID mapping
	cm.AddCIDChar([]byte{0x00, 0x41}, 100)

	// Add CID range
	cm.AddCIDRange([]byte{0x00, 0x61}, []byte{0x00, 0x7A}, 200)

	// Test single char lookup
	cid, found := cm.LookupCID([]byte{0x00, 0x41})
	if !found {
		t.Error("expected to find CID for 0x0041")
	}
	if cid != 100 {
		t.Errorf("expected CID 100, got %d", cid)
	}

	// Test range lookup
	cid, found = cm.LookupCID([]byte{0x00, 0x61})
	if !found {
		t.Error("expected to find CID for 0x0061")
	}
	if cid != 200 {
		t.Errorf("expected CID 200, got %d", cid)
	}

	cid, found = cm.LookupCID([]byte{0x00, 0x62})
	if !found {
		t.Error("expected to find CID for 0x0062")
	}
	if cid != 201 {
		t.Errorf("expected CID 201, got %d", cid)
	}
}

func TestParseCMap(t *testing.T) {
	cmapData := `/CIDInit /ProcSet findresource begin
12 dict begin
begincmap
/CMapName /Test-UCS2 def
/CMapType 2 def
/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def
1 begincodespacerange
<0000> <FFFF>
endcodespacerange
3 beginbfchar
<0001> <0041>
<0002> <0042>
<0003> <0043>
endbfchar
1 beginbfrange
<0041> <005A> <0041>
endbfrange
endcmap
CMapName currentdict /CMap defineresource pop
end
end`

	cm, err := ParseCMap(strings.NewReader(cmapData), "Test-UCS2")
	if err != nil {
		t.Fatalf("ParseCMap failed: %v", err)
	}

	if cm == nil {
		t.Fatal("ParseCMap returned nil")
	}

	// Test bfchar decode
	result := cm.Decode("\x00\x01")
	if result != "A" {
		t.Errorf("expected 'A' for 0x0001, got '%s'", result)
	}

	result = cm.Decode("\x00\x02")
	if result != "B" {
		t.Errorf("expected 'B' for 0x0002, got '%s'", result)
	}
}

func TestPredefinedCMapRegistry(t *testing.T) {
	// Test that InitPredefinedCMaps was called
	names := ListRegisteredCMaps()
	if len(names) == 0 {
		t.Error("no predefined CMaps registered")
	}

	// Check for some expected CMaps
	expectedCMaps := []string{
		"Identity-H", "Identity-V",
		"GBK-EUC-H", "GBK-EUC-V",
		"B5-H", "B5-V",
		"90ms-RKSJ-H", "90ms-RKSJ-V",
		"KSC-EUC-H", "KSC-EUC-V",
	}

	for _, name := range expectedCMaps {
		cm := GetPredefinedCMap(name)
		if cm == nil {
			t.Errorf("expected predefined CMap '%s' not found", name)
		}
	}
}

func TestCMapInfo(t *testing.T) {
	info := GetCMapInfo("GBK-EUC-H")
	if info == nil {
		t.Fatal("GetCMapInfo returned nil for GBK-EUC-H")
	}

	if info.Registry != "Adobe" {
		t.Errorf("expected registry 'Adobe', got '%s'", info.Registry)
	}
	if info.Ordering != "GB1" {
		t.Errorf("expected ordering 'GB1', got '%s'", info.Ordering)
	}
	if info.WMode != 0 {
		t.Errorf("expected WMode 0, got %d", info.WMode)
	}
}

func TestIsCJKCMap(t *testing.T) {
	tests := []struct {
		name     string
		expected bool
	}{
		{"GBK-EUC-H", true},
		{"UniGB-UCS2-H", true},
		{"B5-H", true},
		{"UniCNS-UTF16-H", true},
		{"90ms-RKSJ-H", true},
		{"UniJIS-UCS2-H", true},
		{"KSC-EUC-H", true},
		{"UniKS-UTF16-H", true},
		{"Identity-H", false},
		{"WinAnsiEncoding", false},
	}

	for _, tt := range tests {
		result := IsCJKCMap(tt.name)
		if result != tt.expected {
			t.Errorf("IsCJKCMap(%s) = %v, expected %v", tt.name, result, tt.expected)
		}
	}
}

func TestGetCMapWritingMode(t *testing.T) {
	tests := []struct {
		name     string
		expected int
	}{
		{"Identity-H", 0},
		{"Identity-V", 1},
		{"GBK-EUC-H", 0},
		{"GBK-EUC-V", 1},
		{"Unknown", -1},
	}

	for _, tt := range tests {
		result := GetCMapWritingMode(tt.name)
		if result != tt.expected {
			t.Errorf("GetCMapWritingMode(%s) = %d, expected %d", tt.name, result, tt.expected)
		}
	}
}

func TestLookupPredefinedCMap(t *testing.T) {
	// Test Identity-H (should be in builtinCMapEncoding)
	enc := LookupPredefinedCMap("Identity-H")
	if enc == nil {
		t.Error("LookupPredefinedCMap returned nil for Identity-H")
	}

	// Test decode with Identity-H
	result := enc.Decode("\x00\x41\x00\x42")
	expected := "AB"
	if result != expected {
		t.Errorf("Identity-H decode: expected '%s', got '%s'", expected, result)
	}
}

func TestEnhancedCMapEncoding(t *testing.T) {
	enc := EnhancedCMapEncoding("Identity-H")
	if enc == nil {
		t.Error("EnhancedCMapEncoding returned nil for Identity-H")
	}
}

func TestToUnicodeCMap(t *testing.T) {
	cm := NewToUnicodeCMap()
	if cm == nil {
		t.Fatal("NewToUnicodeCMap returned nil")
	}

	cm.AddBFChar("\x00\x01", "\x00A")

	result := cm.Decode("\x00\x01")
	if result != "A" {
		t.Errorf("expected 'A', got '%s'", result)
	}

	// Test DecodeCID
	cm.AddBFChar("\x00\x64", "\x00B") // CID 100 -> 'B'
	result = cm.DecodeCID(100)
	if result != "B" {
		t.Errorf("DecodeCID(100) expected 'B', got '%s'", result)
	}
}

func TestCIDToGIDMap(t *testing.T) {
	// Test identity map
	identity := NewIdentityCIDToGIDMap()
	if !identity.IsIdentity() {
		t.Error("expected identity map")
	}
	if identity.LookupGID(100) != 100 {
		t.Errorf("expected GID 100, got %d", identity.LookupGID(100))
	}

	// Test explicit map
	data := []byte{0x00, 0x01, 0x00, 0x02, 0x00, 0x03}
	explicit := NewCIDToGIDMap(data)
	if explicit.IsIdentity() {
		t.Error("expected non-identity map")
	}
	if explicit.LookupGID(0) != 1 {
		t.Errorf("expected GID 1 for CID 0, got %d", explicit.LookupGID(0))
	}
	if explicit.LookupGID(1) != 2 {
		t.Errorf("expected GID 2 for CID 1, got %d", explicit.LookupGID(1))
	}
	if explicit.LookupGID(2) != 3 {
		t.Errorf("expected GID 3 for CID 2, got %d", explicit.LookupGID(2))
	}
}

func TestCIDFont(t *testing.T) {
	font := NewCIDFont()
	if font == nil {
		t.Fatal("NewCIDFont returned nil")
	}

	font.SetWritingMode(1)
	if font.WritingMode() != 1 {
		t.Errorf("expected WMode 1, got %d", font.WritingMode())
	}

	font.SetDefaultWidth(1000)
	font.SetWidth(100, 500)

	if font.GetWidth(100) != 500 {
		t.Errorf("expected width 500 for CID 100, got %d", font.GetWidth(100))
	}
	if font.GetWidth(999) != 1000 {
		t.Errorf("expected default width 1000 for CID 999, got %d", font.GetWidth(999))
	}
}

func TestCIDFontWithToUnicode(t *testing.T) {
	font := NewCIDFont()

	toUnicode := NewToUnicodeCMap()
	toUnicode.AddBFChar("\x00\x01", "\x00A")
	toUnicode.AddBFChar("\x00\x02", "\x00B")

	font.SetToUnicode(toUnicode)

	result := font.DecodeToUnicode("\x00\x01\x00\x02")
	if result != "AB" {
		t.Errorf("expected 'AB', got '%s'", result)
	}
}

func TestParseHexString(t *testing.T) {
	tests := []struct {
		input    string
		expected []byte
	}{
		{"0041", []byte{0x00, 0x41}},
		{"00 41", []byte{0x00, 0x41}},
		{"004", []byte{0x00, 0x40}}, // Odd length padded with 0
		{"FFFF", []byte{0xFF, 0xFF}},
		{"", []byte{}},
	}

	for _, tt := range tests {
		result, err := parseHexString(tt.input)
		if err != nil {
			t.Errorf("parseHexString(%s) error: %v", tt.input, err)
			continue
		}
		if !bytes.Equal(result, tt.expected) {
			t.Errorf("parseHexString(%s) = %X, expected %X", tt.input, result, tt.expected)
		}
	}
}

func TestCMapDecodeUTF16BE(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"\x00A", "A"},
		{"\x00A\x00B", "AB"},
		{"\x4E\x2D", "中"},
		{"\x00A\x4E\x2D", "A中"},
		{"", ""},
		{"A", "A"}, // Single byte passed through
	}

	for _, tt := range tests {
		result := cmapDecodeUTF16BE(tt.input)
		if result != tt.expected {
			t.Errorf("cmapDecodeUTF16BE(%X) = '%s', expected '%s'", []byte(tt.input), result, tt.expected)
		}
	}
}

func TestCMapUseCMap(t *testing.T) {
	// Create parent CMap
	parent := NewCMap("Parent", CMapTypeToUnicode)
	parent.AddBFChar("\x00\x01", "\x00A")

	// Create child CMap that uses parent
	child := NewCMap("Child", CMapTypeToUnicode)
	child.AddBFChar("\x00\x02", "\x00B")
	child.SetUseCMap(parent)

	// Child should decode its own mappings
	result := child.Decode("\x00\x02")
	if result != "B" {
		t.Errorf("expected 'B', got '%s'", result)
	}
}

func TestCMapString(t *testing.T) {
	cm := NewCMap("Test", CMapTypeCID)
	cm.SetCIDSystemInfo("Adobe", "GB1", 5)
	cm.WMode = 0

	str := cm.String()
	if !strings.Contains(str, "Test") {
		t.Error("String() should contain CMap name")
	}
	if !strings.Contains(str, "Adobe") {
		t.Error("String() should contain Registry")
	}
	if !strings.Contains(str, "GB1") {
		t.Error("String() should contain Ordering")
	}
}

// Benchmark tests
func BenchmarkCMapDecode(b *testing.B) {
	cm := NewCMap("Test", CMapTypeToUnicode)

	// Add some mappings
	for i := 0; i < 256; i++ {
		cm.AddBFChar(string([]byte{0x00, byte(i)}), string([]byte{0x00, byte(i + 0x41)}))
	}

	input := "\x00\x01\x00\x02\x00\x03\x00\x04\x00\x05"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = cm.Decode(input)
	}
}

func BenchmarkCMapLookupCID(b *testing.B) {
	cm := NewCMap("Test", CMapTypeCID)

	// Add CID ranges
	for i := 0; i < 16; i++ {
		start := i * 256
		cm.AddCIDRange([]byte{byte(i), 0x00}, []byte{byte(i), 0xFF}, start)
	}

	code := []byte{0x08, 0x80}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = cm.LookupCID(code)
	}
}

func BenchmarkParseCMap(b *testing.B) {
	var builder strings.Builder
	builder.WriteString("/CIDInit /ProcSet findresource begin\n")
	builder.WriteString("12 dict begin\n")
	builder.WriteString("begincmap\n")
	builder.WriteString("/CMapName /Test-UCS2 def\n")
	builder.WriteString("/CMapType 2 def\n")
	builder.WriteString("1 begincodespacerange\n")
	builder.WriteString("<0000> <FFFF>\n")
	builder.WriteString("endcodespacerange\n")
	builder.WriteString("100 beginbfchar\n")

	hexDigits := "0123456789ABCDEF"
	for i := 0; i < 100; i++ {
		builder.WriteByte('<')
		builder.WriteByte(hexDigits[(i>>12)&0xF])
		builder.WriteByte(hexDigits[(i>>8)&0xF])
		builder.WriteByte(hexDigits[(i>>4)&0xF])
		builder.WriteByte(hexDigits[i&0xF])
		builder.WriteString("> <")
		j := i + 0x41
		builder.WriteByte(hexDigits[(j>>12)&0xF])
		builder.WriteByte(hexDigits[(j>>8)&0xF])
		builder.WriteByte(hexDigits[(j>>4)&0xF])
		builder.WriteByte(hexDigits[j&0xF])
		builder.WriteString(">\n")
	}
	builder.WriteString("endbfchar\n")
	builder.WriteString("endcmap\n")
	builder.WriteString("end\n")
	builder.WriteString("end\n")

	cmapData := builder.String()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ParseCMap(strings.NewReader(cmapData), "Test")
	}
}

func BenchmarkCIDToGIDLookup(b *testing.B) {
	// Create a CIDToGIDMap with 65536 entries
	data := make([]byte, 65536*2)
	for i := 0; i < 65536; i++ {
		data[i*2] = byte(i >> 8)
		data[i*2+1] = byte(i & 0xFF)
	}

	m := NewCIDToGIDMap(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m.LookupGID(i % 65536)
	}
}
