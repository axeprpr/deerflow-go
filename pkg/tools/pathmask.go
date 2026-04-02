package tools

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
)

type pathMaskMapping struct {
	actual  string
	virtual string
}

// MaskLocalPaths rewrites host filesystem paths in user-visible output back to
// DeerFlow virtual paths so responses stay portable and do not leak local
// directory structure.
func MaskLocalPaths(ctx context.Context, text string) string {
	if strings.TrimSpace(text) == "" {
		return text
	}

	mappings := localPathMappings(ctx)
	if len(mappings) == 0 {
		return text
	}
	sort.Slice(mappings, func(i, j int) bool {
		return len(mappings[i].actual) > len(mappings[j].actual)
	})

	masked := text
	for _, mapping := range mappings {
		if mapping.actual == "" || mapping.virtual == "" {
			continue
		}
		masked = strings.ReplaceAll(masked, mapping.actual, mapping.virtual)
		slashNormalized := filepath.ToSlash(mapping.actual)
		if slashNormalized != mapping.actual {
			masked = strings.ReplaceAll(masked, slashNormalized, mapping.virtual)
		}
	}
	return masked
}

func localPathMappings(ctx context.Context) []pathMaskMapping {
	threadID := ThreadIDFromContext(ctx)
	if threadID == "" {
		return nil
	}

	mappings := make([]pathMaskMapping, 0, 4)
	if root := threadDataRootFromThreadID(threadID); strings.TrimSpace(root) != "" {
		mappings = append(mappings, pathMaskMapping{
			actual:  filepath.Clean(root),
			virtual: "/mnt/user-data",
		})
	}
	if acpRoot, err := ACPWorkspaceDir(threadID); err == nil && strings.TrimSpace(acpRoot) != "" {
		mappings = append(mappings, pathMaskMapping{
			actual:  filepath.Clean(acpRoot),
			virtual: acpWorkspaceVirtualPath,
		})
	}
	for _, root := range skillRoots() {
		if strings.TrimSpace(root) == "" {
			continue
		}
		mappings = append(mappings, pathMaskMapping{
			actual:  filepath.Clean(root),
			virtual: skillsVirtualPath,
		})
	}
	return mappings
}
