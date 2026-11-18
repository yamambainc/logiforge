package worker

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// HeartbeatFile は、clone のディレクトリに置く、ハートビートのファイルの名前である（design/backend/worker/operation.md の「ハートビート」）。
const HeartbeatFile = "heartbeat"

// stallFactor は、見回りの間隔の何倍のあいだハートビートが書かれなければ、止まっているとみなすかである。
// GC や、ディスクの一時的な遅れで、1 回の見回りが少し遅れただけでは、再起動しないようにするため。
const stallFactor = 10

// WriteHeartbeat は、dir のハートビートのファイルに、now を書く。一時ファイルに書いてから名前を付け替える。
func WriteHeartbeat(dir string, now time.Time) error {
	tmp, err := os.CreateTemp(dir, HeartbeatFile+".*")
	if err != nil {
		return err
	}
	if _, err := tmp.WriteString(now.UTC().Format(time.RFC3339Nano) + "\n"); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, HeartbeatFile))
}

// CheckHeartbeat は、dir のハートビートが、見回りの間隔の stallFactor 倍より古くないかを確かめる。
// `logiforge worker-health` が使う。古いか、読めなければ、誤りを返す。
func CheckHeartbeat(dir string, interval time.Duration, now time.Time) error {
	if interval == 0 {
		interval = DefaultInterval
	}
	b, err := os.ReadFile(filepath.Join(dir, HeartbeatFile))
	if err != nil {
		return fmt.Errorf("ハートビートを読めない：%w", err)
	}
	at, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(b)))
	if err != nil {
		return fmt.Errorf("ハートビートの形が正しくない：%w", err)
	}
	if age := now.Sub(at); age > stallFactor*interval {
		return fmt.Errorf("ハートビートが %v 前で、見回りの間隔の %d 倍（%v）より古い", age.Round(time.Millisecond), stallFactor, stallFactor*interval)
	}
	return nil
}
