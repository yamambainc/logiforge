package version

import (
	"strings"
	"testing"
)

// リリースのビルドが埋めた値を、そのまま返す。
func TestEmbedded(t *testing.T) {
	defer func(v, c, d string) { Version, Commit, Date = v, c, d }(Version, Commit, Date)
	Version, Commit, Date = "0.1.0", "0123456789abcdef0123", "2026-11-17T01:00:00Z"
	i := Get()
	if i.Version != "0.1.0" || i.Commit != "0123456789abcdef0123" || i.Date != "2026-11-17T01:00:00Z" || i.Go == "" {
		t.Fatalf("%+v", i)
	}
	if s := i.String(); !strings.HasPrefix(s, "logiforge 0.1.0（コミット 0123456789ab、2026-11-17T01:00:00Z、go") {
		t.Fatal(s)
	}
}

// 埋めていないビルドは、版を dev とする。
func TestDev(t *testing.T) {
	if i := Get(); i.Version != "dev" || !strings.Contains(i.String(), "logiforge dev") {
		t.Fatalf("%+v", i)
	}
}
