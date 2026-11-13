// Package ids は、ノードの識別子を扱う（design/files/layout.md の「識別子」）。
package ids

import (
	"crypto/rand"
	"regexp"
)

// crockford は Crockford の base32（小文字）の文字である。
const crockford = "0123456789abcdefghjkmnpqrstvwxyz"

// Kind は、識別子の種類の接頭辞である。
type Kind string

const (
	Hypothesis Kind = "h"
	Story      Kind = "s"
	Verdict    Kind = "v"
)

var (
	HypothesisPattern = regexp.MustCompile(`^h-[0-9a-hjkmnp-tv-z]{10}$`)
	StoryPattern      = regexp.MustCompile(`^s-[0-9a-hjkmnp-tv-z]{10}$`)
	VerdictPattern    = regexp.MustCompile(`^v-[0-9a-hjkmnp-tv-z]{10}$`)
	Sha256Pattern     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	// RepositoryNamePattern は repository の名前の形（design/api/README.md の「repository」）である。
	RepositoryNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)
)

// New は、種類の接頭辞と、乱数で決めた 10 文字をつないだ識別子を返す。
func New(kind Kind) string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	out := make([]byte, 10)
	for i, x := range b {
		out[i] = crockford[x&31]
	}
	return string(kind) + "-" + string(out)
}

// KindOf は、識別子の種類を返す。形が違えば空文字を返す。
func KindOf(id string) Kind {
	switch {
	case HypothesisPattern.MatchString(id):
		return Hypothesis
	case StoryPattern.MatchString(id):
		return Story
	case VerdictPattern.MatchString(id):
		return Verdict
	}
	return ""
}
