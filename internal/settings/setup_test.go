package settings

import (
	"path/filepath"
	"testing"
)

// Put は、送った設定だけを置き換え、ほかの秘密を消さない。FinishSetup は、記録の鍵を作り、コードを使えなくする。
func TestPutAndFinishSetup(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "s.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bk, _ := ParseKey(NewKey())
	sk, _ := ParseKey(NewKey())
	keys := Keys{Backend: bk, Shared: sk}
	ctx := t.Context()
	if err := db.Put(ctx, keys, map[string]string{"gitforge": "github", "github.client_secret": "s1", "github.client_id": "c1"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := db.Put(ctx, keys, map[string]string{"github.client_id": "c2"}, "alice"); err != nil {
		t.Fatal(err)
	}
	c, err := db.Load(ctx, keys)
	if err != nil || c.GitHub.ClientSecret != "s1" || c.GitHub.ClientID != "c2" {
		t.Fatalf("送っていない秘密が消えた：%+v %v", c.GitHub, err)
	}
	if err := db.Put(ctx, keys, map[string]string{"nope": "x"}, ""); err == nil {
		t.Fatal("ない設定を書けた")
	}
	if err := db.Put(ctx, Keys{Shared: sk}, map[string]string{"github.client_secret": "s"}, ""); err == nil {
		t.Fatal("バックエンドの鍵なしで、バックエンドだけの秘密を書けた")
	}
	code, err := db.NewSetupCode(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := db.CheckSetupCode(ctx, code); !ok {
		t.Fatal("コードが合わない")
	}
	if ok, _ := db.CheckSetupCode(ctx, "x"+code); ok {
		t.Fatal("違うコードが合った")
	}
	if err := db.FinishSetup(ctx, keys, "alice"); err != nil {
		t.Fatal(err)
	}
	if done, _ := db.SetupDone(ctx); !done {
		t.Fatal("終わらない")
	}
	if ok, _ := db.CheckSetupCode(ctx, code); ok {
		t.Fatal("終わったあとも、コードが合った")
	}
	if err := db.FinishSetup(ctx, keys, "alice"); err != ErrSetupDone {
		t.Fatalf("2 回目：%v", err)
	}
	if c, _ := db.Load(ctx, keys); len(c.RecordKeys) != 1 || c.RecordKeys[0].Seed == nil {
		t.Fatalf("記録の鍵：%+v", c.RecordKeys)
	}
}
