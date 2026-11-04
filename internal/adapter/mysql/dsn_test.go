package mysql

import (
	"strings"
	"testing"
	"time"
)

func TestConfigFromDSN(t *testing.T) {
	cfg, err := configFromDSN("mysql://reader:p%40ss:w%2Frd@db.example.com:3307/shop?tls=skip-verify&sql_mode=ANSI_QUOTES")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.User != "reader" || cfg.Passwd != "p@ss:w/rd" {
		t.Errorf("credentials = %q / %q, want them URL-decoded", cfg.User, cfg.Passwd)
	}
	if cfg.Net != "tcp" || cfg.Addr != "db.example.com:3307" || cfg.DBName != "shop" {
		t.Errorf("net/addr/db = %q %q %q", cfg.Net, cfg.Addr, cfg.DBName)
	}
	if cfg.TLSConfig != "skip-verify" {
		t.Errorf("tls = %q, want skip-verify", cfg.TLSConfig)
	}
	if cfg.Params["sql_mode"] != "ANSI_QUOTES" {
		t.Errorf("params = %v, want sql_mode passed through", cfg.Params)
	}
	if !cfg.ParseTime || cfg.Loc != time.UTC {
		t.Errorf("ParseTime=%v Loc=%v, want times parsed in UTC", cfg.ParseTime, cfg.Loc)
	}
}

func TestConfigFromDSNDefaults(t *testing.T) {
	cfg, err := configFromDSN("mariadb://root@/")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "localhost:3306" || cfg.DBName != "" || cfg.Passwd != "" {
		t.Errorf("addr=%q db=%q pw=%q, want localhost:3306 and no database or password", cfg.Addr, cfg.DBName, cfg.Passwd)
	}
}

func TestConfigFromDSNRejectsBadInput(t *testing.T) {
	for name, dsn := range map[string]string{
		"wrong scheme":    "postgres://u:hunter2@h/d",
		"bad port":        "mysql://u:hunter2@h:notaport/d",
		"control char":    "mysql://u:hunter2@h/d\x7f",
		"not a url":       "u:hunter2@tcp(h)/d",
		"missing colon":   "mysql//u:hunter2@h/d",
		"percent garbage": "mysql://u:hunter2@h/%zz",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := configFromDSN(dsn)
			if err == nil {
				t.Fatal("expected an error")
			}
			if strings.Contains(err.Error(), "hunter2") {
				t.Errorf("error leaks the password: %v", err)
			}
		})
	}
}

func TestQuoteIdentUsesBackticks(t *testing.T) {
	if got, want := quoteIdent("we`ird"), "`we``ird`"; got != want {
		t.Errorf("quoteIdent = %s, want %s", got, want)
	}
}
