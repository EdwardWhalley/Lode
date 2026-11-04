// Rows over database/sql, and the result of a statement with no rows.

package adapter

import "database/sql"

// NoRows is the result of a statement with no rows to show: a count if there is one, and
// any error.
func NoRows(n int64, known bool, err error) Rows { return noRows{n: n, known: known, err: err} }

type noRows struct {
	n     int64
	known bool
	err   error
}

func (r noRows) Columns() []Column           { return nil }
func (r noRows) Next() bool                  { return false }
func (r noRows) Values() []any               { return nil }
func (r noRows) Err() error                  { return r.err }
func (r noRows) RowsAffected() (int64, bool) { return r.n, r.known }
func (r noRows) Close() error                { return nil }

// FromSQL adapts a database/sql result set to Rows. It takes ownership of rs.
func FromSQL(rs *sql.Rows) (Rows, error) {
	types, err := rs.ColumnTypes()
	if err != nil {
		rs.Close()
		return nil, err
	}
	cols := make([]Column, len(types))
	for i, t := range types {
		cols[i] = Column{Name: t.Name(), Type: t.DatabaseTypeName()}
	}
	return &sqlRows{rs: rs, cols: cols}, nil
}

type sqlRows struct {
	rs      *sql.Rows
	cols    []Column
	vals    []any
	scanErr error
}

func (r *sqlRows) Columns() []Column { return r.cols }

func (r *sqlRows) Next() bool {
	if r.scanErr != nil || !r.rs.Next() {
		return false
	}
	dest := make([]any, len(r.cols))
	ptrs := make([]any, len(r.cols))
	for i := range dest {
		ptrs[i] = &dest[i]
	}
	if err := r.rs.Scan(ptrs...); err != nil {
		r.scanErr = err
		r.vals = nil
		return false
	}
	r.vals = dest
	return true
}

func (r *sqlRows) Values() []any { return r.vals }

func (r *sqlRows) Err() error {
	if r.scanErr != nil {
		return r.scanErr
	}
	return r.rs.Err()
}

// RowsAffected is not known: database/sql gives it only to Exec.
func (r *sqlRows) RowsAffected() (int64, bool) { return 0, false }

func (r *sqlRows) Close() error { return r.rs.Close() }
