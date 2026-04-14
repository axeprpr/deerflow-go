package harnessruntime

import "strings"

const DefaultRunIDMetadataKey = "run_id"

const DefaultActiveRunMetadataKey = "active_run_id"

func metadataRunID(value any) string {
	raw, _ := value.(string)
	return strings.TrimSpace(raw)
}
