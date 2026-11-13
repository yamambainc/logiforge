//go:build unix

package worker

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// ErrAlreadyRunning は、ほかの worker が、同じディレクトリで動いていることを表す。
var ErrAlreadyRunning = errors.New("worker: ほかの worker が動いている")

// Lock は、clone のディレクトリに排他のロックファイルを取る。取れなければ ErrAlreadyRunning を返す。
// worker は 1 つだけ動かす（design/backend/worker.md）。起動のときに呼び、止まるまで持ち続ける。
func Lock(dir string) (release func(), err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(dir, ".worker.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrAlreadyRunning
		}
		return nil, fmt.Errorf("worker: ロックを取れない: %w", err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}, nil
}
