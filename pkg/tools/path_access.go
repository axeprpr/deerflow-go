package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

const (
	skillsWriteDeniedMessage = "Write access to skills path is not allowed"
	acpWriteDeniedMessage    = "Write access to ACP workspace is not allowed"
)

// ValidateWritableToolPath rejects writes to read-only DeerFlow virtual paths.
func ValidateWritableToolPath(ctx context.Context, requestedPath, resolvedPath string) error {
	requestedPath = strings.TrimSpace(requestedPath)
	resolvedPath = filepath.Clean(strings.TrimSpace(resolvedPath))
	if requestedPath == "" && resolvedPath == "." {
		return nil
	}

	if requestedPath == skillsVirtualPath || strings.HasPrefix(requestedPath, skillsVirtualPath+"/") {
		return fmt.Errorf("%s: %s", skillsWriteDeniedMessage, requestedPath)
	}
	if requestedPath == acpWorkspaceVirtualPath || strings.HasPrefix(requestedPath, acpWorkspaceVirtualPath+"/") {
		return fmt.Errorf("%s: %s", acpWriteDeniedMessage, requestedPath)
	}

	for _, root := range skillRoots() {
		if pathWithinRoot(root, resolvedPath) {
			return fmt.Errorf("%s: %s", skillsWriteDeniedMessage, MaskLocalPaths(ctx, resolvedPath))
		}
	}
	if acpRoot, err := ACPWorkspaceDir(ThreadIDFromContext(ctx)); err == nil && pathWithinRoot(acpRoot, resolvedPath) {
		return fmt.Errorf("%s: %s", acpWriteDeniedMessage, MaskLocalPaths(ctx, resolvedPath))
	}
	return nil
}

func pathWithinRoot(root, candidate string) bool {
	root = filepath.Clean(strings.TrimSpace(root))
	candidate = filepath.Clean(strings.TrimSpace(candidate))
	if root == "" || candidate == "" {
		return false
	}
	if root == candidate {
		return true
	}
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
}
