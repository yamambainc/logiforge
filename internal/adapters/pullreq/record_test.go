package pullreq

import (
	"errors"
	"strings"
	"testing"
)

var ring = Keyring{Current: "k2", Keys: map[string][]byte{"k1": []byte("old"), "k2": []byte("new")}}

func TestRecordRoundTrip(t *testing.T) {
	r := Record{Body: "理由です。\n\n2 段落目", Trailers: []Trailer{{KeyRecord, RecordClose}, {KeySession, "s\n1"}}}
	text, err := ring.Format(r)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ring.Parse(text + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != r.Body || got.Get(KeySession) != "s 1" || got.Get(KeyRecord) != RecordClose {
		t.Fatalf("got %+v", got)
	}
	if !strings.Contains(text, KeyMac+": k2:") {
		t.Fatal(text)
	}
}

func TestRecordTampered(t *testing.T) {
	text, _ := ring.Format(Record{Body: "回答", Trailers: []Trailer{{KeyRecord, RecordAnswer}, {KeyAttachment, "a.png sha256:00"}}})
	cases := map[string]string{
		"本文を変えた":       strings.Replace(text, "回答", "別の回答", 1),
		"trailer を足した": strings.Replace(text, KeyMac, KeyAttachment+": b.png sha256:11\n"+KeyMac, 1),
		"知らない鍵":        strings.Replace(text, "k2:", "k9:", 1),
	}
	for name, s := range cases {
		if _, err := ring.Parse(s); !errors.Is(err, ErrBadMac) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// 古い鍵で書いたものも確かめられる。
	old := Keyring{Current: "k1", Keys: ring.Keys}
	text, _ = old.Format(Record{Trailers: []Trailer{{KeyRecord, RecordClose}}})
	if _, err := ring.Parse(text); err != nil {
		t.Fatal(err)
	}
	if _, err := ring.Parse("ただのコメント"); !errors.Is(err, ErrNoRecord) {
		t.Fatal(err)
	}
}
