package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/models"
	"github.com/axeprpr/deerflow-go/pkg/tools"
)

const (
	defaultReadFileOutputMaxChars = 50000
	defaultGlobMaxResults         = 200
	defaultGrepMaxResults         = 100
)

func ReadFileHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	if err := tools.EnsureThreadDataDirs(ctx); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("prepare thread directories failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	args := call.Arguments
	path, ok := args["path"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is required; use an absolute virtual path such as /mnt/user-data/uploads/input.txt or /mnt/user-data/workspace/draft.txt")
	}
	path = resolveReadableFilePath(ctx, path)

	data, err := os.ReadFile(path)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("read failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}

	if startLine, endLine, ok := resolveLineRange(args); ok {
		data = []byte(sliceContentLines(string(data), startLine, endLine))
	}
	if limit, ok := args["limit"].(float64); ok && limit > 0 && int(limit) < len(data) {
		data = data[:int(limit)]
	}

	content := truncateReadFileOutput(string(data), defaultReadFileOutputMaxChars)

	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: content}, nil
}

func resolveLineRange(args map[string]any) (int, int, bool) {
	start, startOK := intArg(args["start_line"])
	end, endOK := intArg(args["end_line"])
	if !startOK && !endOK {
		return 0, 0, false
	}
	if !startOK {
		start = 1
	}
	if !endOK {
		end = int(^uint(0) >> 1)
	}
	if start < 1 {
		start = 1
	}
	if end < start {
		end = start
	}
	return start, end, true
}

func intArg(raw any) (int, bool) {
	switch value := raw.(type) {
	case int:
		return value, true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case float64:
		return int(value), true
	case json.Number:
		parsed, err := strconv.Atoi(value.String())
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func intValueFromArgs(raw any, fallback int) int {
	if value, ok := intArg(raw); ok {
		return value
	}
	return fallback
}

func sliceContentLines(content string, startLine, endLine int) string {
	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if startLine > len(lines) {
		return ""
	}
	startIdx := startLine - 1
	endIdx := endLine
	if endIdx > len(lines) {
		endIdx = len(lines)
	}
	if startIdx < 0 {
		startIdx = 0
	}
	if endIdx < startIdx {
		endIdx = startIdx
	}
	return strings.Join(lines[startIdx:endIdx], "\n")
}

func resolveReadableFilePath(ctx context.Context, path string) string {
	path = strings.TrimSpace(path)
	resolved := tools.ResolveVirtualPath(ctx, path)
	if !shouldPreferMarkdownCompanion(path) {
		return resolved
	}

	companion := strings.TrimSuffix(resolved, filepath.Ext(resolved)) + ".md"
	info, err := os.Stat(companion)
	if err != nil || !info.Mode().IsRegular() {
		return resolved
	}
	return companion
}

func shouldPreferMarkdownCompanion(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if !strings.HasPrefix(path, "/mnt/user-data/uploads/") {
		return false
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf", ".ppt", ".pptx", ".xls", ".xlsx", ".doc", ".docx", ".csv", ".tsv", ".json", ".yaml", ".yml":
		return true
	default:
		return false
	}
}

func compileGlobMatcher(pattern string) (func(string) bool, error) {
	pattern = path.Clean(strings.TrimSpace(filepath.ToSlash(pattern)))
	if pattern == "." || pattern == "" {
		pattern = "*"
	}
	re, err := regexp.Compile("^" + globToRegexp(pattern) + "$")
	if err != nil {
		return nil, err
	}
	return func(candidate string) bool {
		candidate = filepath.ToSlash(strings.TrimSpace(candidate))
		return re.MatchString(candidate)
	}, nil
}

func globToRegexp(pattern string) string {
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		switch pattern[i] {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				if i+2 < len(pattern) && pattern[i+2] == '/' {
					b.WriteString(`(?:.*/)?`)
					i += 2
				} else {
					b.WriteString(".*")
					i++
				}
			} else {
				b.WriteString(`[^/]*`)
			}
		case '?':
			b.WriteString(`[^/]`)
		default:
			b.WriteString(regexp.QuoteMeta(string(pattern[i])))
		}
	}
	return b.String()
}

func joinVirtualPath(root, rel string) string {
	root = strings.TrimRight(strings.TrimSpace(root), "/")
	rel = filepath.ToSlash(strings.TrimSpace(rel))
	if rel == "" || rel == "." {
		return root
	}
	if root == "" {
		return "/" + strings.TrimLeft(rel, "/")
	}
	return root + "/" + strings.TrimLeft(rel, "/")
}

func formatGlobResults(rootPath string, matches []string, truncated bool) string {
	if len(matches) == 0 {
		return fmt.Sprintf("No files matched under %s", rootPath)
	}
	lines := []string{fmt.Sprintf("Found %d paths under %s", len(matches), rootPath)}
	if truncated {
		lines[0] += fmt.Sprintf(" (showing first %d)", len(matches))
	}
	for idx, match := range matches {
		lines = append(lines, fmt.Sprintf("%d. %s", idx+1, match))
	}
	if truncated {
		lines = append(lines, "Results truncated. Narrow the path or pattern to see fewer matches.")
	}
	return strings.Join(lines, "\n")
}

func formatGrepResults(rootPath string, matches []string, truncated bool) string {
	if len(matches) == 0 {
		return fmt.Sprintf("No matches found under %s", rootPath)
	}
	lines := []string{fmt.Sprintf("Found %d matches under %s", len(matches), rootPath)}
	if truncated {
		lines[0] += fmt.Sprintf(" (showing first %d)", len(matches))
	}
	lines = append(lines, matches...)
	if truncated {
		lines = append(lines, "Results truncated. Narrow the path or add a glob filter.")
	}
	return strings.Join(lines, "\n")
}

func truncateReadFileOutput(output string, maxChars int) string {
	if maxChars == 0 {
		return output
	}
	runes := []rune(output)
	if len(runes) <= maxChars {
		return output
	}
	total := len(runes)
	markerMaxLen := len([]rune(fmt.Sprintf(
		"\n... [truncated: showing first %d of %d chars. Use start_line/end_line to read a specific range] ...",
		total,
		total,
	)))
	kept := max(0, maxChars-markerMaxLen)
	if kept == 0 {
		return string(runes[:maxChars])
	}
	marker := fmt.Sprintf(
		"\n... [truncated: showing first %d of %d chars. Use start_line/end_line to read a specific range] ...",
		kept,
		total,
	)
	return string(runes[:kept]) + marker
}

func grepLineMatches(line string, re *regexp.Regexp, literalNeed string, literal bool, caseSensitive bool) bool {
	if literal {
		if !caseSensitive {
			line = strings.ToLower(line)
		}
		return strings.Contains(line, literalNeed)
	}
	return re != nil && re.MatchString(line)
}

type dirEntryFromFileInfo struct {
	info fs.FileInfo
}

func (d dirEntryFromFileInfo) Name() string               { return d.info.Name() }
func (d dirEntryFromFileInfo) IsDir() bool                { return d.info.IsDir() }
func (d dirEntryFromFileInfo) Type() fs.FileMode          { return d.info.Mode().Type() }
func (d dirEntryFromFileInfo) Info() (fs.FileInfo, error) { return d.info, nil }

func WriteFileHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	if err := tools.EnsureThreadDataDirs(ctx); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("prepare thread directories failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	args := call.Arguments
	requestedPath, ok := args["path"].(string)
	if !ok || strings.TrimSpace(requestedPath) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is required; use an absolute virtual path such as /mnt/user-data/workspace/draft.txt or /mnt/user-data/outputs/index.html")
	}
	path := tools.ResolveVirtualPath(ctx, requestedPath)
	if err := tools.ValidateWritableToolPath(ctx, requestedPath, path); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, err
	}
	content, ok := args["content"].(string)
	if !ok {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("content is required")
	}
	appendMode, _ := args["append"].(bool)

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("mkdir failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	if appendMode {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("write failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
		}
		defer f.Close()
		if _, err := f.WriteString(content); err != nil {
			return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("write failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
		}
	} else if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("write failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}

	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: "OK"}, nil
}

func GlobHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	if err := tools.EnsureThreadDataDirs(ctx); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("prepare thread directories failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	args := call.Arguments
	pattern, ok := args["pattern"].(string)
	if !ok || strings.TrimSpace(pattern) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("pattern is required")
	}
	requestedPath, hasRoot := args["path"].(string)
	if !hasRoot || strings.TrimSpace(requestedPath) == "" {
		legacyPattern := tools.ResolveVirtualPath(ctx, pattern)
		matches, err := filepath.Glob(legacyPattern)
		if err != nil {
			return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("glob failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
		}
		for i := range matches {
			matches[i] = tools.MaskLocalPaths(ctx, matches[i])
		}
		data, _ := json.Marshal(matches)
		return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: string(data)}, nil
	}

	resolvedRoot := tools.ResolveVirtualPath(ctx, requestedPath)
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("glob failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	if !info.IsDir() {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is not a directory")
	}

	includeDirs, _ := args["include_dirs"].(bool)
	maxResults := intValueFromArgs(args["max_results"], defaultGlobMaxResults)
	if maxResults <= 0 {
		maxResults = defaultGlobMaxResults
	}
	matcher, err := compileGlobMatcher(pattern)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("glob failed: %v", err)
	}

	matches := make([]string, 0, min(maxResults, 16))
	truncated := false
	err = filepath.WalkDir(resolvedRoot, func(candidate string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if candidate == resolvedRoot {
			return nil
		}
		rel, err := filepath.Rel(resolvedRoot, candidate)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			if includeDirs && matcher(rel) {
				if len(matches) >= maxResults {
					truncated = true
					return fs.SkipAll
				}
				matches = append(matches, joinVirtualPath(requestedPath, rel))
			}
			return nil
		}
		if !matcher(rel) {
			return nil
		}
		if len(matches) >= maxResults {
			truncated = true
			return fs.SkipAll
		}
		matches = append(matches, joinVirtualPath(requestedPath, rel))
		return nil
	})
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("glob failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	return models.ToolResult{
		CallID:   call.ID,
		ToolName: call.Name,
		Content:  formatGlobResults(requestedPath, matches, truncated),
	}, nil
}

func GrepHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	if err := tools.EnsureThreadDataDirs(ctx); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("prepare thread directories failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	args := call.Arguments
	pattern, ok := args["pattern"].(string)
	if !ok || strings.TrimSpace(pattern) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("pattern is required")
	}
	requestedPath, ok := args["path"].(string)
	if !ok || strings.TrimSpace(requestedPath) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is required")
	}

	maxResults := intValueFromArgs(args["max_results"], defaultGrepMaxResults)
	if maxResults <= 0 {
		maxResults = defaultGrepMaxResults
	}
	globFilter, _ := args["glob"].(string)
	literal, _ := args["literal"].(bool)
	caseSensitive, _ := args["case_sensitive"].(bool)

	var (
		re          *regexp.Regexp
		literalNeed string
	)
	if literal {
		literalNeed = pattern
		if !caseSensitive {
			literalNeed = strings.ToLower(literalNeed)
		}
	} else {
		compiledPattern := pattern
		if !caseSensitive {
			compiledPattern = "(?i)" + compiledPattern
		}
		var err error
		re, err = regexp.Compile(compiledPattern)
		if err != nil {
			return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("grep failed: %v", err)
		}
	}

	var filter func(string) bool
	if strings.TrimSpace(globFilter) != "" {
		var err error
		filter, err = compileGlobMatcher(globFilter)
		if err != nil {
			return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("grep failed: %v", err)
		}
	}

	resolvedRoot := tools.ResolveVirtualPath(ctx, requestedPath)
	info, err := os.Stat(resolvedRoot)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("grep failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}

	matches := make([]string, 0, min(maxResults, 16))
	truncated := false
	searchFile := func(candidate string, rel string) error {
		if filter != nil && !filter(rel) {
			return nil
		}
		data, err := os.ReadFile(candidate)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for idx, line := range lines {
			if !grepLineMatches(line, re, literalNeed, literal, caseSensitive) {
				continue
			}
			virtualPath := requestedPath
			if info.IsDir() {
				virtualPath = joinVirtualPath(requestedPath, rel)
			}
			matches = append(matches, fmt.Sprintf("%s:%d: %s", virtualPath, idx+1, line))
			if len(matches) >= maxResults {
				truncated = true
				return fs.SkipAll
			}
		}
		return nil
	}

	if info.IsDir() {
		err = filepath.WalkDir(resolvedRoot, func(candidate string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(resolvedRoot, candidate)
			if err != nil {
				return err
			}
			return searchFile(candidate, filepath.ToSlash(rel))
		})
	} else {
		err = searchFile(resolvedRoot, filepath.Base(requestedPath))
	}
	if err != nil && err != fs.SkipAll {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("grep failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}

	return models.ToolResult{
		CallID:   call.ID,
		ToolName: call.Name,
		Content:  formatGrepResults(requestedPath, matches, truncated),
	}, nil
}

func LsHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	if err := tools.EnsureThreadDataDirs(ctx); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("prepare thread directories failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	args := call.Arguments
	path, ok := args["path"].(string)
	if !ok || strings.TrimSpace(path) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is required; use a directory path such as /mnt/user-data/uploads or /mnt/user-data/workspace")
	}
	path = tools.ResolveVirtualPath(ctx, path)

	info, err := os.Stat(path)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("list failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	if !info.IsDir() {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is not a directory")
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("list failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	if len(entries) == 0 {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: "(empty)"}, nil
	}

	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: renderDirTree(path, entries, 0)}, nil
}

func renderDirTree(root string, entries []os.DirEntry, depth int) string {
	lines := make([]string, 0, len(entries))
	sort.Slice(entries, func(i, j int) bool {
		left := entries[i]
		right := entries[j]
		if left.IsDir() != right.IsDir() {
			return left.IsDir()
		}
		return left.Name() < right.Name()
	})

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		lines = append(lines, strings.Repeat("  ", depth)+name)
		if !entry.IsDir() || depth >= 1 {
			continue
		}
		children, err := os.ReadDir(filepath.Join(root, entry.Name()))
		if err != nil || len(children) == 0 {
			continue
		}
		lines = append(lines, renderDirTree(filepath.Join(root, entry.Name()), children, depth+1))
	}
	return strings.Join(lines, "\n")
}

func StrReplaceHandler(ctx context.Context, call models.ToolCall) (models.ToolResult, error) {
	if err := tools.EnsureThreadDataDirs(ctx); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("prepare thread directories failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	args := call.Arguments
	requestedPath, ok := args["path"].(string)
	if !ok || strings.TrimSpace(requestedPath) == "" {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("path is required")
	}
	oldStr, ok := args["old_str"].(string)
	if !ok {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("old_str is required")
	}
	newStr, ok := args["new_str"].(string)
	if !ok {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("new_str is required")
	}
	replaceAll, _ := args["replace_all"].(bool)
	path := tools.ResolveVirtualPath(ctx, requestedPath)
	if err := tools.ValidateWritableToolPath(ctx, requestedPath, path); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("replace failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	content := string(data)
	if !strings.Contains(content, oldStr) {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("string to replace not found")
	}
	if !replaceAll && strings.Count(content, oldStr) != 1 {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("string to replace must appear exactly once")
	}
	if replaceAll {
		content = strings.ReplaceAll(content, oldStr, newStr)
	} else {
		content = strings.Replace(content, oldStr, newStr, 1)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return models.ToolResult{CallID: call.ID, ToolName: call.Name}, fmt.Errorf("replace failed: %s", tools.MaskLocalPaths(ctx, err.Error()))
	}
	return models.ToolResult{CallID: call.ID, ToolName: call.Name, Content: "OK"}, nil
}

func GlobTool() models.Tool {
	return models.Tool{
		Name:        "glob",
		Description: "Find files or directories that match a glob pattern under a root directory.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description":  map[string]any{"type": "string", "description": "Explain why you are searching for these paths in short words. ALWAYS PROVIDE THIS PARAMETER FIRST."},
				"pattern":      map[string]any{"type": "string", "description": "The glob pattern to match relative to the root path, for example `**/*.py`."},
				"path":         map[string]any{"type": "string", "description": "The absolute root directory to search under."},
				"include_dirs": map[string]any{"type": "boolean", "description": "Whether matching directories should also be returned. Default is false."},
				"max_results":  map[string]any{"type": "integer", "description": "Maximum number of paths to return. Default is 200."},
			},
			"required": []any{"description", "pattern", "path"},
		},
		Handler: GlobHandler,
	}
}

func GrepTool() models.Tool {
	return models.Tool{
		Name:        "grep",
		Description: "Search for matching lines inside text files under a root directory.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description":    map[string]any{"type": "string", "description": "Explain why you are searching file contents in short words. ALWAYS PROVIDE THIS PARAMETER FIRST."},
				"pattern":        map[string]any{"type": "string", "description": "The string or regex pattern to search for."},
				"path":           map[string]any{"type": "string", "description": "The absolute root directory to search under."},
				"glob":           map[string]any{"type": "string", "description": "Optional glob filter for candidate files, for example `**/*.py`."},
				"literal":        map[string]any{"type": "boolean", "description": "Whether to treat `pattern` as a plain string. Default is false."},
				"case_sensitive": map[string]any{"type": "boolean", "description": "Whether matching is case-sensitive. Default is false."},
				"max_results":    map[string]any{"type": "integer", "description": "Maximum number of matching lines to return. Default is 100."},
			},
			"required": []any{"description", "pattern", "path"},
		},
		Handler: GrepHandler,
	}
}

func LsTool() models.Tool {
	return models.Tool{
		Name:        "ls",
		Description: "List the contents of a directory up to 2 levels deep in tree format.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{"type": "string", "description": "Explain why you are listing this directory in short words. ALWAYS PROVIDE THIS PARAMETER FIRST."},
				"path":        map[string]any{"type": "string", "description": "The absolute path to the directory to list."},
			},
			"required": []any{"description", "path"},
		},
		Handler: LsHandler,
	}
}

func ReadFileTool() models.Tool {
	return models.Tool{
		Name:        "read_file",
		Description: "Read the contents of a text file. Use this to examine source code, configuration files, logs, or any text-based file.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{"type": "string", "description": "Explain why you are reading this file in short words. ALWAYS PROVIDE THIS PARAMETER FIRST."},
				"path":        map[string]any{"type": "string", "description": "The absolute path to the file to read."},
				"start_line":  map[string]any{"type": "integer", "description": "Optional starting line number (1-indexed, inclusive)."},
				"end_line":    map[string]any{"type": "integer", "description": "Optional ending line number (1-indexed, inclusive)."},
			},
			"required": []any{"description", "path"},
		},
		Handler: ReadFileHandler,
	}
}

func WriteFileTool() models.Tool {
	return models.Tool{
		Name:        "write_file",
		Description: "Write text content to a file.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{"type": "string", "description": "Explain why you are writing to this file in short words. ALWAYS PROVIDE THIS PARAMETER FIRST."},
				"path":        map[string]any{"type": "string", "description": "The absolute path to the file to write to. ALWAYS PROVIDE THIS PARAMETER SECOND."},
				"content":     map[string]any{"type": "string", "description": "The content to write to the file. ALWAYS PROVIDE THIS PARAMETER THIRD."},
				"append":      map[string]any{"type": "boolean", "description": "Append instead of overwrite."},
			},
			"required": []any{"description", "path", "content"},
		},
		Handler: WriteFileHandler,
	}
}

func StrReplaceTool() models.Tool {
	return models.Tool{
		Name:        "str_replace",
		Description: "Replace a substring in a file with another substring. If `replace_all` is false, the substring to replace must appear exactly once in the file.",
		Groups:      []string{"builtin", "file_ops"},
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"description": map[string]any{"type": "string", "description": "Explain why you are replacing the substring in short words. ALWAYS PROVIDE THIS PARAMETER FIRST."},
				"path":        map[string]any{"type": "string", "description": "The absolute path to the file to update. ALWAYS PROVIDE THIS PARAMETER SECOND."},
				"old_str":     map[string]any{"type": "string", "description": "The substring to replace. ALWAYS PROVIDE THIS PARAMETER THIRD."},
				"new_str":     map[string]any{"type": "string", "description": "The new substring. ALWAYS PROVIDE THIS PARAMETER FOURTH."},
				"replace_all": map[string]any{"type": "boolean", "description": "Whether to replace all occurrences instead of only the first."},
			},
			"required": []any{"description", "path", "old_str", "new_str"},
		},
		Handler: StrReplaceHandler,
	}
}

// FileTools returns all file operation tools.
func FileTools() []models.Tool {
	return []models.Tool{
		LsTool(),
		ReadFileTool(),
		GlobTool(),
		GrepTool(),
		WriteFileTool(),
		StrReplaceTool(),
	}
}
