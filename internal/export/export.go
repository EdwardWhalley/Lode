// Package export writes query results to a CSV or JSON file.
package export

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ErrExists is returned by Create for a path that already exists.
var ErrExists = errors.New("file already exists")

// Target is a file created for one export. Write fills it and closes it.
type Target struct {
	path   string
	file   *os.File
	format format
}

type format int

const (
	formatCSV format = iota
	formatJSON
)

// Create makes the owner-only file for an export, by extension, failing with ErrExists if
// the path exists.
func Create(path string) (*Target, error) {
	var f format
	switch strings.ToLower(filepath.Ext(path)) {
	case ".csv":
		f = formatCSV
	case ".json":
		f = formatJSON
	default:
		return nil, errors.New("cannot export to this file name: use .csv or .json")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil, ErrExists
	}
	if err != nil {
		return nil, err
	}
	return &Target{path: path, file: file, format: f}, nil
}

// Write writes the rows and closes the file, removing it if anything fails.
func (t *Target) Write(cols []string, rows [][]any) error { return t.writeTo(t.file, cols, rows) }

func (t *Target) writeTo(w io.Writer, cols []string, rows [][]any) error {
	bw := bufio.NewWriter(w)
	var err error
	switch t.format {
	case formatCSV:
		err = writeCSV(bw, cols, rows)
	default:
		err = writeJSON(bw, cols, rows)
	}
	if err == nil {
		err = bw.Flush()
	}
	if cerr := t.file.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(t.path)
		return err
	}
	return nil
}

// uniqueNames suffixes repeated column names: a, a_1, a_2.
func uniqueNames(cols []string) []string {
	used := map[string]bool{}
	for _, c := range cols {
		used[c] = true
	}
	seen := map[string]bool{}
	out := make([]string, len(cols))
	for i, c := range cols {
		name := c
		if seen[name] {
			for n := 1; ; n++ {
				name = c + "_" + strconv.Itoa(n)
				if !used[name] && !seen[name] {
					break
				}
			}
		}
		seen[name] = true
		out[i] = name
	}
	return out
}

func writeCSV(w io.Writer, cols []string, rows [][]any) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(uniqueNames(cols)); err != nil {
		return err
	}
	rec := make([]string, len(cols))
	for _, row := range rows {
		for i := range rec {
			rec[i] = ""
			if i < len(row) {
				rec[i] = CellText(row[i])
			}
		}
		if len(rec) == 1 && rec[0] == "" {
			cw.Flush()
			if _, err := io.WriteString(w, "\"\"\n"); err != nil {
				return err
			}
			continue
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// RowText is a row as tab-separated text to paste into a spreadsheet, with no line ending.
func RowText(row []any) string {
	cells := make([]string, len(row))
	for i, v := range row {
		cells[i] = CellText(v)
		if strings.ContainsAny(cells[i], "\t\r\n\"") {
			cells[i] = `"` + strings.ReplaceAll(cells[i], `"`, `""`) + `"`
		}
	}
	return strings.Join(cells, "\t")
}

// CellText is one value as text: NULL is empty, timestamps are RFC 3339, binary is \x hex,
// a list or struct JSON.
func CellText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case []byte:
		if isText(v) {
			return string(v)
		}
		return fmt.Sprintf(`\x%x`, v)
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case float32:
		return strconv.FormatFloat(float64(v), 'g', -1, 32)
	case float64:
		return strconv.FormatFloat(v, 'g', -1, 64)
	case string:
		return v
	case []any, map[string]any:
		if b, err := nestedJSON(v); err == nil {
			return string(b)
		}
		return fmt.Sprint(v)
	default:
		return fmt.Sprint(v)
	}
}

// isText says bytes are text and not binary: valid UTF-8 with no control character but tabs
// and line breaks.
func isText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if unicode.IsControl(r) && r != '\t' && r != '\r' && r != '\n' {
			return false
		}
	}
	return true
}

// writeJSON writes an array of objects in column order.
func writeJSON(w io.Writer, cols []string, rows [][]any) error {
	names := uniqueNames(cols)
	keys := make([][]byte, len(names))
	for i, n := range names {
		keys[i], _ = json.Marshal(n)
	}
	if len(rows) == 0 {
		_, err := io.WriteString(w, "[]\n")
		return err
	}
	if _, err := io.WriteString(w, "[\n"); err != nil {
		return err
	}
	for r, row := range rows {
		var b []byte
		b = append(b, "  {"...)
		for i := range names {
			if i > 0 {
				b = append(b, ", "...)
			}
			b = append(b, keys[i]...)
			b = append(b, ": "...)
			var cell any
			if i < len(row) {
				cell = row[i]
			}
			b = append(b, jsonValue(cell)...)
		}
		b = append(b, '}')
		if r < len(rows)-1 {
			b = append(b, ',')
		}
		b = append(b, '\n')
		if _, err := w.Write(b); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, "]\n")
	return err
}

// jsonValue renders one cell as a JSON value.
func jsonValue(v any) []byte {
	switch v := v.(type) {
	case nil:
		return []byte("null")
	case bool:
		return strconv.AppendBool(nil, v)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return []byte(fmt.Sprint(v))
	case *big.Int:
		return []byte(v.String())
	case float32:
		return jsonFloat(float64(v), 32)
	case float64:
		return jsonFloat(v, 64)
	case []byte:
		if isText(v) {
			return jsonString(string(v))
		}
		return jsonString(base64.StdEncoding.EncodeToString(v))
	case time.Time:
		return jsonString(v.Format(time.RFC3339Nano))
	case string:
		return jsonString(v)
	case decimal:
		return []byte(v.String())
	case map[string]any, []any:
		if b, err := nestedJSON(v); err == nil {
			return b
		}
	}
	return jsonString(fmt.Sprint(v))
}

// decimal is a driver's exact number, told by its methods because this package imports no
// driver.
type decimal interface {
	fmt.Stringer
	Float64() float64
}

// nestedJSON is a list or struct as JSON, leaving <, > and & as they are.
func nestedJSON(v any) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(jsonReady(v)); err != nil {
		return nil, err
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}

// jsonReady makes a nested value something json.Marshal writes properly.
func jsonReady(v any) any {
	switch v := v.(type) {
	case decimal:
		return json.Number(v.String())
	case time.Time:
		return v.Format(time.RFC3339Nano)
	case []byte:
		if isText(v) {
			return string(v)
		}
		return base64.StdEncoding.EncodeToString(v)
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return strconv.FormatFloat(v, 'g', -1, 64)
		}
	case float32:
		if f := float64(v); math.IsNaN(f) || math.IsInf(f, 0) {
			return strconv.FormatFloat(f, 'g', -1, 32)
		}
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = jsonReady(e)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = jsonReady(e)
		}
		return out
	}
	return v
}

// jsonFloat writes a number, or a string for NaN and the infinities, which JSON cannot
// hold.
func jsonFloat(f float64, bits int) []byte {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return jsonString(strconv.FormatFloat(f, 'g', -1, bits))
	}
	return []byte(strconv.FormatFloat(f, 'g', -1, bits))
}

func jsonString(s string) []byte {
	b, _ := json.Marshal(s)
	return b
}
