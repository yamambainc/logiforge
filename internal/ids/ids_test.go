package ids

import (
	"strings"
	"testing"
)

func TestNew(t *testing.T) {
	for range 200 {
		id := New(Hypothesis)
		if !HypothesisPattern.MatchString(id) {
			t.Fatalf("形が違う: %s", id)
		}
		if KindOf(id) != Hypothesis {
			t.Fatalf("種類が違う: %s", id)
		}
	}
}

func TestNewDoesNotUseAmbiguousLetters(t *testing.T) {
	var sb strings.Builder
	for range 500 {
		sb.WriteString(New(Story)[2:])
	}
	if strings.ContainsAny(sb.String(), "ilou") {
		t.Fatal("i、l、o、u を使っている")
	}
}

func TestKindOfRejectsMalformed(t *testing.T) {
	for _, id := range []string{"h-short", "x-0123456789", "h-0123456789x", "h-ABCDEFGHJK"} {
		if KindOf(id) != "" {
			t.Errorf("%s に種類がある", id)
		}
	}
}
