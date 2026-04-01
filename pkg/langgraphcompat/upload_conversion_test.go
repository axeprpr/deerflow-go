package langgraphcompat

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConvertPDFToMarkdownExtractsTextFromFlateStream(t *testing.T) {
	text := "Quarterly Review\nRevenue grew 20%"
	md, err := convertPDFToMarkdown(writeTempFile(t, "report.pdf", minimalPDF(t, text, pdfCompressionFlate)))
	if err != nil {
		t.Fatalf("convert pdf: %v", err)
	}
	if !strings.Contains(md, "Quarterly Review") {
		t.Fatalf("markdown missing first line: %q", md)
	}
	if !strings.Contains(md, "Revenue grew 20%") {
		t.Fatalf("markdown missing second line: %q", md)
	}
}

func TestConvertPDFToMarkdownExtractsTextFromZlibFlateStream(t *testing.T) {
	text := "Roadmap\nLaunch in Q3"
	md, err := convertPDFToMarkdown(writeTempFile(t, "roadmap.pdf", minimalPDF(t, text, pdfCompressionZlib)))
	if err != nil {
		t.Fatalf("convert pdf: %v", err)
	}
	if !strings.Contains(md, "Roadmap") {
		t.Fatalf("markdown missing first line: %q", md)
	}
	if !strings.Contains(md, "Launch in Q3") {
		t.Fatalf("markdown missing second line: %q", md)
	}
}

func TestConvertUploadedDocumentToMarkdownReturnsEmptyForPDFWithoutText(t *testing.T) {
	content, err := convertUploadedDocumentToMarkdown(writeTempFile(t, "image.pdf", []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\n%%EOF")), ".pdf")
	if err != nil {
		t.Fatalf("convert pdf: %v", err)
	}
	if content != "" {
		t.Fatalf("content=%q want empty", content)
	}
}

func TestConvertUploadedDocumentToMarkdownExtractsLegacyOfficeText(t *testing.T) {
	tests := []struct {
		name string
		ext  string
		data []byte
		want []string
	}{
		{
			name: "doc utf16",
			ext:  ".doc",
			data: append(minimalOLEHeader(), utf16LEText("Project Phoenix\nLaunch checklist")...),
			want: []string{"Project Phoenix", "Launch checklist"},
		},
		{
			name: "ppt ascii",
			ext:  ".ppt",
			data: append(minimalOLEHeader(), []byte("Quarterly Kickoff\x00North Star Metrics\x00")...),
			want: []string{"Quarterly Kickoff", "North Star Metrics"},
		},
		{
			name: "xls mixed",
			ext:  ".xls",
			data: append(append(minimalOLEHeader(), []byte("Revenue\x00Margin\x00")...), utf16LEText("Q1 2026")...),
			want: []string{"Revenue", "Margin", "Q1 2026"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			md, err := convertUploadedDocumentToMarkdown(writeTempFile(t, "legacy"+tt.ext, tt.data), tt.ext)
			if err != nil {
				t.Fatalf("convert legacy office: %v", err)
			}
			for _, want := range tt.want {
				if !strings.Contains(md, want) {
					t.Fatalf("markdown missing %q: %q", want, md)
				}
			}
			if strings.Contains(md, "Automatic Markdown extraction is not available") {
				t.Fatalf("unexpected placeholder markdown: %q", md)
			}
		})
	}
}

func TestConvertDOCXToMarkdownPreservesParagraphsAndTables(t *testing.T) {
	path := writeTempFile(t, "report.docx", minimalZipArchive(t, map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>Project Phoenix</w:t></w:r></w:p>
    <w:p><w:r><w:t>Launch checklist</w:t></w:r></w:p>
    <w:tbl>
      <w:tr>
        <w:tc><w:p><w:r><w:t>Owner</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>Status</w:t></w:r></w:p></w:tc>
      </w:tr>
      <w:tr>
        <w:tc><w:p><w:r><w:t>Ops</w:t></w:r></w:p></w:tc>
        <w:tc><w:p><w:r><w:t>Ready</w:t></w:r></w:p></w:tc>
      </w:tr>
    </w:tbl>
  </w:body>
</w:document>`,
	}))

	md, err := convertDOCXToMarkdown(path)
	if err != nil {
		t.Fatalf("convert docx: %v", err)
	}
	for _, want := range []string{
		"Project Phoenix",
		"Launch checklist",
		"| Owner | Status |",
		"| --- | --- |",
		"| Ops | Ready |",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q: %q", want, md)
		}
	}
}

func TestConvertDOCXToMarkdownPreservesHeadingsAndLists(t *testing.T) {
	path := writeTempFile(t, "brief.docx", minimalZipArchive(t, map[string]string{
		"word/document.xml": `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p>
      <w:pPr><w:pStyle w:val="Heading1"/></w:pPr>
      <w:r><w:t>Launch Plan</w:t></w:r>
    </w:p>
    <w:p>
      <w:pPr><w:pStyle w:val="Heading2"/></w:pPr>
      <w:r><w:t>Milestones</w:t></w:r>
    </w:p>
    <w:p>
      <w:pPr>
        <w:numPr>
          <w:ilvl w:val="0"/>
          <w:numId w:val="1"/>
        </w:numPr>
      </w:pPr>
      <w:r><w:t>Prepare launch assets</w:t></w:r>
    </w:p>
    <w:p>
      <w:pPr>
        <w:numPr>
          <w:ilvl w:val="0"/>
          <w:numId w:val="1"/>
        </w:numPr>
      </w:pPr>
      <w:r><w:t>Train support team</w:t></w:r>
    </w:p>
  </w:body>
</w:document>`,
	}))

	md, err := convertDOCXToMarkdown(path)
	if err != nil {
		t.Fatalf("convert docx headings/lists: %v", err)
	}
	for _, want := range []string{
		"# Launch Plan",
		"## Milestones",
		"- Prepare launch assets",
		"- Train support team",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q: %q", want, md)
		}
	}
}

func TestConvertXLSXToMarkdownUsesSheetNamesAndMarkdownTables(t *testing.T) {
	path := writeTempFile(t, "forecast.xlsx", minimalZipArchive(t, map[string]string{
		"xl/workbook.xml": `<?xml version="1.0" encoding="UTF-8"?>
<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
  <sheets>
    <sheet name="Forecast" sheetId="1" r:id="rId1"/>
  </sheets>
</workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
</Relationships>`,
		"xl/sharedStrings.xml": `<?xml version="1.0" encoding="UTF-8"?>
<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <si><t>Quarter</t></si>
  <si><t>Revenue</t></si>
  <si><t>Q1</t></si>
</sst>`,
		"xl/worksheets/sheet1.xml": `<?xml version="1.0" encoding="UTF-8"?>
<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
  <sheetData>
    <row r="1">
      <c r="A1" t="s"><v>0</v></c>
      <c r="B1" t="s"><v>1</v></c>
    </row>
    <row r="2">
      <c r="A2" t="s"><v>2</v></c>
      <c r="B2"><v>120</v></c>
    </row>
  </sheetData>
</worksheet>`,
	}))

	md, err := convertXLSXToMarkdown(path)
	if err != nil {
		t.Fatalf("convert xlsx: %v", err)
	}
	for _, want := range []string{
		"## Forecast",
		"| Quarter | Revenue |",
		"| --- | --- |",
		"| Q1 | 120 |",
	} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q: %q", want, md)
		}
	}
}

type pdfCompressionMode int

const (
	pdfCompressionFlate pdfCompressionMode = iota
	pdfCompressionZlib
)

func writeTempFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func minimalPDF(t *testing.T, text string, mode pdfCompressionMode) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer, err := newPDFCompressionWriter(&compressed, mode)
	if err != nil {
		t.Fatalf("new compression writer: %v", err)
	}
	stream := "BT\n/F1 12 Tf\n72 720 Td\n"
	for _, line := range strings.Split(text, "\n") {
		stream += fmt.Sprintf("(%s) Tj\n0 -18 Td\n", escapePDFLiteral(line))
	}
	stream += "ET\n"
	if _, err := writer.Write([]byte(stream)); err != nil {
		t.Fatalf("compress stream: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close flate writer: %v", err)
	}

	return []byte(fmt.Sprintf(`%%PDF-1.4
1 0 obj
<< /Length %d /Filter /FlateDecode >>
stream
%sendstream
endobj
trailer
<< /Root 1 0 R >>
%%%%EOF
`, compressed.Len(), compressed.String()))
}

func escapePDFLiteral(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`)
	return replacer.Replace(s)
}

func newPDFCompressionWriter(dst *bytes.Buffer, mode pdfCompressionMode) (io.WriteCloser, error) {
	switch mode {
	case pdfCompressionZlib:
		return zlib.NewWriterLevel(dst, zlib.DefaultCompression)
	default:
		return flate.NewWriter(dst, flate.DefaultCompression)
	}
}

func minimalOLEHeader() []byte {
	return []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}
}

func utf16LEText(text string) []byte {
	var out []byte
	for _, r := range text {
		out = append(out, byte(r), byte(r>>8))
	}
	out = append(out, 0x00, 0x00)
	return out
}

func minimalZipArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}
