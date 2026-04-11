package langgraphcompat

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) handleThreadSearch(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	var req struct {
		Query      string         `json:"query"`
		Status     string         `json:"status"`
		Limit      int            `json:"limit"`
		PageSize   int            `json:"page_size"`
		PageSizeX  int            `json:"pageSize"`
		Offset     int            `json:"offset"`
		SortBy     string         `json:"sort_by"`
		SortByX    string         `json:"sortBy"`
		SortOrder  string         `json:"sort_order"`
		SortOrderX string         `json:"sortOrder"`
		Select     []string       `json:"select"`
		Metadata   map[string]any `json:"metadata"`
		Values     map[string]any `json:"values"`
	}
	limitProvided := false
	if r.Body != nil {
		defer r.Body.Close()
		body, _ := io.ReadAll(r.Body)
		if len(body) > 0 {
			_ = json.Unmarshal(body, &raw)
			_ = json.Unmarshal(body, &req)
			_, hasLimit := raw["limit"]
			_, hasPageSize := raw["pageSize"]
			_, hasPageSizeSnake := raw["page_size"]
			limitProvided = hasLimit || hasPageSize || hasPageSizeSnake
		}
	}
	queryValues := r.URL.Query()
	if req.Query == "" {
		req.Query = strings.TrimSpace(queryValues.Get("query"))
	}

	if req.Limit == 0 {
		req.Limit = req.PageSize
	}
	if req.Limit == 0 {
		req.Limit = req.PageSizeX
	}
	if req.Limit == 0 {
		if rawLimit := firstNonEmpty(queryValues.Get("limit"), queryValues.Get("pageSize"), queryValues.Get("page_size")); rawLimit != "" {
			req.Limit, _ = strconv.Atoi(rawLimit)
			limitProvided = true
		}
	}
	if req.Limit < 0 {
		req.Limit = 0
	}
	if !limitProvided && req.Limit == 0 {
		req.Limit = 50
	}
	if req.Offset == 0 {
		if rawOffset := strings.TrimSpace(queryValues.Get("offset")); rawOffset != "" {
			req.Offset, _ = strconv.Atoi(rawOffset)
		}
	}
	if req.Offset < 0 {
		req.Offset = 0
	}
	req.SortBy = firstNonEmpty(req.SortBy, req.SortByX)
	req.SortOrder = firstNonEmpty(req.SortOrder, req.SortOrderX)
	req.SortBy = firstNonEmpty(req.SortBy, queryValues.Get("sortBy"), queryValues.Get("sort_by"))
	req.SortOrder = firstNonEmpty(req.SortOrder, queryValues.Get("sortOrder"), queryValues.Get("sort_order"))
	if len(req.Select) == 0 {
		if rawSelect := strings.TrimSpace(queryValues.Get("select")); rawSelect != "" {
			for _, item := range strings.Split(rawSelect, ",") {
				item = strings.TrimSpace(item)
				if item != "" {
					req.Select = append(req.Select, item)
				}
			}
		}
	}
	req.SortBy = normalizeThreadFieldName(req.SortBy)
	if req.SortBy == "" {
		req.SortBy = "updated_at"
	}
	if req.SortOrder == "" {
		req.SortOrder = "desc"
	}

	writeJSON(w, http.StatusOK, s.searchThreadResponses(threadSearchRequest{
		Query:     req.Query,
		Status:    req.Status,
		Limit:     req.Limit,
		Offset:    req.Offset,
		SortBy:    req.SortBy,
		SortOrder: req.SortOrder,
		Select:    req.Select,
		Metadata:  req.Metadata,
		Values:    req.Values,
	}))
}
