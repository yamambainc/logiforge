package pullreq

import (
	"errors"
	"strings"
	"testing"
)

var ring = mustRing([]string{"k2", "k1"}, []string{"new", "old"})

func mustRing(ids, secrets []string) Keyring {
	k, err := KeyringFromSecrets(ids, secrets)
	if err != nil {
		panic(err)
	}
	return k
}

var here = Scope{RepoID: "id-1", Number: 1}

func TestRecordRoundTrip(t *testing.T) {
	r := Record{Body: "理由です。\r\n\r\n2 段落目\n\n", Trailers: []Trailer{{KeyRecord, RecordClose}, {KeySession, "s\n1"}}}
	text, err := ring.Format(r, here)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ring.Parse(text+"\n", here)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body != "理由です。\n\n2 段落目" || got.Get(KeySession) != "s 1" || got.Get(KeyRecord) != RecordClose {
		t.Fatalf("got %+v", got)
	}
	if r.Trailers[1].Value != "s\n1" {
		t.Fatal("Format が、渡した記録を書き換えた")
	}
}

func TestRecordTampered(t *testing.T) {
	text, _ := ring.Format(Record{Body: "回答", Trailers: []Trailer{{KeyRecord, RecordAnswer}, {KeyAttachment, "a.png sha256:00"}}}, here)
	cases := map[string]string{
		"本文を変えた":       strings.Replace(text, "回答", "別の回答", 1),
		"trailer を足した": strings.Replace(text, KeyMac, KeyAttachment+": b.png sha256:11\n"+KeyMac, 1),
		"知らない鍵":        strings.Replace(text, "k2:", "k9:", 1),
	}
	for name, s := range cases {
		if _, err := ring.Parse(s, here); !errors.Is(err, ErrBadMac) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	// ほかの場所に写した記録は、MAC が合わない。
	for _, at := range []Scope{{RepoID: "id-1", Number: 2}, {RepoID: "id-2", Number: 1}} {
		if _, err := ring.Parse(text, at); !errors.Is(err, ErrBadMac) {
			t.Fatalf("%+v に写した記録を読んだ：%v", at, err)
		}
	}
	// 古い鍵で書いたものも確かめられる。
	old := Keyring{Current: "k1", Private: ring.Private, Public: ring.Public}
	text, _ = old.Format(Record{Trailers: []Trailer{{KeyRecord, RecordClose}}}, here)
	if _, err := ring.Parse(text, here); err != nil {
		t.Fatal(err)
	}
	if _, err := ring.Parse("ただのコメント", here); !errors.Is(err, ErrNoRecord) {
		t.Fatal(err)
	}
}
