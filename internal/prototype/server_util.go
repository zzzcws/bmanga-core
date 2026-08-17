package prototype

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) query(stmt string, args ...any) ([]map[string]any, error) {
	rows, err := s.db.Query(stmt, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	result := []map[string]any{}
	for rows.Next() {
		values := make([]any, len(cols))
		scan := make([]any, len(cols))
		for i := range values {
			scan[i] = &values[i]
		}
		if err := rows.Scan(scan...); err != nil {
			return nil, err
		}
		item := map[string]any{}
		for i, col := range cols {
			item[col] = normalizeDBValue(values[i])
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *Server) localTableExists(name string) bool {
	rows, err := s.query(`SELECT 1 FROM sqlite_master WHERE type IN ('table', 'view') AND name = ? LIMIT 1`, name)
	return err == nil && len(rows) > 0
}

func normalizeDBValue(value any) any {
	switch v := value.(type) {
	case nil:
		return nil
	case []byte:
		return string(v)
	default:
		return v
	}
}

func readJSONBody(r *http.Request, maxBytes int64) (map[string]any, error) {
	if r.Body == nil {
		return map[string]any{}, nil
	}
	defer r.Body.Close()
	reader := io.LimitReader(r.Body, maxBytes+1)
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errors.New("request too large")
	}
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, errors.New("invalid json")
	}
	if payload == nil {
		return map[string]any{}, nil
	}
	return payload, nil
}

func whereClause(filters []string) string {
	if len(filters) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(filters, " AND ")
}

func firstRow(rows []map[string]any) map[string]any {
	if len(rows) == 0 {
		return map[string]any{}
	}
	return rows[0]
}

func firstPositive(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func coalesceAny(values ...any) any {
	for _, value := range values {
		if value != nil && stringValue(value) != "" {
			return value
		}
	}
	return nil
}

func coalesceString(values ...any) string {
	return stringValue(coalesceAny(values...))
}

func intValue(value any) int {
	switch v := value.(type) {
	case nil:
		return 0
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case string:
		i, _ := strconv.Atoi(v)
		return i
	default:
		return 0
	}
}

func floatValue(value any) float64 {
	switch v := value.(type) {
	case nil:
		return 0
	case float64:
		return v
	case float32:
		return float64(v)
	case int64:
		return float64(v)
	case int:
		return float64(v)
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	default:
		return 0
	}
}

func optionalInt(value any, low int, high int, field string) (any, error) {
	if value == nil || stringValue(value) == "" {
		return nil, nil
	}
	parsed := intValue(value)
	if parsed < low || parsed > high {
		return nil, fmt.Errorf("%s out of range", field)
	}
	return parsed, nil
}

func optionalIntDefault(value any, fallback int, low int, high int, field string) (int, error) {
	if value == nil || stringValue(value) == "" {
		return fallback, nil
	}
	parsed := intValue(value)
	if parsed < low || parsed > high {
		return fallback, fmt.Errorf("%s out of range", field)
	}
	return parsed, nil
}

func validReadStatus(value string) bool {
	switch value {
	case "unread", "reading", "completed", "abandoned":
		return true
	default:
		return false
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nowISO() string {
	return time.Now().Local().Format("2006-01-02T15:04:05-07:00")
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func stringValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}

func shelfTitle(item map[string]any) string {
	for _, key := range []string{"display_title", "title", "series_title"} {
		value := stringValue(item[key])
		if value != "" {
			return value
		}
	}
	return ""
}

func clampInt(raw string, fallback, low, high int) int {
	value := fallback
	if raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil {
			value = parsed
		}
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func allowGet(w http.ResponseWriter, r *http.Request) bool {
	if r.Method == http.MethodGet {
		return true
	}
	writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
	return false
}

func writeJSON(w http.ResponseWriter, payload any) {
	writeJSONStatus(w, http.StatusOK, payload)
}

func writeJSONStatus(w http.ResponseWriter, status int, payload any) {
	body, err := json.Marshal(payload)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func writeJSONBytes(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func writeJSONError(w http.ResponseWriter, status int, message string) {
	if message == "" {
		message = http.StatusText(status)
	}
	body, err := json.Marshal(map[string]any{"error": message})
	if err != nil {
		body = []byte(`{"error":"internal error"}`)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func ErrNotImplemented() error {
	return errors.New("not implemented in Go prototype yet")
}
