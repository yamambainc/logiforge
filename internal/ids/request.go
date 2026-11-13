package ids

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"time"
)

// crockfordUpper は、Crockford の base32（大文字）の文字である。
const crockfordUpper = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// 要求で作るものの識別子の接頭辞（design/backend/ports/gitforge.md の「識別子」）。
const (
	PrefixApplyRequest = "apr_"
	PrefixReview       = "rev_"
	PrefixHumanRequest = "hrq_"
)

// encode130 は、b の先頭の 130 ビットを、26 文字の Crockford の base32 にする。
func encode130(b []byte) string {
	out := make([]byte, 26)
	for i := range out {
		bit := i * 5
		v := (uint16(b[bit/8])<<8 | uint16(b[bit/8+1])) >> (11 - bit%8) & 31
		out[i] = crockfordUpper[v]
	}
	return string(out)
}

// FromKey は、repository の名前・主体・Idempotency-Key から、識別子を決定的に作る（design/api/writes.md の「同じ要求の繰り返し」）。
// 3 つを改行でつないだ文字列の SHA-256 の、先頭の 130 ビットを、26 文字にする。
func FromKey(prefix, repo, principal, key string) string {
	sum := sha256.Sum256([]byte(repo + "\n" + principal + "\n" + key))
	return prefix + encode130(sum[:])
}

// NewULID は、時刻（ミリ秒）と乱数から、ULID の形の識別子を作る。Idempotency-Key のない要求で使う。
func NewULID(prefix string, now time.Time) string {
	var b [18]byte // 48 ビットの時刻 + 80 ビットの乱数 + 余り
	binary.BigEndian.PutUint64(b[0:8], uint64(now.UnixMilli())<<16)
	if _, err := rand.Read(b[6:16]); err != nil {
		panic(err)
	}
	// ULID は 128 ビットを 26 文字にする。先頭の 2 ビットを 0 にして、130 ビットとして符号化する。
	var u [18]byte
	for i := 0; i < 16; i++ {
		u[i] |= b[i] >> 2
		u[i+1] |= b[i] << 6
	}
	return prefix + encode130(u[:])
}
