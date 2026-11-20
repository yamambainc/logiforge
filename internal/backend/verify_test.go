package backend

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/snapstore"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/store"
)

// 反映かの判定は、だれが merge したかでなく、中身で決める（design/backend/verify/README.md）。
func TestVerifyMerge(t *testing.T) {
	ctx := context.Background()
	f := live(t)
	st := f.b.Snap.(*snapstore.Adapter).Store
	verify := func(sha string) ports.MergeVerdict {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			m, err := store.GetMain(ctx, st, "q", sha)
			if err == nil {
				files, err := m.SnapshotTree.Files()
				if err != nil {
					t.Fatal(err)
				}
				parents := strings.Fields(f.r.git("rev-list", "--parents", "-n", "1", sha))[1:]
				v, err := f.b.VerifyMerge(ctx, "q", "o/q", sha, parents, parents[0], time.Now(), files, nil)
				if err != nil {
					t.Fatal(err)
				}
				return v
			}
			if time.Now().After(deadline) {
				t.Fatalf("worker が %s を取り込まない", sha)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	review := func(id, session string, kind ports.ReviewKind) {
		rs := f.forge.reviews[id]
		rs.Items = append(rs.Items, ports.Review{ID: "rev_" + session, Kind: kind, Revision: f.forge.ar(id).HeadRevision, Session: session})
		f.forge.reviews[id] = rs
	}

	// 根の仮説の背景情報に、1 行ずつ足す変更。
	lines := ""
	bg := func(line string) map[string][]byte {
		lines += line + "\n"
		return map[string][]byte{dH0 + "/background.md": []byte(lines)}
	}

	// Logiforge が reviewer のトークンで merge したもの。
	a := create(t, f, "va", "s3", bg("9"))
	if _, err := approve(t, f, a); err != nil {
		t.Fatal(err)
	}
	if v := verify(f.forge.ar(a).MergedRevision); !v.Authorized || v.ApplyRequest != a {
		t.Fatalf("reviewer の merge：%+v", v)
	}

	// 承認された apply request を、人が GitForge の画面で merge したもの（Logiforge を通さない）。
	b := create(t, f, "vb", "s4", bg("8"))
	review(b, "s9", ports.ReviewApprove)
	sha, err := f.forge.Merge(ctx, "o/q", b, f.forge.ar(b).HeadRevision, "画面から", "")
	if err != nil {
		t.Fatal(err)
	}
	if v := verify(sha); !v.Authorized || v.ApplyRequest != b {
		t.Fatalf("人の merge：%+v", v)
	}

	// approve のあとに request_changes が付いた apply request を、人が merge したもの。
	c := create(t, f, "vc", "s5", bg("7"))
	review(c, "s9", ports.ReviewApprove)
	review(c, "s8", ports.ReviewRequestChanges)
	sha, err = f.forge.Merge(ctx, "o/q", c, f.forge.ar(c).HeadRevision, "画面から", "")
	if err != nil {
		t.Fatal(err)
	}
	if v := verify(sha); v.Authorized || !strings.Contains(v.Detail, "approve でない") {
		t.Fatalf("request_changes のあとの merge：%+v", v)
	}

	// reviewer のセッションが、先頭のコミットを書いたセッションと同じ。
	d := create(t, f, "vd", "s6", bg("6"))
	review(d, "s6", ports.ReviewApprove)
	sha, err = f.forge.Merge(ctx, "o/q", d, f.forge.ar(d).HeadRevision, "画面から", "")
	if err != nil {
		t.Fatal(err)
	}
	if v := verify(sha); v.Authorized || !strings.Contains(v.Detail, "セッション") {
		t.Fatalf("同じセッション：%+v", v)
	}

	// merge のコミットに、apply request の差分のほかの変更を足したもの。
	e := create(t, f, "ve", "s7", bg("5"))
	review(e, "s9", ports.ReviewApprove)
	if _, err := f.forge.Merge(ctx, "o/q", e, f.forge.ar(e).HeadRevision, "画面から", ""); err != nil {
		t.Fatal(err)
	}
	f.r.write("extra.txt", []byte("x\n"))
	f.r.git("add", "-A")
	f.r.git("commit", "--quiet", "--amend", "--no-edit")
	sha = f.r.git("rev-parse", "main")
	f.forge.ar(e).MergedRevision = sha
	if v := verify(sha); v.Authorized || !strings.Contains(v.Detail, "同じでない") {
		t.Fatalf("足した変更：%+v", v)
	}

	// main に直接 push したもの（merge した pull request がない）。
	f.r.write("direct.txt", []byte("x\n"))
	sha = f.r.commit("直接")
	if v := verify(sha); v.Authorized || !v.Pending {
		t.Fatalf("直接の push：%+v", v)
	}
}
