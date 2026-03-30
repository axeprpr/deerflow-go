package langgraphcompat

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
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
