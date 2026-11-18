package ports

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// PermissionTTL は、権限を問い合わせた結果を持つ長さである。
// なぜ 60 秒：GitForge で権限を外したあと、Logiforge で使えなくなるまでの遅れを、1 分までに収めるため
// （design/settings/users.md の「権限のキャッシュ」）。
const PermissionTTL = 60 * time.Second

// PermCache は、トークンごとの、権限の問い合わせの結果のキャッシュである。鍵は、トークンの SHA-256 と、問いの名前である。
// トークンそのものは持たない。
type PermCache struct {
	mu      sync.Mutex
	entries map[string]permEntry
	// Now は、時刻を返す。nil なら time.Now（テストで差し替える）。
	Now func() time.Time
}

type permEntry struct {
	v  any
	at time.Time
}

func (c *PermCache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

func permKey(token, q string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:]) + "\x00" + q
}

// Get は、持っている結果を返す。古ければ、ないものとする。
func (c *PermCache) Get(token, q string) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[permKey(token, q)]
	if !ok || c.now().Sub(e.at) >= PermissionTTL {
		return nil, false
	}
	return e.v, true
}

// Put は、結果を持つ。古いものを、ついでに捨てる。
func (c *PermCache) Put(token, q string, v any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.entries == nil {
		c.entries = map[string]permEntry{}
	}
	if len(c.entries) > 1024 {
		for k, e := range c.entries {
			if now.Sub(e.at) >= PermissionTTL {
				delete(c.entries, k)
			}
		}
	}
	c.entries[permKey(token, q)] = permEntry{v: v, at: now}
}
