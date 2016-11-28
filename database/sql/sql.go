// Package sql provides a generic interface around SQL
// (or SQL-like) databases.
// It is like standard library database/sql,
// except it accepts Context parameters throughout,
// and performs context-aware functions,
// such as recording metrics and relaying distributed
// tracing identifiers to drivers and remote hosts.
//
// The sql package must be used in conjunction with a database driver.
// See https://golang.org/s/sqldrivers for a list of drivers.
//
// For more usage examples, see the wiki page at
// https://golang.org/s/sqlwiki.
package sql

// TODO(kr): many databases—Postgres in particular—report the
// execution time of each query or statement as measured on the
// database backend. Find a way to record that timing info in
// the trace.

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"time"

	"chain/errors"
	"chain/log"
)

// Register makes a database driver available by the provided name.
// If Register is called twice with the same name or if driver is nil,
// it panics.
func Register(name string, driver driver.Driver) {
	sql.Register(name, driver)
}

const maxArgsLogLen = 20 // bytes

var logQueries bool

// EnableQueryLogging enables or disables log output for queries.
// It must be called before Open.
func EnableQueryLogging(e bool) {
	logQueries = e
}

func logQuery(ctx context.Context, query string, args interface{}) {
	if logQueries {
		s := fmt.Sprint(args)
		if len(s) > maxArgsLogLen {
			s = s[:maxArgsLogLen-3] + "..."
		}
		log.Write(ctx, "query", query, "args", s)
	}
}

func logLongQuery(ctx context.Context, query string, start time.Time) {
	dur := time.Now().Sub(start)
	if dur > 500*time.Millisecond {
		log.Write(ctx, "query", query, "duration", dur.String())
	}
}

// ErrNoRows is returned by Scan when QueryRow doesn't return a
// row. In such a case, QueryRow returns a placeholder *Row value that
// defers this error until a Scan.
var ErrNoRows = sql.ErrNoRows

// ErrTxDone is returned by Commit or Rollback when one or the other
// has already happened on a transaction.
var ErrTxDone = sql.ErrTxDone

// DB is a database handle representing a pool of zero or more
// underlying connections. It's safe for concurrent use by multiple
// goroutines.
//
// The sql package creates and frees connections automatically; it
// also maintains a free pool of idle connections. If the database has
// a concept of per-connection state, such state can only be reliably
// observed within a transaction. Once DB.Begin is called, the
// returned Tx is bound to a single connection. Once Commit or
// Rollback is called on the transaction, that transaction's
// connection is returned to DB's idle connection pool. The pool size
// can be controlled with SetMaxIdleConns.
type DB struct {
	db *sql.DB
}

// Tx is an in-progress database transaction.
//
// A transaction must end with a call to Commit or Rollback.
//
// After a call to Commit or Rollback, all operations on the
// transaction fail with ErrTxDone.
//
// The statements prepared for a transaction by calling
// the transaction's Prepare or Stmt methods are closed
// by the call to Commit or Rollback.
type Tx struct {
	tx *sql.Tx
}

// Rows is the result of a query. Its cursor starts before the first row
// of the result set. Use Next to advance through the rows:
//
//     rows, err := db.Query("SELECT ...")
//     ...
//     defer rows.Close()
//     for rows.Next() {
//         var id int
//         var name string
//         err = rows.Scan(&id, &name)
//         ...
//     }
//     err = rows.Err() // get any error encountered during iteration
//     ...
type Rows struct {
	ctx   context.Context
	query string
	start time.Time
	rows  *sql.Rows
}

// Row is the result of calling QueryRow to select a single row.
type Row struct {
	ctx   context.Context
	query string
	start time.Time
	row   *sql.Row
}

// A Result summarizes an executed SQL command.
type Result interface {
	// LastInsertId returns the integer generated by the database
	// in response to a command. Typically this will be from an
	// "auto increment" column when inserting a new row. Not all
	// databases support this feature, and the syntax of such
	// statements varies.
	LastInsertId() (int64, error)

	// RowsAffected returns the number of rows affected by an
	// update, insert, or delete. Not every database or database
	// driver may support this.
	RowsAffected() (int64, error)
}

// Open opens a database specified by its database driver name and a
// driver-specific data source name, usually consisting of at least a
// database name and connection information.
//
// Most users will open a database via a driver-specific connection
// helper function that returns a *DB. No database drivers are included
// in the Go standard library. See https://golang.org/s/sqldrivers for
// a list of third-party drivers.
//
// Open may just validate its arguments without creating a connection
// to the database. To verify that the data source name is valid, call
// Ping.
//
// The returned DB is safe for concurrent use by multiple goroutines
// and maintains its own pool of idle connections. Thus, the Open
// function should be called just once. It is rarely necessary to
// close a DB.
func Open(driverName, dataSourceName string) (*DB, error) {
	db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	return &DB{db: db}, nil
}

// Close closes the database, releasing any open resources.
//
// It is rare to Close a DB, as the DB handle is meant to be
// long-lived and shared between many goroutines.
func (db *DB) Close() error {
	return db.db.Close()
}

// SetMaxIdleConns sets the maximum number of connections in the idle
// connection pool.
//
// If MaxOpenConns is greater than 0 but less than the new MaxIdleConns
// then the new MaxIdleConns will be reduced to match the MaxOpenConns limit
//
// If n <= 0, no idle connections are retained.
func (db *DB) SetMaxIdleConns(n int) {
	db.db.SetMaxIdleConns(n)
}

// SetMaxOpenConns sets the maximum number of open connections to the database.
//
// If MaxIdleConns is greater than 0 and the new MaxOpenConns is less than
// MaxIdleConns, then MaxIdleConns will be reduced to match the new
// MaxOpenConns limit
//
// If n <= 0, then there is no limit on the number of open connections.
// The default is 0 (unlimited).
func (db *DB) SetMaxOpenConns(n int) {
	db.db.SetMaxOpenConns(n)
}

// Begin starts a transaction. The isolation level is dependent on
// the driver.
func (db *DB) Begin(ctx context.Context) (*Tx, error) {
	tx, err := db.db.Begin()
	if err != nil {
		return nil, errors.Wrap(err)
	}
	return &Tx{tx: tx}, nil
}

// Exec executes a query without returning any rows.
// The args are for any placeholder parameters in the query.
func (db *DB) Exec(ctx context.Context, query string, args ...interface{}) (Result, error) {
	s := time.Now()
	defer logLongQuery(ctx, query, s)

	logQuery(ctx, query, args)
	return db.db.Exec(query, args...)
}

// Query executes a query that returns rows, typically a SELECT.
// The args are for any placeholder parameters in the query.
func (db *DB) Query(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	s := time.Now()
	logQuery(ctx, query, args)
	rows, err := db.db.Query(query, args...)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	return &Rows{
		rows:  rows,
		ctx:   ctx,
		query: query,
		start: s,
	}, nil
}

// QueryRow executes a query that is expected to return at most one row.
// QueryRow always return a non-nil value. Errors are deferred until
// Row's Scan method is called.
func (db *DB) QueryRow(ctx context.Context, query string, args ...interface{}) *Row {
	s := time.Now()
	logQuery(ctx, query, args)
	row := db.db.QueryRow(query, args...)
	return &Row{
		row:   row,
		ctx:   ctx,
		query: query,
		start: s,
	}
}

// Commit commits the transaction.
func (tx *Tx) Commit(ctx context.Context) error {
	return tx.tx.Commit()
}

// Rollback aborts the transaction.
func (tx *Tx) Rollback(ctx context.Context) error {
	return tx.tx.Rollback()
}

// Exec executes a query that doesn't return rows.
// For example: an INSERT and UPDATE.
func (tx *Tx) Exec(ctx context.Context, query string, args ...interface{}) (Result, error) {
	s := time.Now()
	defer logLongQuery(ctx, query, s)

	logQuery(ctx, query, args)
	return tx.tx.Exec(query, args...)
}

// Query executes a query that returns rows, typically a SELECT.
// The args are for any placeholder parameters in the query.
func (tx *Tx) Query(ctx context.Context, query string, args ...interface{}) (*Rows, error) {
	s := time.Now()
	logQuery(ctx, query, args)
	rows, err := tx.tx.Query(query, args...)
	if err != nil {
		return nil, errors.Wrap(err)
	}
	return &Rows{rows: rows, ctx: ctx, query: query, start: s}, nil
}

// QueryRow executes a query that is expected to return at most one row.
// QueryRow always return a non-nil value. Errors are deferred until
// Row's Scan method is called.
func (tx *Tx) QueryRow(ctx context.Context, query string, args ...interface{}) *Row {
	s := time.Now()
	logQuery(ctx, query, args)
	row := tx.tx.QueryRow(query, args...)
	return &Row{row: row, ctx: ctx, query: query, start: s}
}

// Close closes the Rows, preventing further enumeration. If Next returns
// false, the Rows are closed automatically and it will suffice to check the
// result of Err. Close is idempotent and does not affect the result of Err.
func (rs *Rows) Close() error {
	logLongQuery(rs.ctx, rs.query, rs.start)
	return rs.rows.Close()
}

// Next prepares the next result row for reading with the Scan method.  It
// returns true on success, or false if there is no next result row or an error
// happened while preparing it.  Err should be consulted to distinguish between
// the two cases.
//
// Every call to Scan, even the first one, must be preceded by a call to Next.
func (rs *Rows) Next() bool {
	return rs.rows.Next()
}

// Err returns the error, if any, that was encountered during iteration.
// Err may be called after an explicit or implicit Close.
func (rs *Rows) Err() error {
	logLongQuery(rs.ctx, rs.query, rs.start)
	return rs.rows.Err()
}

// Scan copies the columns in the current row into the values pointed
// at by dest.
//
// If an argument has type *[]byte, Scan saves in that argument a copy
// of the corresponding data. The copy is owned by the caller and can
// be modified and held indefinitely. The copy can be avoided by using
// an argument of type *RawBytes instead; see the documentation for
// RawBytes for restrictions on its use.
//
// If an argument has type *interface{}, Scan copies the value
// provided by the underlying driver without conversion. If the value
// is of type []byte, a copy is made and the caller owns the result.
func (rs *Rows) Scan(dest ...interface{}) error {
	return rs.rows.Scan(dest...)
}

// Scan copies the columns from the matched row into the values
// pointed at by dest.  If more than one row matches the query,
// Scan uses the first row and discards the rest.  If no row matches
// the query, Scan returns ErrNoRows.
func (r *Row) Scan(dest ...interface{}) error {
	err := r.row.Scan(dest...)
	logLongQuery(r.ctx, r.query, r.start)
	return err
}
