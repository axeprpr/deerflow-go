package langgraphcompat

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

var convertibleUploadExtensions = map[string]struct{}{
	".pdf":  {},
	".ppt":  {},
	".pptx": {},
	".xls":  {},
	".xlsx": {},
	".doc":  {},
	".docx": {},
}

func isConvertibleUploadExtension(name string) bool {
	_, ok := convertibleUploadExtensions[strings.ToLower(filepath.Ext(name))]
	return ok
}

func generateUploadMarkdownCompanion(path string) (string, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if _, ok := convertibleUploadExtensions[ext]; !ok {
		return "", nil
	}

	content, err := convertUploadedDocumentToMarkdown(path, ext)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(content) == "" {
		content = defaultConversionPlaceholder(filepath.Base(path), ext)
	}

	mdPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".md"
	if err := os.WriteFile(mdPath, []byte(content), 0o644); err != nil {
		return "", err
	}
	return mdPath, nil
}

func convertUploadedDocumentToMarkdown(path string, ext string) (string, error) {
	switch ext {
	case ".pdf":
		return convertPDFToMarkdown(path)
	case ".doc", ".ppt", ".xls":
		return convertLegacyOfficeToMarkdown(path)
	case ".docx":
		return convertDOCXToMarkdown(path)
	case ".pptx":
		return convertPPTXToMarkdown(path)
	case ".xlsx":
		return convertXLSXToMarkdown(path)
	default:
		return defaultConversionPlaceholder(filepath.Base(path), ext), nil
	}
}

func defaultConversionPlaceholder(name string, ext string) string {
	return fmt.Sprintf("# %s\n\nAutomatic Markdown extraction is not available for `%s` yet.\nOpen the original file from `/mnt/user-data/uploads/%s`.\n", name, ext, name)
}

func convertLegacyOfficeToMarkdown(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	text := extractLegacyOfficeText(data)
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	return "# " + filepath.Base(path) + "\n\n" + text + "\n", nil
}

func convertPDFToMarkdown(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	text := extractPDFText(data)
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	return "# " + filepath.Base(path) + "\n\n" + text + "\n", nil
}

func convertDOCXToMarkdown(path string) (string, error) {
	rc, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	file := zipEntry(&rc.Reader, "word/document.xml")
	if file == nil {
		return "", fmt.Errorf("word/document.xml not found")
	}

	text, err := extractXMLText(file)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	return "# " + filepath.Base(path) + "\n\n" + text + "\n", nil
}

func convertPPTXToMarkdown(path string) (string, error) {
	rc, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	var slideNames []string
	for _, f := range rc.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			slideNames = append(slideNames, f.Name)
		}
	}
	sort.Slice(slideNames, func(i, j int) bool { return naturalLess(slideNames[i], slideNames[j]) })
	if len(slideNames) == 0 {
		return "", fmt.Errorf("no slide xml found")
	}

	var sections []string
	for idx, name := range slideNames {
		file := zipEntry(&rc.Reader, name)
		if file == nil {
			continue
		}
		text, err := extractXMLText(file)
		if err != nil {
			return "", err
		}
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		sections = append(sections, fmt.Sprintf("## Slide %d\n\n%s", idx+1, text))
	}
	if len(sections) == 0 {
		return "", nil
	}
	return "# " + filepath.Base(path) + "\n\n" + strings.Join(sections, "\n\n") + "\n", nil
}

func convertXLSXToMarkdown(path string) (string, error) {
	rc, err := zip.OpenReader(path)
	if err != nil {
		return "", err
	}
	defer rc.Close()

	sharedStrings, err := readSharedStrings(&rc.Reader)
	if err != nil {
		return "", err
	}

	sheetNames := make([]string, 0)
	for _, f := range rc.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheetNames = append(sheetNames, f.Name)
		}
	}
	sort.Slice(sheetNames, func(i, j int) bool { return naturalLess(sheetNames[i], sheetNames[j]) })
	if len(sheetNames) == 0 {
		return "", fmt.Errorf("no worksheet xml found")
	}

	var sections []string
	for idx, name := range sheetNames {
		file := zipEntry(&rc.Reader, name)
		if file == nil {
			continue
		}
		rows, err := parseWorksheetRows(file, sharedStrings)
		if err != nil {
			return "", err
		}
		if len(rows) == 0 {
			continue
		}
		lines := []string{fmt.Sprintf("## Sheet %d", idx+1), ""}
		for _, row := range rows {
			lines = append(lines, "| "+strings.Join(row, " | ")+" |")
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	if len(sections) == 0 {
		return "", nil
	}
	return "# " + filepath.Base(path) + "\n\n" + strings.Join(sections, "\n\n") + "\n", nil
}

func zipEntry(rc *zip.Reader, name string) *zip.File {
	for _, f := range rc.File {
		if f.Name == name {
			return f
		}
	}
	return nil
}

func extractXMLText(file *zip.File) (string, error) {
	r, err := file.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()

	decoder := xml.NewDecoder(r)
	var parts []string
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
		charData, ok := token.(xml.CharData)
		if !ok {
			continue
		}
		text := strings.TrimSpace(string(charData))
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n"), nil
}

func readSharedStrings(rc *zip.Reader) ([]string, error) {
	file := zipEntry(rc, "xl/sharedStrings.xml")
	if file == nil {
		return nil, nil
	}
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()

	decoder := xml.NewDecoder(r)
	var values []string
	var inSI bool
	var builder strings.Builder
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		switch tok := token.(type) {
		case xml.StartElement:
			if tok.Name.Local == "si" {
				inSI = true
				builder.Reset()
			}
		case xml.EndElement:
			if tok.Name.Local == "si" && inSI {
				values = append(values, strings.TrimSpace(builder.String()))
				inSI = false
			}
		case xml.CharData:
			if inSI {
				builder.WriteString(string(tok))
			}
		}
	}
	return values, nil
}

func parseWorksheetRows(file *zip.File, sharedStrings []string) ([][]string, error) {
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()

	decoder := xml.NewDecoder(r)
	var rows [][]string
	var current []string
	var currentCell *cell
	var currentElement string
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		switch tok := token.(type) {
		case xml.StartElement:
			currentElement = tok.Name.Local
			switch tok.Name.Local {
			case "row":
				current = nil
			case "c":
				currentCell = &cell{}
				for _, attr := range tok.Attr {
					switch attr.Name.Local {
					case "r":
						currentCell.ref = attr.Value
					case "t":
						currentCell.typ = attr.Value
					}
				}
			}
		case xml.EndElement:
			switch tok.Name.Local {
			case "c":
				if currentCell != nil {
					col := columnIndex(currentCell.ref)
					for len(current) <= col {
						current = append(current, "")
					}
					current[col] = resolveCellValue(*currentCell, sharedStrings)
				}
				currentCell = nil
			case "row":
				if len(current) > 0 {
					rows = append(rows, trimTrailingEmpty(current))
				}
			}
			currentElement = ""
		case xml.CharData:
			if currentCell == nil {
				continue
			}
			text := string(tok)
			switch currentElement {
			case "v":
				currentCell.value += text
			case "t":
				currentCell.inline += text
			}
		}
	}
	return rows, nil
}

func resolveCellValue(c cell, sharedStrings []string) string {
	switch c.typ {
	case "s":
		idx, err := strconv.Atoi(strings.TrimSpace(c.value))
		if err == nil && idx >= 0 && idx < len(sharedStrings) {
			return strings.TrimSpace(sharedStrings[idx])
		}
	case "inlineStr":
		return strings.TrimSpace(c.inline)
	}
	return strings.TrimSpace(c.value)
}

type cell struct {
	ref    string
	typ    string
	value  string
	inline string
}

func columnIndex(ref string) int {
	letters := strings.Map(func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r
		}
		if r >= 'a' && r <= 'z' {
			return r - ('a' - 'A')
		}
		return -1
	}, ref)
	if letters == "" {
		return 0
	}
	idx := 0
	for _, r := range letters {
		idx = idx*26 + int(r-'A'+1)
	}
	if idx == 0 {
		return 0
	}
	return idx - 1
}

func trimTrailingEmpty(values []string) []string {
	last := len(values) - 1
	for last >= 0 && strings.TrimSpace(values[last]) == "" {
		last--
	}
	if last < 0 {
		return []string{}
	}
	return values[:last+1]
}

var naturalNumberRE = regexp.MustCompile(`\d+`)

func naturalLess(left string, right string) bool {
	leftNum := naturalNumberRE.FindString(left)
	rightNum := naturalNumberRE.FindString(right)
	if leftNum != "" && rightNum != "" {
		li, _ := strconv.Atoi(leftNum)
		ri, _ := strconv.Atoi(rightNum)
		if li != ri {
			return li < ri
		}
	}
	return left < right
}

func extractPDFText(data []byte) string {
	streams := extractPDFStreams(data)
	if len(streams) == 0 {
		return ""
	}

	var sections []string
	for _, stream := range streams {
		text := strings.TrimSpace(extractPDFTextFromStream(stream))
		if text == "" {
			continue
		}
		sections = append(sections, text)
	}
	return strings.Join(sections, "\n\n")
}

func extractPDFStreams(data []byte) []string {
	var streams []string
	offset := 0
	for {
		idx := bytes.Index(data[offset:], []byte("stream"))
		if idx < 0 {
			break
		}
		idx += offset
		start := idx + len("stream")
		if start < len(data) && data[start] == '\r' {
			start++
		}
		if start < len(data) && data[start] == '\n' {
			start++
		}
		endRel := bytes.Index(data[start:], []byte("endstream"))
		if endRel < 0 {
			break
		}
		end := start + endRel
		raw := bytes.Trim(data[start:end], "\r\n")
		dict := pdfStreamDict(data, idx)
		streams = append(streams, decodePDFStream(raw, dict))
		offset = end + len("endstream")
	}
	return streams
}

func pdfStreamDict(data []byte, streamIdx int) string {
	searchStart := streamIdx - 2048
	if searchStart < 0 {
		searchStart = 0
	}
	window := data[searchStart:streamIdx]
	open := bytes.LastIndex(window, []byte("<<"))
	close := bytes.LastIndex(window, []byte(">>"))
	if open < 0 || close < 0 || close < open {
		return ""
	}
	return string(window[open : close+2])
}

func decodePDFStream(raw []byte, dict string) string {
	if strings.Contains(dict, "/FlateDecode") {
		if decoded, err := readPDFFlateStream(raw); err == nil {
			return string(decoded)
		}
	}
	return string(raw)
}

func readPDFFlateStream(raw []byte) ([]byte, error) {
	reader, err := zlib.NewReader(bytes.NewReader(raw))
	if err == nil {
		defer reader.Close()
		return io.ReadAll(reader)
	}

	reader = flate.NewReader(bytes.NewReader(raw))
	defer reader.Close()
	return io.ReadAll(reader)
}

func extractPDFTextFromStream(stream string) string {
	var out strings.Builder
	inText := false
	needsSpace := false
	for i := 0; i < len(stream); {
		switch {
		case hasPDFOperator(stream, i, "BT"):
			inText = true
			i += 2
		case hasPDFOperator(stream, i, "ET"):
			inText = false
			appendPDFNewline(&out)
			i += 2
		case !inText:
			i++
		case stream[i] == '(':
			text, next := consumePDFLiteralString(stream, i)
			appendPDFText(&out, text, &needsSpace)
			i = next
		case stream[i] == '<' && (i+1 >= len(stream) || stream[i+1] != '<'):
			text, next := consumePDFHexString(stream, i)
			appendPDFText(&out, text, &needsSpace)
			i = next
		case hasPDFOperator(stream, i, "Tj"), hasPDFOperator(stream, i, "TJ"), hasPDFOperator(stream, i, "'"), hasPDFOperator(stream, i, `"`):
			appendPDFNewline(&out)
			needsSpace = false
			switch {
			case hasPDFOperator(stream, i, "TJ"):
				i += 2
			case hasPDFOperator(stream, i, "Tj"):
				i += 2
			default:
				i++
			}
		case hasPDFOperator(stream, i, "Td"), hasPDFOperator(stream, i, "TD"), hasPDFOperator(stream, i, "Tm"), hasPDFOperator(stream, i, "T*"):
			appendPDFNewline(&out)
			needsSpace = false
			i += 2
		default:
			i++
		}
	}
	return normalizePDFExtractedText(out.String())
}

func hasPDFOperator(stream string, idx int, op string) bool {
	if idx < 0 || idx+len(op) > len(stream) || stream[idx:idx+len(op)] != op {
		return false
	}
	if idx > 0 {
		r := rune(stream[idx-1])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	if idx+len(op) < len(stream) {
		r := rune(stream[idx+len(op)])
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func consumePDFLiteralString(stream string, start int) (string, int) {
	var out strings.Builder
	depth := 0
	for i := start; i < len(stream); i++ {
		ch := stream[i]
		if ch == '(' {
			if depth > 0 {
				out.WriteByte(ch)
			}
			depth++
			continue
		}
		if ch == ')' {
			depth--
			if depth == 0 {
				return out.String(), i + 1
			}
			out.WriteByte(ch)
			continue
		}
		if ch == '\\' && i+1 < len(stream) {
			i++
			switch stream[i] {
			case 'n':
				out.WriteByte('\n')
			case 'r':
				out.WriteByte('\r')
			case 't':
				out.WriteByte('\t')
			case 'b':
				out.WriteByte('\b')
			case 'f':
				out.WriteByte('\f')
			case '(', ')', '\\':
				out.WriteByte(stream[i])
			case '\n':
			case '\r':
				if i+1 < len(stream) && stream[i+1] == '\n' {
					i++
				}
			default:
				if stream[i] >= '0' && stream[i] <= '7' {
					value := int(stream[i] - '0')
					for count := 1; count < 3 && i+1 < len(stream) && stream[i+1] >= '0' && stream[i+1] <= '7'; count++ {
						i++
						value = value*8 + int(stream[i]-'0')
					}
					out.WriteByte(byte(value))
				} else {
					out.WriteByte(stream[i])
				}
			}
			continue
		}
		if depth > 0 {
			out.WriteByte(ch)
		}
	}
	return out.String(), len(stream)
}

func consumePDFHexString(stream string, start int) (string, int) {
	end := start + 1
	for end < len(stream) && stream[end] != '>' {
		end++
	}
	if end >= len(stream) {
		return "", len(stream)
	}
	hex := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, stream[start+1:end])
	if len(hex)%2 == 1 {
		hex += "0"
	}
	var out strings.Builder
	for i := 0; i+1 < len(hex); i += 2 {
		value, err := strconv.ParseUint(hex[i:i+2], 16, 8)
		if err != nil {
			continue
		}
		out.WriteByte(byte(value))
	}
	return out.String(), end + 1
}

func appendPDFText(out *strings.Builder, text string, needsSpace *bool) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if out.Len() > 0 && *needsSpace {
		last, _ := utf8.DecodeLastRuneInString(out.String())
		if last != '\n' {
			out.WriteByte(' ')
		}
	}
	out.WriteString(text)
	*needsSpace = true
}

func appendPDFNewline(out *strings.Builder) {
	if out.Len() == 0 {
		return
	}
	last, _ := utf8.DecodeLastRuneInString(out.String())
	if last == '\n' {
		return
	}
	out.WriteByte('\n')
}

func normalizePDFExtractedText(text string) string {
	lines := strings.Split(text, "\n")
	cleaned := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
		if line == "" {
			continue
		}
		cleaned = append(cleaned, line)
	}
	return strings.Join(cleaned, "\n")
}

var legacyOfficeNoise = map[string]struct{}{
	"root entry":                 {},
	"worddocument":               {},
	"powerpoint document":        {},
	"workbook":                   {},
	"book":                       {},
	"summaryinformation":         {},
	"documentsummaryinformation": {},
	"compobj":                    {},
	"objectpool":                 {},
	"1table":                     {},
	"0table":                     {},
}

func extractLegacyOfficeText(data []byte) string {
	candidates := append(extractUTF16LEStrings(data, 4), extractASCIIStrings(data, 6)...)
	if len(candidates) == 0 {
		return ""
	}

	seen := make(map[string]struct{}, len(candidates))
	lines := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		line := normalizeLegacyOfficeLine(candidate)
		if line == "" {
			continue
		}
		key := strings.ToLower(line)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func extractASCIIStrings(data []byte, minLen int) []string {
	var out []string
	var buf []byte
	flush := func() {
		if len(buf) >= minLen {
			out = append(out, string(buf))
		}
		buf = buf[:0]
	}

	for _, b := range data {
		if isASCIITextByte(b) {
			buf = append(buf, b)
			continue
		}
		flush()
	}
	flush()
	return out
}

func extractUTF16LEStrings(data []byte, minRunes int) []string {
	var out []string
	for start := 0; start <= 1; start++ {
		buf := make([]uint16, 0, 32)
		flush := func() {
			if len(buf) >= minRunes {
				out = append(out, string(utf16.Decode(buf)))
			}
			buf = buf[:0]
		}

		for i := start; i+1 < len(data); i += 2 {
			value := uint16(data[i]) | uint16(data[i+1])<<8
			if isLikelyUTF16TextRune(rune(value)) {
				buf = append(buf, value)
				continue
			}
			flush()
		}
		flush()
	}
	return out
}

func isASCIITextByte(b byte) bool {
	return b == '\t' || b == '\n' || b == '\r' || (b >= 32 && b <= 126)
}

func isLikelyUTF16TextRune(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return true
	}
	if r < 32 || r == utf8.RuneError {
		return false
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) || unicode.IsPunct(r) || unicode.IsSymbol(r)
}

func normalizeLegacyOfficeLine(line string) string {
	line = strings.Map(func(r rune) rune {
		switch {
		case r == '\u0000':
			return -1
		case unicode.IsControl(r) && r != '\n' && r != '\t' && r != '\r':
			return -1
		default:
			return r
		}
	}, line)
	line = strings.Join(strings.Fields(strings.TrimSpace(line)), " ")
	if utf8.RuneCountInString(line) < 4 {
		return ""
	}

	lower := strings.ToLower(line)
	if _, ok := legacyOfficeNoise[lower]; ok {
		return ""
	}

	var alphaNum int
	var weird int
	for _, r := range line {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			alphaNum++
		case unicode.IsSpace(r):
		case unicode.IsPunct(r):
			if !strings.ContainsRune(".,:;!?()[]{}<>-_/#%&+*'\"@", r) {
				weird++
			}
		default:
			weird++
		}
	}
	if alphaNum == 0 {
		return ""
	}
	if weird > alphaNum {
		return ""
	}
	return line
}
