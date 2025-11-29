package pdf

import (
	"testing"
)

// BenchmarkType1FontCreation benchmarks the creation of a Type1Font from a simple font data
func BenchmarkType1FontCreation(b *testing.B) {
	// Simple Type1 font data (minimal example)
	fontData := `%%!PS-AdobeFont-1.0: TestFont 1.0
%%%%CreationDate: 1998 Feb 18 14:00:00
%% Copyright (URW)++,Copyright 1999 by (URW)++ Design & Development
%% (URW)++,Copyright 1999 by (URW)++ Design & Development
FontDirectory/TestFont known{/TestFont findfont dup/Encoding StandardEncoding copy
dup length array copy
dup/FontName/TestFont put
dup/OrigFontName/TestFont put
load 0 code
} if
/TestFont findfont definefont pop
%%%%EndComments
%%%%BeginProcSet: TestFont 1.0 0
/TestFont 10 dict def
TestFont begin
/FontType 1 def
/FontName /TestFont def
/FontInfo 8 dict def
FontInfo begin
/Notice ((URW)++,Copyright 1999 by (URW)++ Design & Development) def
/FullName (TestFont) def
/FamilyName (TestFont) def
/Weight (Regular) def
/ItalicAngle 0 def
/isFixedPitch false def
/UnderlinePosition -100 def
/UnderlineThickness 50 def
end
/Encoding StandardEncoding def
/FontBBox{-168 -218 1000 935}def
/PaintType 0 def
/FontMatrix[0.001 0 0 0.001 0 0]def
/Private 9 dict def
Private begin
/BlueValues[-12 0 486 498 574 586 638 650 722 734 784 796] def
/OtherBlues[-217 -205] def
/FamilyBlues[-12 0 486 498 574 586 638 650 722 734 784 796] def
/FamilyOtherBlues[-217 -205] def
/BlueScale 0.039625 def
/BlueShift 7 def
/BlueFuzz 0 def
/StdHW[50] def
/StdVW[80] def
end
/CharStrings 5 dict def
CharStrings begin
/.notdef 0 def
/space 1 def
/exclam 2 def
/quotedbl 3 def
/numbersign 4 def
end
end
%%%%EndProcSet
%%%%Trailer`

	data := []byte(fontData)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := NewType1Font(data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkType1FontWidthLookup benchmarks glyph width lookups
func BenchmarkType1FontWidthLookup(b *testing.B) {
	fontData := `%%!PS-AdobeFont-1.0: TestFont 1.0
%%%%CreationDate: 1998 Feb 18 14:00:00
%% Copyright (URW)++,Copyright 1999 by (URW)++ Design & Development
%% (URW)++,Copyright 1999 by (URW)++ Design & Development
FontDirectory/TestFont known{/TestFont findfont dup/Encoding StandardEncoding copy
dup length array copy
dup/FontName/TestFont put
dup/OrigFontName/TestFont put
load 0 code
} if
/TestFont findfont definefont pop
%%%%EndComments
%%%%BeginProcSet: TestFont 1.0 0
/TestFont 10 dict def
TestFont begin
/FontType 1 def
/FontName /TestFont def
/FontInfo 8 dict def
FontInfo begin
/Notice ((URW)++,Copyright 1999 by (URW)++ Design & Development) def
/FullName (TestFont) def
/FamilyName (TestFont) def
/Weight (Regular) def
/ItalicAngle 0 def
/isFixedPitch false def
/UnderlinePosition -100 def
/UnderlineThickness 50 def
end
/Encoding StandardEncoding def
/FontBBox{-168 -218 1000 935}def
/PaintType 0 def
/FontMatrix[0.001 0 0 0.001 0 0]def
/Private 9 dict def
Private begin
/BlueValues[-12 0 486 498 574 586 638 650 722 734 784 796] def
/OtherBlues[-217 -205] def
/FamilyBlues[-12 0 486 498 574 586 638 650 722 734 784 796] def
/FamilyOtherBlues[-217 -205] def
/BlueScale 0.039625 def
/BlueShift 7 def
/BlueFuzz 0 def
/StdHW[50] def
/StdVW[80] def
end
/CharStrings 5 dict def
CharStrings begin
/.notdef 0 def
/space 1 def
/exclam 2 def
/quotedbl 3 def
/numbersign 4 def
end
end
%%%%EndProcSet
%%%%Trailer`

	data := []byte(fontData)
	font, err := NewType1Font(data)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = font.GlyphWidth("a")
	}
}

// BenchmarkType1FontEncodingLookup benchmarks encoding lookups
func BenchmarkType1FontEncodingLookup(b *testing.B) {
	fontData := `%%!PS-AdobeFont-1.0: TestFont 1.0
%%%%CreationDate: 1998 Feb 18 14:00:00
%% Copyright (URW)++,Copyright 1999 by (URW)++ Design & Development
%% (URW)++,Copyright 1999 by (URW)++ Design & Development
FontDirectory/TestFont known{/TestFont findfont dup/Encoding StandardEncoding copy
dup length array copy
dup/FontName/TestFont put
dup/OrigFontName/TestFont put
load 0 code
} if
/TestFont findfont definefont pop
%%%%EndComments
%%%%BeginProcSet: TestFont 1.0 0
/TestFont 10 dict def
TestFont begin
/FontType 1 def
/FontName /TestFont def
/FontInfo 8 dict def
FontInfo begin
/Notice ((URW)++,Copyright 1999 by (URW)++ Design & Development) def
/FullName (TestFont) def
/FamilyName (TestFont) def
/Weight (Regular) def
/ItalicAngle 0 def
/isFixedPitch false def
/UnderlinePosition -100 def
/UnderlineThickness 50 def
end
/Encoding StandardEncoding def
/FontBBox{-168 -218 1000 935}def
/PaintType 0 def
/FontMatrix[0.001 0 0 0.001 0 0]def
/Private 9 dict def
Private begin
/BlueValues[-12 0 486 498 574 586 638 650 722 734 784 796] def
/OtherBlues[-217 -205] def
/FamilyBlues[-12 0 486 498 574 586 638 650 722 734 784 796] def
/FamilyOtherBlues[-217 -205] def
/BlueScale 0.039625 def
/BlueShift 7 def
/BlueFuzz 0 def
/StdHW[50] def
/StdVW[80] def
end
/CharStrings 5 dict def
CharStrings begin
/.notdef 0 def
/space 1 def
/exclam 2 def
/quotedbl 3 def
/numbersign 4 def
end
end
%%%%EndProcSet
%%%%Trailer`

	data := []byte(fontData)
	font, err := NewType1Font(data)
	if err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = font.GlyphName('a')
	}
}
