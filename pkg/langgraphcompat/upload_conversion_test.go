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
