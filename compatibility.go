// compatibility.go - PDF format compatibility handling
package pdf

import (
	"fmt"
	"strings"
)

// PDFVersion represents a PDF version
type PDFVersion struct {
	Major int
	Minor int
}

// String returns the version string
func (v PDFVersion) String() string {
	return fmt.Sprintf("%d.%d", v.Major, v.Minor)
}

// SupportedVersions defines the supported PDF versions
var SupportedVersions = []PDFVersion{
	{1, 0}, {1, 1}, {1, 2}, {1, 3}, {1, 4}, {1, 5}, {1, 6}, {1, 7},
	{2, 0},
}

// IsSupported checks if a version is supported
func (v PDFVersion) IsSupported() bool {
	for _, sv := range SupportedVersions {
		if sv.Major == v.Major && sv.Minor == v.Minor {
			return true
		}
	}
	return false
}

// PDFCompatibilityInfo holds compatibility information
type PDFCompatibilityInfo struct {
	Version             PDFVersion
	IsLinearized        bool
	LinearizationParams map[string]interface{}
	SubFormat           string // "PDF/A", "PDF/X", or ""
	Encryption          string
	HasTransparency     bool
	HasLayers           bool
	HasForms            bool
	HasJavaScript       bool
	Warnings            []string
	Errors              []string
}

// CheckPDFCompatibility analyzes a PDF file for compatibility
func CheckPDFCompatibility(data []byte) (*PDFCompatibilityInfo, error) {
	info := &PDFCompatibilityInfo{}

	// Parse version
	version, err := parsePDFVersion(data)
	if err != nil {
		return nil, err
	}
	info.Version = version

	// Check if version is supported
	if !version.IsSupported() {
		return nil, fmt.Errorf("PDF version %s is not supported", version.String())
	}

	// Check for linearization
	info.IsLinearized = isLinearizedPDF(data)

	// Check for sub-formats
	info.SubFormat = detectSubFormat(data)

	// Check for advanced features
	info.HasTransparency = hasTransparency(data)
	info.HasLayers = hasLayers(data)
	info.HasForms = hasForms(data)
	info.HasJavaScript = hasJavaScript(data)

	// Generate warnings for unsupported features
	if info.HasTransparency {
		info.Warnings = append(info.Warnings, "PDF contains transparency features (may not be fully supported)")
	}
	if info.HasLayers {
		info.Warnings = append(info.Warnings, "PDF contains layers/OCG (may not be fully supported)")
	}
	if info.HasForms {
		info.Warnings = append(info.Warnings, "PDF contains interactive forms (may not be fully supported)")
	}
	if info.HasJavaScript {
		info.Warnings = append(info.Warnings, "PDF contains JavaScript (may not be fully supported)")
	}

	return info, nil
}

// parsePDFVersion extracts PDF version from header
func parsePDFVersion(data []byte) (PDFVersion, error) {
	if len(data) < 8 {
		return PDFVersion{}, fmt.Errorf("data too short for PDF header")
	}

	// Find %PDF- header
	sig := "%PDF-"
	sigIdx := -1
	for i := 0; i <= len(data)-len(sig); i++ {
		if string(data[i:i+len(sig)]) == sig {
			sigIdx = i
			break
		}
	}
	if sigIdx == -1 {
		return PDFVersion{}, fmt.Errorf("not a PDF file: missing %%PDF- header")
	}

	// sigIdx+7 points to the last character of version (e.g., '7' in '%PDF-1.7')
	// We need at least sigIdx+8 bytes to have the complete version string
	if sigIdx+8 > len(data) {
		return PDFVersion{}, fmt.Errorf("not a PDF file: invalid header")
	}

	major := int(data[sigIdx+5] - '0')
	minor := int(data[sigIdx+7] - '0')

	return PDFVersion{Major: major, Minor: minor}, nil
}

// isLinearizedPDF checks if PDF is linearized
func isLinearizedPDF(data []byte) bool {
	// Linearized PDFs have a linearization dictionary as the first object
	// Look for "/Linearized" in the first few objects
	dataStr := string(data)
	return strings.Contains(dataStr, "/Linearized")
}

// detectSubFormat detects PDF/A or PDF/X format
func detectSubFormat(data []byte) string {
	dataStr := string(data)

	// Check for PDF/A in metadata
	if strings.Contains(dataStr, "pdfaid:part") && strings.Contains(dataStr, "pdfaid:conformance") {
		return "PDF/A"
	}

	// Check for PDF/X in metadata
	if strings.Contains(dataStr, "pdfx:") || strings.Contains(dataStr, "PDF/X") {
		return "PDF/X"
	}

	// Check for GTS_PDFA or GTS_PDFX (older PDF/A and PDF/X identifiers)
	if strings.Contains(dataStr, "/GTS_PDFA") {
		return "PDF/A"
	}
	if strings.Contains(dataStr, "/GTS_PDFX") {
		return "PDF/X"
	}

	return ""
}

// hasTransparency checks for transparency features
func hasTransparency(data []byte) bool {
	dataStr := string(data)
	return strings.Contains(dataStr, "/GS") || // Graphics state with transparency
		strings.Contains(dataStr, "/SMask") || // Soft mask
		strings.Contains(dataStr, "/BM") // Blend mode
}

// hasLayers checks for layers/OCG
func hasLayers(data []byte) bool {
	dataStr := string(data)
	return strings.Contains(dataStr, "/OCG") || strings.Contains(dataStr, "/D")
}

// hasForms checks for interactive forms
func hasForms(data []byte) bool {
	dataStr := string(data)
	return strings.Contains(dataStr, "/AcroForm") || strings.Contains(dataStr, "/FT")
}

// ValidatePDFA validates PDF/A compliance
func ValidatePDFA(data []byte) ([]string, error) {
	var warnings []string
	dataStr := string(data)

	// Check for required PDF/A metadata
	if !strings.Contains(dataStr, "pdfaid:part") {
		warnings = append(warnings, "Missing PDF/A identification metadata")
	}

	// Check for embedded fonts (PDF/A requirement)
	if !strings.Contains(dataStr, "/Font") {
		warnings = append(warnings, "No fonts found - PDF/A requires all fonts to be embedded")
	}

	// Check for transparency (not allowed in PDF/A-1)
	if strings.Contains(dataStr, "/SMask") || strings.Contains(dataStr, "/BM") {
		warnings = append(warnings, "Transparency found - not allowed in PDF/A-1")
	}

	// Check for JavaScript (not allowed in PDF/A)
	if strings.Contains(dataStr, "/JS") || strings.Contains(dataStr, "/JavaScript") {
		warnings = append(warnings, "JavaScript found - not allowed in PDF/A")
	}

	return warnings, nil
}

// ValidatePDFX validates PDF/X compliance
func ValidatePDFX(data []byte) ([]string, error) {
	var warnings []string
	dataStr := string(data)

	// Check for required PDF/X metadata
	if !strings.Contains(dataStr, "pdfx:") && !strings.Contains(dataStr, "/GTS_PDFX") {
		warnings = append(warnings, "Missing PDF/X identification metadata")
	}

	// Check for output intents (required for PDF/X)
	if !strings.Contains(dataStr, "/OutputIntents") {
		warnings = append(warnings, "Missing output intents - required for PDF/X")
	}

	// Check for color spaces
	if !strings.Contains(dataStr, "/ColorSpace") {
		warnings = append(warnings, "No color space definitions found")
	}

	return warnings, nil
}

// hasJavaScript checks for JavaScript
func hasJavaScript(data []byte) bool {
	dataStr := string(data)
	return strings.Contains(dataStr, "/JS") || strings.Contains(dataStr, "/JavaScript")
}
