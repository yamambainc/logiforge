package settings

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func testKeys(t *testing.T) Keys {
	t.Helper()
	b, err := ParseKey(NewKey())
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseKey(NewKey())
	if err != nil {
		t.Fatal(err)
	}
	return Keys{Backend: b, Shared: s}
}

func sample() *Config {
	return &Config{
		GitForge: GitForgeForgejo,
		WebUIURL: "http://localhost:8080",
		Forgejo: ForgejoConfig{URL: "http://forgejo:3000", Org: "acme", UserTeam: "users", WorkerLogin: "reader", WorkerToken: "wtok",
			OAuthClientID: "cid", OAuthClientSecret: "csecret"},
		Intervals:    Intervals{Patrol: "1s", List: "10s"},
		Repositories: []Repository{{Name: "q", GitForgeRef: "acme/app", Directory: "logiforge/q"}},
		RecordKeys:   []RecordKey{{ID: "k1", Secret: "s1"}},
	}
}

func openRW(t *testing.T) (*DB, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "settings.db")
	d, err := Open(p, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d, p
}

func TestSaveLoad(t *testing.T) {
	ctx := context.Background()
	d, p := openRW(t)
	keys := testKeys(t)
	if err := d.Save(ctx, keys, sample(), "alice"); err != nil {
		t.Fatal(err)
	}
	got, err := d.Load(ctx, keys)
	if err != nil {
		t.Fatal(err)
	}
	if got.Forgejo.WorkerToken != "wtok" || got.Forgejo.OAuthClientSecret != "csecret" || len(got.RecordKeys) != 1 || len(got.RecordKeys[0].Seed) != 32 {
		t.Fatalf("読めない：%+v", got)
	}
	if err := got.Ready(); err != nil {
		t.Fatal(err)
	}
	// worker は、共有の鍵だけで、読むだけで開く。バックエンドだけの秘密と、記録の秘密鍵は読めない。
	ro, err := Open(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	w, err := ro.Load(ctx, Keys{Shared: keys.Shared})
	if err != nil {
		t.Fatal(err)
	}
	if w.Forgejo.WorkerToken != "wtok" || w.Forgejo.OAuthClientSecret != "" || w.RecordKeys[0].Seed != nil || len(w.RecordKeys[0].Public) != 32 {
		t.Fatalf("worker が読めるものが違う：%+v", w)
	}
	if err := ro.Save(ctx, Keys{Shared: keys.Shared}, sample(), ""); err == nil {
		t.Fatal("読むだけの DB に書けた")
	}
}

func TestSealedValueIsBoundToName(t *testing.T) {
	keys := testKeys(t)
	s := keys.Backend.seal("github.client_secret", []byte("x"))
	if _, err := keys.Backend.open("forgejo.oauth_client_secret", s); err == nil {
		t.Fatal("ほかの設定の名前で読めた")
	}
	if _, err := keys.Shared.open("github.client_secret", s); err != ErrWrongKey {
		t.Fatalf("ほかの鍵で読もうとしたときの誤りが違う：%v", err)
	}
}

func TestSecretsAreNotPlainInFile(t *testing.T) {
	ctx := context.Background()
	d, p := openRW(t)
	if err := d.Save(ctx, testKeys(t), sample(), ""); err != nil {
		t.Fatal(err)
	}
	d.Close()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"wtok", "csecret"} {
		if strings.Contains(string(b), s) {
			t.Fatalf("DB のファイルに %q が平文で残っている", s)
		}
	}
}

func TestRepositoryCannotChange(t *testing.T) {
	ctx := context.Background()
	d, _ := openRW(t)
	keys := testKeys(t)
	c := sample()
	if err := d.Save(ctx, keys, c, ""); err != nil {
		t.Fatal(err)
	}
	c.Repositories[0].GitForgeRef = "acme/other"
	if err := d.Save(ctx, keys, c, ""); err == nil {
		t.Fatal("登録した repository の gitforge_ref を変えられた")
	}
	// 外して、同じ組で登録し直すのはよい。違う組では、外したあとも登録できない。
	c = sample()
	c.Repositories = nil
	if err := d.Save(ctx, keys, c, ""); err != nil {
		t.Fatal(err)
	}
	c.Repositories = []Repository{{Name: "q", GitForgeRef: "acme/other"}}
	if err := d.Save(ctx, keys, c, ""); err == nil {
		t.Fatal("外した名前を、違う組で登録できた")
	}
	if err := d.Save(ctx, keys, sample(), ""); err != nil {
		t.Fatal(err)
	}
}

func TestRevisionAndRekey(t *testing.T) {
	ctx := context.Background()
	d, _ := openRW(t)
	keys := testKeys(t)
	r0, _ := d.Revision(ctx)
	if err := d.Save(ctx, keys, sample(), ""); err != nil {
		t.Fatal(err)
	}
	if r1, _ := d.Revision(ctx); r1 <= r0 {
		t.Fatalf("書いても revision が進まない：%d → %d", r0, r1)
	}
	next := testKeys(t)
	n, err := d.Rekey(ctx, keys, next)
	if err != nil || n != 3 {
		t.Fatalf("rekey：%d %v", n, err)
	}
	if n, err := d.Rekey(ctx, keys, next); err != nil || n != 0 {
		t.Fatalf("2 度目の rekey：%d %v", n, err)
	}
	got, err := d.Load(ctx, next)
	if err != nil || got.Forgejo.OAuthClientSecret != "csecret" || len(got.RecordKeys[0].Seed) != 32 {
		t.Fatalf("新しい鍵で読めない：%+v %v", got, err)
	}
	if _, err := d.Load(ctx, keys); err == nil {
		t.Fatal("古い鍵で読めた")
	}
}

// TestReadOnlyWhileWriting は、バックエンドが書いている最中も、worker が読むだけの接続で、書く前か後のどちらかをまとめて読めることを確かめる。
func TestReadOnlyWhileWriting(t *testing.T) {
	ctx := context.Background()
	d, p := openRW(t)
	keys := testKeys(t)
	first := sample()
	first.Intervals.List = first.Intervals.Patrol
	if err := d.Save(ctx, keys, first, ""); err != nil {
		t.Fatal(err)
	}
	ro, err := Open(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	stop := time.Now().Add(time.Second)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; time.Now().Before(stop); i++ {
			c := sample()
			c.Intervals.Patrol = []string{"1s", "2s"}[i%2]
			c.Intervals.List = c.Intervals.Patrol
			if err := d.Save(ctx, keys, c, ""); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	for time.Now().Before(stop) {
		c, err := ro.Load(ctx, Keys{Shared: keys.Shared})
		if err != nil {
			t.Fatal(err)
		}
		if c.Intervals.Patrol != c.Intervals.List {
			t.Fatalf("書きかけを読んだ：%q %q", c.Intervals.Patrol, c.Intervals.List)
		}
	}
	wg.Wait()
}

func TestEmptyDBForWorker(t *testing.T) {
	p := filepath.Join(t.TempDir(), "settings.db")
	// バックエンドが一度も開いていない DB（空のファイル）を、worker が読むだけで開く。
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ro, err := Open(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	c, err := ro.Load(context.Background(), Keys{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Ready() == nil {
		t.Fatal("空の設定が、そろっているとされた")
	}
}
