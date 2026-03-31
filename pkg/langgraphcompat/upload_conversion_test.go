package langgraphcompat

import (
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
