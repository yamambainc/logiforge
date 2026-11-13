package ids

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

var form = regexp.MustCompile(`^[a-z]+_[0-9A-HJKMNP-TV-Z]{26}$`)

func TestFromKey(t *testing.T) {
	a := FromKey(PrefixApplyRequest, "q", "alice", "k1")
	if a != FromKey(PrefixApplyRequest, "q", "alice", "k1") || !form.MatchString(a) || !strings.HasPrefix(a, "apr_") {
		t.Fatalf("got %q", a)
	}
	for _, other := range []string{FromKey(PrefixApplyRequest, "q", "bob", "k1"), FromKey(PrefixApplyRequest, "q", "alice", "k2"), FromKey(PrefixApplyRequest, "r", "alice", "k1")} {
		if other == a {
			t.Fatal("違う要求で、同じ識別子になった")
		}
	}
}

func TestNewULIDSortsByTime(t *testing.T) {
	t0 := time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)
	a, b := NewULID(PrefixReview, t0), NewULID(PrefixReview, t0.Add(time.Second))
	if !form.MatchString(a) || !(a < b) || a == NewULID(PrefixReview, t0) {
		t.Fatalf("%s %s", a, b)
	}
}
