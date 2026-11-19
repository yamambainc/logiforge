package settings

import (
	"context"
	"errors"
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
		Forgejo: ForgejoConfig{URL: "http://forgejo:3000", Org: "acme", WorkerLogin: "reader", WorkerToken: "wtok",
			OAuthClientID: "cid", OAuthClientSecret: "csecret"},
		Intervals:    Intervals{Patrol: "1s", List: "10s"},
		Repositories: []Repository{{Name: "q", GitForgeRef: "acme/app", Directory: "logiforge/q"}},
		RecordKeys:   []RecordKey{{ID: "k1", Secret: "0123456789abcdef0123456789abcdef-test"}},
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

// 読むだけの接続は、SQLite の側でも書けない（Go の確かめを通らずに、直接書こうとしても断られる）。
func TestReadOnlyConnectionCannotWrite(t *testing.T) {
	ctx := context.Background()
	d, p := openRW(t)
	if err := d.Save(ctx, testKeys(t), sample(), ""); err != nil {
		t.Fatal(err)
	}
	ro, err := Open(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if _, err := ro.db.ExecContext(ctx, `INSERT INTO settings (name, value, updated_at) VALUES ('x', 'y', 'z')`); err == nil {
		t.Fatal("読むだけの接続で、SQLite に書けた")
	}
	if _, err := ro.db.ExecContext(ctx, `UPDATE record_keys SET public_key = 'x'`); err == nil {
		t.Fatal("読むだけの接続で、記録の公開鍵を書き換えられた")
	}
}

// パスに ? や # があっても開ける（URI の中で、パスとして扱う）。
func TestPathWithSpecialCharacters(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a?b#c d.db")
	d, err := Open(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Save(context.Background(), testKeys(t), sample(), ""); err != nil {
		t.Fatal(err)
	}
	d.Close()
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("そのパスに作られていない：%v", err)
	}
}

// 記録の鍵の secret は、短い文字列を断る。
func TestShortRecordSecret(t *testing.T) {
	d, _ := openRW(t)
	c := sample()
	c.RecordKeys = []RecordKey{{ID: "k1", Secret: "short"}}
	if err := d.Save(context.Background(), testKeys(t), c, ""); err == nil {
		t.Fatal("短い secret で、記録の鍵を作れた")
	}
}

// 設定が足りないことは、ErrNotReady で見分けられる。
func TestNotReadyIsDistinct(t *testing.T) {
	if err := (&Config{}).Ready(); !errors.Is(err, ErrNotReady) {
		t.Fatalf("足りないことが ErrNotReady でない：%v", err)
	}
}

// worker のトークンは、共有の鍵で読める。書いても revision は進まない。
func TestWorkerTokens(t *testing.T) {
	ctx := context.Background()
	d, p := openRW(t)
	keys := testKeys(t)
	if err := d.Save(ctx, keys, sample(), ""); err != nil {
		t.Fatal(err)
	}
	r0, _ := d.Revision(ctx)
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	if err := d.PutWorkerToken(ctx, keys, WorkerToken{Owner: "acme", Token: "ghs_x", ExpiresAt: exp, Repositories: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if r1, _ := d.Revision(ctx); r1 != r0 {
		t.Fatalf("worker のトークンを書いて revision が進んだ：%d → %d", r0, r1)
	}
	ro, err := Open(p, true)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	got, ok, err := ro.WorkerTokenOf(ctx, Keys{Shared: keys.Shared}, "acme")
	if err != nil || !ok || got.Token != "ghs_x" || !got.ExpiresAt.Equal(exp) || len(got.Repositories) != 2 {
		t.Fatalf("worker が読めない：%+v %v %v", got, ok, err)
	}
	if _, ok, _ := ro.WorkerTokenOf(ctx, Keys{Shared: keys.Shared}, "other"); ok {
		t.Fatal("ない owner のトークンがあった")
	}
	if err := d.DeleteWorkerTokens(ctx, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := ro.WorkerTokenOf(ctx, Keys{Shared: keys.Shared}, "acme"); ok {
		t.Fatal("消したトークンが残っている")
	}
}

// WebUI のセッション：バックエンドの鍵で暗号化して置き、revision を進めない。rekey では消す。長く使われていないものは消す。
func TestWebUISessions(t *testing.T) {
	ctx := context.Background()
	d, p := openRW(t)
	keys := testKeys(t)
	if err := d.Save(ctx, keys, sample(), ""); err != nil {
		t.Fatal(err)
	}
	r0, _ := d.Revision(ctx)
	now := time.Now().UTC().Truncate(time.Second)
	s := WebUISession{IDHash: "h1", ClientID: "cid", AccessToken: "at", AccessExpires: now.Add(time.Hour), RefreshToken: "rt-secret", CreatedAt: now, UsedAt: now}
	if err := d.PutWebUISession(ctx, keys, s); err != nil {
		t.Fatal(err)
	}
	if r1, _ := d.Revision(ctx); r1 != r0 {
		t.Fatalf("セッションを書いて revision が進んだ：%d → %d", r0, r1)
	}
	got, ok, err := d.WebUISessionOf(ctx, keys, "h1")
	if err != nil || !ok || got != s {
		t.Fatalf("読んだもの：%+v %v %v", got, ok, err)
	}
	raw, _ := os.ReadFile(p)
	if strings.Contains(string(raw), "rt-secret") {
		t.Fatal("refresh token を平文で置いた")
	}
	// 共有の鍵だけ（worker）では読めない
	if _, _, err := d.WebUISessionOf(ctx, Keys{Shared: keys.Shared}, "h1"); err == nil {
		t.Fatal("バックエンドの鍵なしで読めた")
	}
	// 違う鍵では復号できない。知らないセッションとして扱い、消す（エラーにしない）
	if _, ok, err := d.WebUISessionOf(ctx, testKeys(t), "h1"); ok || err != nil {
		t.Fatalf("違う鍵：%v %v", ok, err)
	}
	if _, ok, _ := d.WebUISessionOf(ctx, keys, "h1"); ok {
		t.Fatal("復号できなかったセッションが残った")
	}
	_ = d.PutWebUISession(ctx, keys, s)
	old := WebUISession{IDHash: "h-old", ClientID: "cid", AccessExpires: now, CreatedAt: now, UsedAt: now.Add(-SessionMaxIdle - time.Hour)}
	if err := d.PutWebUISession(ctx, keys, old); err != nil {
		t.Fatal(err)
	}
	if err := d.PutWebUISession(ctx, keys, s); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := d.WebUISessionOf(ctx, keys, "h-old"); ok {
		t.Fatal("長く使われていないセッションが残った")
	}
	if err := d.DeleteWebUISession(ctx, "h1"); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := d.WebUISessionOf(ctx, keys, "h1"); ok {
		t.Fatal("消したのに残った")
	}
	_ = d.PutWebUISession(ctx, keys, s)
	if _, err := d.Rekey(ctx, keys, testKeys(t)); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := d.WebUISessionOf(ctx, keys, "h1"); ok {
		t.Fatal("rekey でセッションを消していない")
	}
}
