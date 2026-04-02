package langgraphcompat

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/csv"
	"encoding/json"
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
	".pdf":   {},
	".ppt":   {},
	".pptx":  {},
	".xls":   {},
	".xlsx":  {},
	".doc":   {},
	".docx":  {},
	".csv":   {},
	".tsv":   {},
	".json":  {},
	".txt":   {},
	".log":   {},
	".ini":   {},
	".cfg":   {},
	".conf":  {},
	".env":   {},
	".toml":  {},
	".xml":   {},
	".html":  {},
	".htm":   {},
	".xhtml": {},
	".yaml":  {},
	".yml":   {},
}

const (
	maxStructuredPreviewRows = 40
	maxStructuredPreviewCols = 8
)

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
	case ".csv":
		return convertDelimitedTextToMarkdown(path, ',', "CSV")
	case ".tsv":
		return convertDelimitedTextToMarkdown(path, '\t', "TSV")
	case ".json":
		return convertJSONToMarkdown(path)
	case ".txt", ".log":
		return convertPlainTextToMarkdown(path, "")
	case ".ini", ".cfg", ".conf":
		return convertPlainTextToMarkdown(path, "ini")
	case ".env":
		return convertPlainTextToMarkdown(path, "dotenv")
	case ".toml":
		return convertPlainTextToMarkdown(path, "toml")
	case ".xml":
		return convertPlainTextToMarkdown(path, "xml")
	case ".html", ".htm", ".xhtml":
		return convertPlainTextToMarkdown(path, "html")
	case ".yaml", ".yml":
		return convertYAMLToMarkdown(path)
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

func convertDelimitedTextToMarkdown(path string, comma rune, label string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	reader := csv.NewReader(strings.NewReader(string(data)))
	reader.Comma = comma
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true

	rows, err := reader.ReadAll()
	if err != nil {
		return "", err
	}
	rows = compactTableRows(rows)
	if len(rows) == 0 {
		return "", nil
	}

	trimmed, note := limitMarkdownTable(rows, maxStructuredPreviewRows, maxStructuredPreviewCols)
	sections := []string{
		"# " + filepath.Base(path),
		"",
		fmt.Sprintf("Detected %s content.", label),
		"",
		renderMarkdownTable(trimmed),
	}
	if note != "" {
		sections = append(sections, "", note)
	}
	return strings.Join(sections, "\n") + "\n", nil
}

func convertJSONToMarkdown(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return "", err
	}

	sections := []string{"# " + filepath.Base(path), ""}
	if rows := jsonArrayTable(decoded); len(rows) > 0 {
		trimmed, note := limitMarkdownTable(rows, maxStructuredPreviewRows, maxStructuredPreviewCols)
		sections = append(sections, "Structured JSON preview.", "", renderMarkdownTable(trimmed))
		if note != "" {
			sections = append(sections, "", note)
		}
		return strings.Join(sections, "\n") + "\n", nil
	}

	pretty, err := json.MarshalIndent(decoded, "", "  ")
	if err != nil {
		return "", err
	}
	sections = append(sections, "```json", string(pretty), "```")
	return strings.Join(sections, "\n") + "\n", nil
}

func convertYAMLToMarkdown(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return "", nil
	}
	return strings.Join([]string{
		"# " + filepath.Base(path),
		"",
		"```yaml",
		text,
		"```",
	}, "\n") + "\n", nil
}

func convertPlainTextToMarkdown(path string, language string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return "", nil
	}

	sections := []string{"# " + filepath.Base(path), ""}
	if language == "" {
		sections = append(sections, text)
		return strings.Join(sections, "\n") + "\n", nil
	}
	sections = append(sections, "```"+language, text, "```")
	return strings.Join(sections, "\n") + "\n", nil
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

	text, err := extractDOCXText(file)
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

	sheets, err := readWorkbookSheets(&rc.Reader)
	if err != nil {
		return "", err
	}
	if len(sheets) == 0 {
		return "", fmt.Errorf("no worksheet xml found")
	}

	var sections []string
	for idx, sheet := range sheets {
		file := zipEntry(&rc.Reader, sheet.Path)
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
		title := strings.TrimSpace(sheet.Name)
		if title == "" {
			title = fmt.Sprintf("Sheet %d", idx+1)
		}
		trimmed, note := limitMarkdownTable(rows, maxStructuredPreviewRows, maxStructuredPreviewCols)
		lines := []string{"## " + title, "", renderMarkdownTable(trimmed)}
		if note != "" {
			lines = append(lines, "", note)
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

func extractDOCXText(file *zip.File) (string, error) {
	r, err := file.Open()
	if err != nil {
		return "", err
	}
	defer r.Close()

	decoder := xml.NewDecoder(r)
	var sections []string
	var currentParagraph strings.Builder
	var currentCellParagraphs []string
	var currentRow []string
	var currentTable [][]string
	var currentParagraphStyle string
	currentParagraphIsList := false
	insideParagraph := false
	insideTable := false
	insideCell := false

	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return "", err
		}
		switch tok := token.(type) {
		case xml.StartElement:
			switch tok.Name.Local {
			case "tbl":
				insideTable = true
				currentTable = nil
			case "tr":
				if insideTable {
					currentRow = nil
				}
			case "tc":
				if insideTable {
					insideCell = true
					currentCellParagraphs = nil
				}
			case "p":
				insideParagraph = true
				currentParagraph.Reset()
				currentParagraphStyle = ""
				currentParagraphIsList = false
			case "pStyle":
				if insideParagraph {
					currentParagraphStyle = attrValue(tok.Attr, "val")
				}
			case "numPr":
				if insideParagraph {
					currentParagraphIsList = true
				}
			case "tab":
				if insideParagraph {
					currentParagraph.WriteByte('\t')
				}
			case "br", "cr":
				if insideParagraph {
					currentParagraph.WriteByte('\n')
				}
			}
		case xml.EndElement:
			switch tok.Name.Local {
			case "p":
				if !insideParagraph {
					continue
				}
				insideParagraph = false
				paragraph := formatDOCXParagraph(
					normalizeDOCXBlock(currentParagraph.String()),
					currentParagraphStyle,
					currentParagraphIsList,
				)
				if paragraph == "" {
					continue
				}
				if insideCell {
					currentCellParagraphs = append(currentCellParagraphs, paragraph)
					continue
				}
				sections = append(sections, paragraph)
			case "tc":
				if !insideTable || !insideCell {
					continue
				}
				insideCell = false
				currentRow = append(currentRow, strings.Join(currentCellParagraphs, "\n\n"))
			case "tr":
				if insideTable && len(currentRow) > 0 {
					currentTable = append(currentTable, trimTrailingEmpty(currentRow))
				}
			case "tbl":
				if !insideTable {
					continue
				}
				insideTable = false
				if len(currentTable) > 0 {
					sections = append(sections, renderMarkdownTable(currentTable))
				}
			}
		case xml.CharData:
			if insideParagraph {
				currentParagraph.WriteString(string(tok))
			}
		}
	}

	return strings.Join(compactSections(sections), "\n\n"), nil
}

func normalizeDOCXBlock(text string) string {
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

func formatDOCXParagraph(text string, style string, isList bool) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}

	if level, ok := docxHeadingLevel(style); ok {
		return strings.Repeat("#", level) + " " + text
	}

	if isList {
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			lines[i] = "- " + line
		}
		return strings.Join(lines, "\n")
	}

	return text
}

func docxHeadingLevel(style string) (int, bool) {
	style = strings.TrimSpace(style)
	if style == "" {
		return 0, false
	}

	matches := regexp.MustCompile(`(?i)^heading([1-6])$`).FindStringSubmatch(style)
	if len(matches) == 2 {
		level, err := strconv.Atoi(matches[1])
		if err == nil && level >= 1 && level <= 6 {
			return level, true
		}
	}
	switch strings.ToLower(style) {
	case "title":
		return 1, true
	case "subtitle":
		return 2, true
	default:
		return 0, false
	}
}

func attrValue(attrs []xml.Attr, local string) string {
	for _, attr := range attrs {
		if attr.Name.Local == local {
			return strings.TrimSpace(attr.Value)
		}
	}
	return ""
}

func compactSections(sections []string) []string {
	out := make([]string, 0, len(sections))
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section == "" {
			continue
		}
		out = append(out, section)
	}
	return out
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

type workbookSheet struct {
	Name string
	Path string
}

type workbookSheetMeta struct {
	Name string
	Rel  string
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

func renderMarkdownTable(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	width := 0
	for _, row := range rows {
		if len(row) > width {
			width = len(row)
		}
	}
	if width == 0 {
		return ""
	}

	normalized := make([][]string, 0, len(rows))
	for _, row := range rows {
		current := make([]string, width)
		for i := 0; i < width; i++ {
			if i < len(row) {
				current[i] = escapeMarkdownTableCell(row[i])
			}
		}
		normalized = append(normalized, current)
	}

	lines := make([]string, 0, len(normalized)+1)
	lines = append(lines, "| "+strings.Join(normalized[0], " | ")+" |")
	separators := make([]string, width)
	for i := range separators {
		separators[i] = "---"
	}
	lines = append(lines, "| "+strings.Join(separators, " | ")+" |")
	for _, row := range normalized[1:] {
		lines = append(lines, "| "+strings.Join(row, " | ")+" |")
	}
	return strings.Join(lines, "\n")
}

func compactTableRows(rows [][]string) [][]string {
	out := make([][]string, 0, len(rows))
	for _, row := range rows {
		trimmed := trimTrailingEmpty(row)
		nonEmpty := false
		for i := range trimmed {
			trimmed[i] = strings.TrimSpace(trimmed[i])
			if trimmed[i] != "" {
				nonEmpty = true
			}
		}
		if !nonEmpty {
			continue
		}
		out = append(out, trimmed)
	}
	return out
}

func limitMarkdownTable(rows [][]string, maxRows int, maxCols int) ([][]string, string) {
	if len(rows) == 0 {
		return nil, ""
	}

	originalRows := len(rows)
	originalCols := 0
	for _, row := range rows {
		if len(row) > originalCols {
			originalCols = len(row)
		}
	}

	if maxRows > 0 && len(rows) > maxRows {
		rows = rows[:maxRows]
	}
	if maxCols > 0 {
		for i := range rows {
			if len(rows[i]) > maxCols {
				rows[i] = rows[i][:maxCols]
			}
		}
	}

	if originalRows == len(rows) && originalCols <= maxCols {
		return rows, ""
	}

	note := fmt.Sprintf("_Preview truncated to %d rows and %d columns", len(rows), min(originalCols, maxCols))
	switch {
	case originalRows > len(rows) && originalCols > maxCols:
		note += fmt.Sprintf(" from %d rows and %d columns._", originalRows, originalCols)
	case originalRows > len(rows):
		note += fmt.Sprintf(" from %d rows._", originalRows)
	default:
		note += fmt.Sprintf(" from %d columns._", originalCols)
	}
	return rows, note
}

func jsonArrayTable(value any) [][]string {
	items, ok := value.([]any)
	if !ok || len(items) == 0 {
		return nil
	}

	keySet := map[string]struct{}{}
	rows := make([]map[string]string, 0, len(items))
	for _, item := range items {
		obj, ok := item.(map[string]any)
		if !ok {
			return nil
		}
		row := make(map[string]string, len(obj))
		for key, raw := range obj {
			text, ok := jsonScalarString(raw)
			if !ok {
				return nil
			}
			row[key] = text
			keySet[key] = struct{}{}
		}
		rows = append(rows, row)
	}
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	if len(keys) == 0 {
		return nil
	}

	table := make([][]string, 0, len(rows)+1)
	table = append(table, keys)
	for _, row := range rows {
		current := make([]string, len(keys))
		for i, key := range keys {
			current[i] = row[key]
		}
		table = append(table, current)
	}
	return table
}

func jsonScalarString(value any) (string, bool) {
	switch v := value.(type) {
	case nil:
		return "", true
	case string:
		return v, true
	case bool:
		if v {
			return "true", true
		}
		return "false", true
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), true
	default:
		return "", false
	}
}

func escapeMarkdownTableCell(value string) string {
	value = strings.TrimSpace(value)
	value = strings.ReplaceAll(value, "|", `\|`)
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	value = strings.ReplaceAll(value, "\n", "<br>")
	return value
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

func readWorkbookSheets(rc *zip.Reader) ([]workbookSheet, error) {
	sheetMetas, err := readWorkbookSheetMetadata(rc)
	if err != nil {
		return nil, err
	}
	relTargets, err := readWorkbookRelationships(rc)
	if err != nil {
		return nil, err
	}

	sheets := make([]workbookSheet, 0, len(sheetMetas))
	for _, meta := range sheetMetas {
		path := relTargets[meta.Rel]
		if path == "" {
			continue
		}
		sheets = append(sheets, workbookSheet{
			Name: meta.Name,
			Path: path,
		})
	}
	if len(sheets) > 0 {
		return sheets, nil
	}

	var fallback []workbookSheet
	for _, f := range rc.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			fallback = append(fallback, workbookSheet{
				Name: strings.TrimSuffix(filepath.Base(f.Name), filepath.Ext(f.Name)),
				Path: f.Name,
			})
		}
	}
	sort.Slice(fallback, func(i, j int) bool { return naturalLess(fallback[i].Path, fallback[j].Path) })
	return fallback, nil
}

func readWorkbookSheetMetadata(rc *zip.Reader) ([]workbookSheetMeta, error) {
	file := zipEntry(rc, "xl/workbook.xml")
	if file == nil {
		return nil, nil
	}
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()

	decoder := xml.NewDecoder(r)
	var sheets []workbookSheetMeta
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "sheet" {
			continue
		}
		var meta workbookSheetMeta
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "name":
				meta.Name = attr.Value
			case "id":
				meta.Rel = attr.Value
			}
		}
		if meta.Rel != "" {
			sheets = append(sheets, meta)
		}
	}
	return sheets, nil
}

func readWorkbookRelationships(rc *zip.Reader) (map[string]string, error) {
	file := zipEntry(rc, "xl/_rels/workbook.xml.rels")
	if file == nil {
		return nil, nil
	}
	r, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()

	decoder := xml.NewDecoder(r)
	targets := map[string]string{}
	for {
		token, err := decoder.Token()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		var id string
		var target string
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "Id":
				id = attr.Value
			case "Target":
				target = attr.Value
			}
		}
		if id == "" || target == "" {
			continue
		}
		target = strings.TrimPrefix(filepath.ToSlash(filepath.Clean("xl/"+target)), "./")
		targets[id] = target
	}
	return targets, nil
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
		case stream[i] == '[':
			text, next := consumePDFArrayText(stream, i)
			appendPDFText(&out, text, &needsSpace)
			i = next
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

func consumePDFArrayText(stream string, start int) (string, int) {
	if start >= len(stream) || stream[start] != '[' {
		return "", start
	}
	var parts []string
	depth := 1
	for i := start + 1; i < len(stream); {
		switch stream[i] {
		case '[':
			depth++
			i++
		case ']':
			depth--
			i++
			if depth == 0 {
				return strings.Join(parts, ""), i
			}
		case '(':
			text, next := consumePDFLiteralString(stream, i)
			if text != "" {
				parts = append(parts, text)
			}
			i = next
		case '<':
			if i+1 < len(stream) && stream[i+1] == '<' {
				i += 2
				continue
			}
			text, next := consumePDFHexString(stream, i)
			if text != "" {
				parts = append(parts, text)
			}
			i = next
		default:
			i++
		}
	}
	return strings.Join(parts, ""), len(stream)
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
	data, next := consumePDFLiteralBytes(stream, start)
	return decodePDFStringBytes(data), next
}

func consumePDFLiteralBytes(stream string, start int) ([]byte, int) {
	var out bytes.Buffer
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
				return out.Bytes(), i + 1
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
	return out.Bytes(), len(stream)
}

func consumePDFHexString(stream string, start int) (string, int) {
	data, next := consumePDFHexBytes(stream, start)
	return decodePDFStringBytes(data), next
}

func consumePDFHexBytes(stream string, start int) ([]byte, int) {
	end := start + 1
	for end < len(stream) && stream[end] != '>' {
		end++
	}
	if end >= len(stream) {
		return nil, len(stream)
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
	var out bytes.Buffer
	for i := 0; i+1 < len(hex); i += 2 {
		value, err := strconv.ParseUint(hex[i:i+2], 16, 8)
		if err != nil {
			continue
		}
		out.WriteByte(byte(value))
	}
	return out.Bytes(), end + 1
}

func decodePDFStringBytes(data []byte) string {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return ""
	}
	if decoded, ok := decodeUTF16PDFString(data); ok {
		return decoded
	}
	if utf8.Valid(data) {
		return string(data)
	}
	return strings.ToValidUTF8(string(data), "")
}

func decodeUTF16PDFString(data []byte) (string, bool) {
	if len(data) < 2 {
		return "", false
	}
	switch {
	case len(data)%2 == 0 && data[0] == 0xFE && data[1] == 0xFF:
		return decodeUTF16Words(data[2:], true), true
	case len(data)%2 == 0 && data[0] == 0xFF && data[1] == 0xFE:
		return decodeUTF16Words(data[2:], false), true
	case looksLikeUTF16BE(data):
		return decodeUTF16Words(data, true), true
	case looksLikeUTF16LE(data):
		return decodeUTF16Words(data, false), true
	default:
		return "", false
	}
}

func looksLikeUTF16BE(data []byte) bool {
	if len(data) < 4 || len(data)%2 != 0 {
		return false
	}
	var zeroHigh int
	for i := 0; i+1 < len(data); i += 2 {
		if data[i] == 0 && data[i+1] != 0 {
			zeroHigh++
		}
	}
	return zeroHigh*2 >= len(data)/2
}

func looksLikeUTF16LE(data []byte) bool {
	if len(data) < 4 || len(data)%2 != 0 {
		return false
	}
	var zeroLow int
	for i := 0; i+1 < len(data); i += 2 {
		if data[i] != 0 && data[i+1] == 0 {
			zeroLow++
		}
	}
	return zeroLow*2 >= len(data)/2
}

func decodeUTF16Words(data []byte, bigEndian bool) string {
	if len(data)%2 != 0 {
		data = data[:len(data)-1]
	}
	words := make([]uint16, 0, len(data)/2)
	for i := 0; i+1 < len(data); i += 2 {
		if bigEndian {
			words = append(words, uint16(data[i])<<8|uint16(data[i+1]))
			continue
		}
		words = append(words, uint16(data[i])|uint16(data[i+1])<<8)
	}
	return string(utf16.Decode(words))
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
