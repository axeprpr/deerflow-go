package langgraphcompat

import (
	"net/url"
	"regexp"
	"strings"

	"github.com/axeprpr/deerflow-go/pkg/models"
)

func addArtifactPath(paths *[]string, seen map[string]struct{}, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	if _, exists := seen[path]; exists {
		return true
	}
	seen[path] = struct{}{}
	*paths = append(*paths, path)
	return true
}

var artifactMarkdownLinkPattern = regexp.MustCompile(`(?m)(\[[^\]]*?\])\((/mnt/user-data/(?:uploads|outputs|workspace)/[^)\n]*?\.[A-Za-z0-9]+)\)`)
var artifactBarePathPattern = regexp.MustCompile(`(?m)(^|[\s>:"'` + "`" + `])(/mnt/user-data/(?:uploads|outputs|workspace)/[^)\]\n]*?\.[A-Za-z0-9]+)`)

func rewriteAssistantArtifactLinks(threadID string, content any, role models.Role) any {
	if role != models.RoleAI {
		return content
	}
	text, ok := content.(string)
	if !ok || strings.TrimSpace(text) == "" {
		return content
	}
	return rewriteArtifactLinksInText(threadID, text)
}

func rewriteArtifactLinksInText(threadID, text string) string {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" || strings.TrimSpace(text) == "" {
		return text
	}
	rewritten := artifactMarkdownLinkPattern.ReplaceAllStringFunc(text, func(match string) string {
		parts := artifactMarkdownLinkPattern.FindStringSubmatch(match)
		if len(parts) != 3 || strings.HasPrefix(parts[1], "![") {
			return match
		}
		return parts[1] + "(" + artifactURLForThread(threadID, parts[2]) + ")"
	})
	return artifactBarePathPattern.ReplaceAllStringFunc(rewritten, func(match string) string {
		parts := artifactBarePathPattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		return parts[1] + artifactURLForThread(threadID, parts[2])
	})
}

func artifactURLForThread(threadID, virtualPath string) string {
	virtualPath = strings.TrimSpace(virtualPath)
	if virtualPath == "" {
		return virtualPath
	}
	trimmed := strings.TrimPrefix(virtualPath, "/")
	segments := strings.Split(trimmed, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return "/api/threads/" + strings.TrimSpace(threadID) + "/artifacts/" + strings.Join(segments, "/")
}

func messageArtifactPaths(message models.Message) []string {
	paths := make([]string, 0)
	for _, call := range message.ToolCalls {
		if call.Status != models.CallStatusCompleted {
			continue
		}
		switch call.Name {
		case "present_file":
			if path, _ := call.Arguments["path"].(string); strings.TrimSpace(path) != "" {
				paths = append(paths, path)
			}
		case "present_files":
			paths = append(paths, anyStringSlice(call.Arguments["filepaths"])...)
		}
	}
	if message.ToolResult != nil {
		if message.ToolResult.Status != models.CallStatusCompleted {
			return paths
		}
		switch message.ToolResult.ToolName {
		case "present_file":
			if path, _ := message.ToolResult.Data["path"].(string); strings.TrimSpace(path) != "" {
				paths = append(paths, path)
			}
		case "present_files":
			paths = append(paths, anyStringSlice(message.ToolResult.Data["filepaths"])...)
		}
	}
	return paths
}
