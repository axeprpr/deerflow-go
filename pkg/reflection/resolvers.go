package reflection

import (
	"fmt"
	"strings"
)

var ModuleToPackageHints = map[string]string{
	"langchain_google_genai": "langchain-google-genai",
	"langchain_anthropic":    "langchain-anthropic",
	"langchain_openai":       "langchain-openai",
	"langchain_deepseek":     "langchain-deepseek",
}

type SymbolLookupResult[T any] struct {
	Value       T
	ModuleFound bool
	SymbolFound bool
}

func BuildMissingDependencyHint(modulePath, missingModule string) string {
	moduleRoot := strings.TrimSpace(modulePath)
	if idx := strings.Index(moduleRoot, "."); idx >= 0 {
		moduleRoot = moduleRoot[:idx]
	}
	missingModule = strings.TrimSpace(missingModule)
	if missingModule == "" {
		missingModule = moduleRoot
	}
	packageName := ModuleToPackageHints[moduleRoot]
	if packageName == "" {
		packageName = ModuleToPackageHints[missingModule]
	}
	if packageName == "" {
		packageName = strings.ReplaceAll(missingModule, "_", "-")
	}
	return fmt.Sprintf("Missing dependency '%s'. Install it with `uv add %s` (or `pip install %s`), then restart DeerFlow.", missingModule, packageName, packageName)
}

func ResolveVariable[T any](variablePath string, lookup func(modulePath, variableName string) SymbolLookupResult[T]) (T, error) {
	var zero T
	modulePath, variableName, err := parseSymbolPath(variablePath)
	if err != nil {
		return zero, err
	}

	result := lookup(modulePath, variableName)
	if !result.ModuleFound {
		moduleRoot := modulePath
		if idx := strings.Index(moduleRoot, "."); idx >= 0 {
			moduleRoot = moduleRoot[:idx]
		}
		hint := BuildMissingDependencyHint(modulePath, moduleRoot)
		return zero, fmt.Errorf("Could not import module %s. %s", modulePath, hint)
	}
	if !result.SymbolFound {
		return zero, fmt.Errorf("Module %s does not define a %s attribute/class", modulePath, variableName)
	}
	return result.Value, nil
}

func ResolveClass[T any](classPath string, lookup func(modulePath, className string) SymbolLookupResult[T]) (T, error) {
	return ResolveVariable(classPath, lookup)
}

func parseSymbolPath(path string) (string, string, error) {
	modulePath, symbolName, ok := strings.Cut(strings.TrimSpace(path), ":")
	if !ok || strings.TrimSpace(modulePath) == "" || strings.TrimSpace(symbolName) == "" {
		return "", "", fmt.Errorf("%s doesn't look like a variable path. Example: parent_package_name.sub_package_name.module_name:variable_name", path)
	}
	return strings.TrimSpace(modulePath), strings.TrimSpace(symbolName), nil
}
