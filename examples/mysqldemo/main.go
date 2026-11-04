// Command mysqldemo loads the demo shop data into a running MySQL or MariaDB server, for
// trying lode against a real one: go run ./examples/mysqldemo -dsn
// mysql://user:password@host:3306/
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strings"

	driver "github.com/go-sql-driver/mysql"

	"github.com/EdwardWhalley/lode/examples/internal/demodata"
)

// markerTable is created in every database this command makes, and allows a later run to
// replace it.
const markerTable = "_lode_demo"

const shopDDL = `
CREATE TABLE customers (
	id        INT PRIMARY KEY,
	name      VARCHAR(100) NOT NULL,
	email     VARCHAR(200),
	country   VARCHAR(50) NOT NULL,
	signed_up DATE NOT NULL,
	notes     TEXT
);
CREATE TABLE products (
	id       INT PRIMARY KEY,
	name     VARCHAR(100) NOT NULL,
	category VARCHAR(50) NOT NULL,
	price    DECIMAL(10,2) NOT NULL
);
CREATE TABLE orders (
	id          INT PRIMARY KEY,
	customer_id INT NOT NULL,
	ordered_at  DATETIME NOT NULL,
	status      VARCHAR(20) NOT NULL,
	FOREIGN KEY (customer_id) REFERENCES customers(id)
);
CREATE TABLE order_items (
	order_id   INT NOT NULL,
	product_id INT NOT NULL,
	quantity   INT NOT NULL,
	FOREIGN KEY (order_id) REFERENCES orders(id),
	FOREIGN KEY (product_id) REFERENCES products(id)
);
CREATE TABLE events (
	id      INT PRIMARY KEY,
	at      DATETIME NOT NULL,
	kind    VARCHAR(30) NOT NULL,
	payload JSON
);
CREATE VIEW customer_totals AS
	SELECT c.name, c.country, count(DISTINCT o.id) AS orders, round(sum(p.price * i.quantity), 2) AS total
	FROM customers c
	JOIN orders o ON o.customer_id = c.id
	JOIN order_items i ON i.order_id = o.id
	JOIN products p ON p.id = i.product_id
	WHERE o.status <> 'cancelled'
	GROUP BY c.id;
`

func main() {
	dsn := flag.String("dsn", os.Getenv("LODE_TEST_MYSQL_DSN"),
		"mysql:// or mariadb:// DSN of an account that can create databases (default: $LODE_TEST_MYSQL_DSN)")
	database := flag.String("database", "shop", "name of the shop database; <name>_reporting is created beside it")
	flag.Parse()

	if *dsn == "" {
		fmt.Fprintln(os.Stderr, "mysqldemo: no server given; pass -dsn mysql://user:password@host:3306/")
		os.Exit(2)
	}

	db, err := open(*dsn)
	if err != nil {
		fail(err)
	}
	defer db.Close()

	fmt.Printf("loading demo data into %q on %s ...\n", *database, hostOf(*dsn))
	if err := load(context.Background(), db, *database); err != nil {
		fail(err)
	}

	scheme := schemeOf(*dsn)
	shop := withDatabase(*dsn, *database)
	fmt.Printf(`
Done. Add to ~/.config/lode/sources.toml (set LODE_DEMO_PASSWORD to the server's
password first; it is not printed here):

  [sources.%[1]s-demo]
  dsn = "%[2]s"

  [sources.%[1]s-demo-rw]
  dsn = "%[2]s"
  write = true

  # No database in the DSN: every database is listed, as database.table
  [sources.%[1]s-demo-all]
  dsn = "%[3]s"

Then:  go run ./cmd/lode %[1]s-demo
`, scheme, redact(shop), redact(withDatabase(*dsn, "")))
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "mysqldemo:", err)
	os.Exit(1)
}

// load (re)creates the shop database and its reporting companion.
func load(ctx context.Context, db *sql.DB, shop string) error {
	reporting := shop + "_reporting"
	if shop == "" || strings.ContainsAny(shop, "`\x00") {
		return fmt.Errorf("invalid database name %q", shop)
	}

	for _, name := range []string{shop, reporting} {
		if err := mustBeOursOrAbsent(ctx, db, name); err != nil {
			return err
		}
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()

	for _, name := range []string{shop, reporting} {
		if _, err := conn.ExecContext(ctx, "DROP DATABASE IF EXISTS "+quote(name)); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "CREATE DATABASE "+quote(name)+" CHARACTER SET utf8mb4"); err != nil {
			return err
		}
		if _, err := conn.ExecContext(ctx, "CREATE TABLE "+quote(name)+"."+markerTable+" (created_by VARCHAR(40))"); err != nil {
			return err
		}
	}

	if _, err := conn.ExecContext(ctx, "USE "+quote(shop)); err != nil {
		return err
	}
	for _, stmt := range splitStatements(shopDDL) {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%s: %w", firstLine(stmt), err)
		}
	}

	data := demodata.Generate()
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []struct {
		name string
		rows [][]any
	}{
		{"customers", data.Customers},
		{"products", data.Products},
		{"orders", data.Orders},
		{"order_items", data.OrderItems},
		{"events", data.Events},
	} {
		if err := insertBatches(ctx, tx, t.name, t.rows); err != nil {
			return fmt.Errorf("%s: %w", t.name, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	_, err = conn.ExecContext(ctx, fmt.Sprintf(
		"CREATE VIEW %s.daily_events AS SELECT DATE(at) AS day, kind, count(*) AS events FROM %s.events GROUP BY DATE(at), kind",
		quote(reporting), quote(shop)))
	return err
}

// mustBeOursOrAbsent returns an error if database name exists but was not made by this command.
func mustBeOursOrAbsent(ctx context.Context, db *sql.DB, name string) error {
	var n int
	if err := db.QueryRowContext(ctx,
		"SELECT count(*) FROM information_schema.schemata WHERE schema_name = ?", name).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return nil
	}
	err := db.QueryRowContext(ctx, "SELECT count(*) FROM "+quote(name)+"."+markerTable).Scan(&n)
	if err != nil {
		return fmt.Errorf("database %q already exists and was not created by mysqldemo; refusing to replace it (use -database to pick another name)", name)
	}
	return nil
}

// insertBatches inserts rows in multi-row statements, which is far faster than one at a time.
func insertBatches(ctx context.Context, tx *sql.Tx, table string, rows [][]any) error {
	const batch = 500
	for start := 0; start < len(rows); start += batch {
		chunk := rows[start:min(start+batch, len(rows))]
		width := len(chunk[0])
		group := "(" + strings.TrimSuffix(strings.Repeat("?,", width), ",") + ")"
		query := "INSERT INTO " + table + " VALUES " + strings.TrimSuffix(strings.Repeat(group+",", len(chunk)), ",")
		args := make([]any, 0, len(chunk)*width)
		for _, row := range chunk {
			args = append(args, row...)
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return nil
}

// open connects with a URL-style DSN.
func open(dsn string) (*sql.DB, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return nil, fmt.Errorf("invalid DSN: %w", err)
	}
	if u.Scheme != "mysql" && u.Scheme != "mariadb" {
		return nil, errors.New("invalid DSN: scheme must be mysql:// or mariadb://")
	}
	cfg := driver.NewConfig()
	cfg.User = u.User.Username()
	cfg.Passwd, _ = u.User.Password()
	cfg.Net = "tcp"
	cfg.Addr = u.Host
	if u.Port() == "" {
		cfg.Addr += ":3306"
	}
	db, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect to %s: %w", u.Host, err)
	}
	return db, nil
}

// quote quotes a MySQL identifier.
func quote(s string) string { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }

func splitStatements(sqlText string) []string {
	var out []string
	for _, s := range strings.Split(sqlText, ";\n") {
		if s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), ";")); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func firstLine(s string) string { return strings.SplitN(strings.TrimSpace(s), "\n", 2)[0] }

func hostOf(dsn string) string {
	if u, err := url.Parse(dsn); err == nil {
		return u.Host
	}
	return "the server"
}

func schemeOf(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		return u.Scheme
	}
	return "mysql"
}

func withDatabase(dsn, database string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return dsn
	}
	u.Path = "/" + database
	return u.String()
}

// redact replaces the password with a ${LODE_DEMO_PASSWORD} reference, for lines shown in a
// terminal.
func redact(dsn string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		return "(unparseable)"
	}
	if _, has := u.User.Password(); !has {
		return u.String()
	}
	user := u.User.Username()
	u.User = url.User(user)
	return strings.Replace(u.String(), "//"+url.User(user).String()+"@", "//"+url.User(user).String()+":${LODE_DEMO_PASSWORD}@", 1)
}
