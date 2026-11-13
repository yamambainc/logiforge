//go:build !unix

package worker

import (
	"errors"
	"os"
	"path/filepath"
)

// Lock は、unix でない環境では、ロックファイルを O_EXCL で作る。止まり方によっては、ファイルが残るので、
// 残ったときは、ほかの worker が動いていないことを確かめてから消す。
func Lock(dir string) (release func(), err error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	p := filepath.Join(dir, ".worker.lock")
	f, err := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrExist) {
		return nil, ErrAlreadyRunning
	}
	if err != nil {
		return nil, err
	}
	f.Close()
	return func() { _ = os.Remove(p) }, nil
}
