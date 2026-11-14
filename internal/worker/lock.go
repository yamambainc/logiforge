package worker

import "errors"

// ErrAlreadyRunning は、ほかの worker が、同じディレクトリで動いていることを表す。
var ErrAlreadyRunning = errors.New("worker: ほかの worker が動いている")

// LockAll は、dirs のそれぞれに、排他のロックを取る。1 つでも取れなければ、取ったものを返して誤りを返す。
// clone のディレクトリと、手元のディレクトリの置き場の根に使う。置き場を共有する 2 つ目の worker も、起動しないようにするため。
func LockAll(dirs ...string) (release func(), err error) {
	var releases []func()
	release = func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	for _, d := range dirs {
		r, err := Lock(d)
		if err != nil {
			release()
			return nil, err
		}
		releases = append(releases, r)
	}
	return release, nil
}
