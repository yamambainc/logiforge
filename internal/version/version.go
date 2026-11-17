// Package version は、logiforge のバイナリの版である（logiforge version）。
//
// リリースのビルド（GoReleaser）は、-ldflags の -X で Version、Commit、Date を埋める（.goreleaser.yaml）。
// 埋めていないビルド（go build、go run）では、Go がバイナリに入れた VCS の情報（コミットと時刻）を使い、版は dev とする。
package version

import (
	"fmt"
	"runtime"
	"runtime/debug"
)

// リリースのビルドが、-ldflags "-X github.com/yamambainc/logiforge/internal/version.Version=..." で埋める。
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// Info は、版の情報である。
type Info struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
	Go      string `json:"go"`
	// Modified は、コミットしていない変更があるビルドか（VCS の情報から分かるときだけ）。
	Modified bool `json:"modified,omitempty"`
}

// Get は、版の情報を返す。
func Get() Info {
	i := Info{Version: Version, Commit: Commit, Date: Date, Go: runtime.Version()}
	if i.Commit != "" && i.Date != "" {
		return i
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, s := range bi.Settings {
			switch s.Key {
			case "vcs.revision":
				if i.Commit == "" {
					i.Commit = s.Value
				}
			case "vcs.time":
				if i.Date == "" {
					i.Date = s.Value
				}
			case "vcs.modified":
				i.Modified = s.Value == "true"
			}
		}
	}
	return i
}

// String は、1 行の版の文字列である。例：logiforge 0.1.0（コミット 0123abc、2026-11-17T01:00:00Z、go1.27.4 linux/amd64）
func (i Info) String() string {
	commit := i.Commit
	if len(commit) > 12 {
		commit = commit[:12]
	}
	if commit == "" {
		commit = "不明"
	}
	if i.Modified {
		commit += "（変更あり）"
	}
	date := i.Date
	if date == "" {
		date = "不明"
	}
	return fmt.Sprintf("logiforge %s（コミット %s、%s、%s %s/%s）", i.Version, commit, date, i.Go, runtime.GOOS, runtime.GOARCH)
}
