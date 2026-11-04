// Turns the DuckDB driver's own types into plain values.

package file

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/marcboeker/go-duckdb/v2"

	"github.com/EdwardWhalley/lode/internal/adapter"
)

// plainValues reads rows as plain values, not the driver's own types: each is one
// adapter.CheckValue allows, and a narrower integer is int64.
func plainValues(rows adapter.Rows) adapter.Rows {
	cols := rows.Columns()
	if len(cols) == 0 {
		return rows
	}
	types := make([]columnType, len(cols))
	for i, c := range cols {
		types[i] = columnTypeOf(c.Type)
	}
	return plainRows{Rows: rows, types: types}
}

type plainRows struct {
	adapter.Rows
	types []columnType
}

func (r plainRows) Values() []any {
	vals := r.Rows.Values()
	if vals == nil {
		return nil
	}
	out := make([]any, len(vals))
	for i, v := range vals {
		out[i] = plain(v, r.types[i])
	}
	return out
}

// columnType is what a value cannot say for itself and only the column's type can, such as
// a UUID or a TIME.
type columnType struct {
	uuid  bool
	clock bool
	zoned bool
}

var (
	quotedName = regexp.MustCompile(`"(?:[^"]|"")*"`)
	uuidType   = regexp.MustCompile(`\bUUID\b`)
	blobType   = regexp.MustCompile(`\bBLOB\b`)
	clockType  = regexp.MustCompile(`\bTIME(TZ)?\b`)
	zonedType  = regexp.MustCompile(`\bTIMETZ\b`)
)

// columnTypeOf says what a column's type text means for reading its values.
func columnTypeOf(t string) columnType {
	t = quotedName.ReplaceAllString(t, "")
	return columnType{
		uuid:  uuidType.MatchString(t) && !blobType.MatchString(t),
		clock: clockType.MatchString(t),
		zoned: zonedType.MatchString(t),
	}
}

// plain turns one value from the driver's type into a plain one, walking lists, structs and
// maps.
func plain(v any, t columnType) any {
	switch v := v.(type) {
	case []byte:
		if t.uuid && len(v) == 16 {
			var u duckdb.UUID
			copy(u[:], v)
			return u.String()
		}
	case time.Time:
		if t.clock {
			if text, ok := clockOfDay(v, t.zoned); ok {
				return text
			}
		}
	case int8:
		return int64(v)
	case int16:
		return int64(v)
	case int32:
		return int64(v)
	case uint8:
		return int64(v)
	case uint16:
		return int64(v)
	case uint32:
		return int64(v)
	case duckdb.Decimal:
		return exactDecimal{v}
	case duckdb.Interval:
		return intervalText(v)
	case duckdb.Union:
		return plain(v.Value, t)
	case []any:
		out := make([]any, len(v))
		for i, e := range v {
			out[i] = plain(e, t)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[k] = plain(e, t)
		}
		return out
	case duckdb.Map:
		out := make(map[string]any, len(v))
		for k, e := range v {
			out[mapKey(plain(k, t))] = plain(e, t)
		}
		return out
	}
	return v
}

// mapKey is a map's key as text, since a key in JSON has to be.
func mapKey(k any) string {
	if at, ok := k.(time.Time); ok {
		return at.Format(time.RFC3339Nano)
	}
	return fmt.Sprint(k)
}

// clockOfDay reads a TIME or TIMETZ, which the driver hands over on a day around the year
// 1.
func clockOfDay(v time.Time, zoned bool) (string, bool) {
	since := v.Sub(time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC))
	if zoned {
		return v.Format("15:04:05.999999") + "+00", since > -24*time.Hour && since < 48*time.Hour
	}
	return v.Format("15:04:05.999999"), since >= 0 && since < 24*time.Hour
}

// exactDecimal keeps the scale the driver's own String drops: 100.00 and not 100.
type exactDecimal struct{ duckdb.Decimal }

func (d exactDecimal) String() string {
	digits := new(big.Int).Abs(d.Value).String()
	if scale := int(d.Scale); scale > 0 {
		if len(digits) <= scale {
			digits = strings.Repeat("0", scale-len(digits)+1) + digits
		}
		digits = digits[:len(digits)-scale] + "." + digits[len(digits)-scale:]
	}
	if d.Value.Sign() < 0 {
		return "-" + digits
	}
	return digits
}

// intervalText writes an interval as DuckDB does: 1 year 2 months 2 days 03:00:00.
func intervalText(v duckdb.Interval) string {
	var parts []string
	unit := func(n int32, name string) {
		switch n {
		case 0:
		case 1, -1:
			parts = append(parts, fmt.Sprintf("%d %s", n, name))
		default:
			parts = append(parts, fmt.Sprintf("%d %ss", n, name))
		}
	}
	unit(v.Months/12, "year")
	unit(v.Months%12, "month")
	unit(v.Days, "day")
	if v.Micros != 0 || len(parts) == 0 {
		parts = append(parts, clockText(v.Micros))
	}
	return strings.Join(parts, " ")
}

// clockText is a span of microseconds as hours, minutes and seconds. The hours can pass 24.
func clockText(micros int64) string {
	sign, size := "", uint64(micros)
	if micros < 0 {
		sign, size = "-", -uint64(micros)
	}
	const second = uint64(time.Second / time.Microsecond)
	seconds := size / second
	text := fmt.Sprintf("%s%02d:%02d:%02d", sign, seconds/3600, seconds/60%60, seconds%60)
	if fraction := size % second; fraction != 0 {
		text += strings.TrimRight(fmt.Sprintf(".%06d", fraction), "0")
	}
	return text
}
