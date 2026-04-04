package reflection

import (
	"strings"
	"testing"
)

func TestResolveVariableReturnsValue(t *testing.T) {
	value, err := ResolveVariable("app.channels.feishu:FeishuChannel", func(modulePath, variableName string) SymbolLookupResult[string] {
		if modulePath == "app.channels.feishu" && variableName == "FeishuChannel" {
			return SymbolLookupResult[string]{
				Value:       "ok",
				ModuleFound: true,
				SymbolFound: true,
			}
		}
		return SymbolLookupResult[string]{}
	})
	if err != nil {
		t.Fatalf("ResolveVariable() error = %v", err)
	}
	if value != "ok" {
		t.Fatalf("value = %q, want ok", value)
	}
}

func TestResolveVariableReturnsMissingModuleHint(t *testing.T) {
	_, err := ResolveVariable("langchain_openai:ChatOpenAI", func(modulePath, variableName string) SymbolLookupResult[string] {
		return SymbolLookupResult[string]{}
	})
	if err == nil {
		t.Fatal("ResolveVariable() expected error")
	}
	if !strings.Contains(err.Error(), "Could not import module langchain_openai") {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "uv add langchain-openai") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveClassReturnsMissingAttributeError(t *testing.T) {
	_, err := ResolveClass("app.channels.feishu:FeishuChannel", func(modulePath, className string) SymbolLookupResult[string] {
		return SymbolLookupResult[string]{ModuleFound: true}
	})
	if err == nil {
		t.Fatal("ResolveClass() expected error")
	}
	if !strings.Contains(err.Error(), "does not define a FeishuChannel attribute/class") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveVariableRejectsInvalidPath(t *testing.T) {
	_, err := ResolveVariable("bad-path", func(modulePath, variableName string) SymbolLookupResult[string] {
		return SymbolLookupResult[string]{}
	})
	if err == nil {
		t.Fatal("ResolveVariable() expected error")
	}
	if !strings.Contains(err.Error(), "doesn't look like a variable path") {
		t.Fatalf("err=%v", err)
	}
}
