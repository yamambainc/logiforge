package backend

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/tree"
)

// 向き先が main でない apply request を、ノード 4,000 個のスナップショットツリーで重ねて読む時間を測る（issue #36。目安は 1 回で約 1 秒）。
//
//	LOGIFORGE_BENCH_OVERLAY=1 go test ./internal/backend/ -run TestBenchOverlayBranch -v
func TestBenchOverlayBranch(t *testing.T) {
	if os.Getenv("LOGIFORGE_BENCH_OVERLAY") == "" {
		t.Skip("LOGIFORGE_BENCH_OVERLAY=1 のときだけ測る")
	}
	ctx := context.Background()
	f := setup(t)
	// main：H2 の下に（識別子は 9 から始め、用意のノードと重ねない）、ストーリー 1 つと仮説 3 つずつを、幅優先でノード約 4,000 個まで足す。
	leaves := []string{dH2}
	nodes := 6
	for i := 0; nodes < 4000; i++ {
		parent := leaves[0]
		leaves = leaves[1:]
		sid := fmt.Sprintf("s-9%09d", i)
		sdir := parent + "/" + sid
		f.r.write(sdir+"/story.md", tree.MarshalStory(tree.StoryContent{ID: sid, Title: fmt.Sprintf("道筋 %d", i), Summary: strings.Repeat("概要", 20)}))
		for j := 0; j < 3; j++ {
			hid := fmt.Sprintf("h-9%07d%02d", i, j)
			hdir := sdir + "/" + hid
			f.r.write(hdir+"/hypothesis.md", tree.MarshalHypothesis(tree.HypothesisContent{ID: hid, Statement: fmt.Sprintf("仮説 %d-%d", i, j)}))
			f.r.write(hdir+"/background.md", []byte(strings.Repeat("背景の文章。", 50)+"\n"))
			leaves = append(leaves, hdir)
		}
		nodes += 4
	}
	f.r.commit("育てる")
	// develop：葉の 1 つに検証結果。b2（develop から）：同じ葉の背景情報を変える。
	leaf := leaves[len(leaves)-1]
	leafID := leaf[strings.LastIndex(leaf, "/")+1:]
	f.r.git("checkout", "--quiet", "-b", "develop", "main")
	f.r.write(leaf+"/verdicts/v-0000000001.md", tree.MarshalVerdict(tree.VerdictContent{ID: "v-0000000001", Value: true, Report: "r\n"}))
	f.r.commit("下\n\nLogiforge-Session-Id: s3")
	f.r.git("checkout", "--quiet", "-b", "b2")
	f.r.write(leaf+"/background.md", []byte("上\n"))
	top := f.r.commit("上\n\nLogiforge-Session-Id: s4")
	f.r.git("checkout", "--quiet", "main")
	syncFixture(t, f)
	f.forge.ars = append(f.forge.ars, ports.ApplyRequest{ID: "apr_3", Number: 3, Branch: "b2", Target: "develop", State: ports.StateOpen, FirstRevision: top, HeadRevision: top})

	at := At{ApplyRequest: "apr_3"}
	var ds []time.Duration
	for i := 0; i < 5; i++ {
		start := time.Now()
		n, err := f.b.GetNode(ctx, f.caller, "q", leafID, at)
		ds = append(ds, time.Since(start))
		if err != nil || n.Node.(*Hypothesis).Background != "上\n" {
			t.Fatalf("%+v %v", n, err)
		}
	}
	start := time.Now()
	l, err := f.b.ListNodes(ctx, f.caller, "q", at, "", -1)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ノード %d 個。getNode（apply_request 付き、向き先が main でない）：%v、listNodes：%v", len(l.Nodes), ds, time.Since(start))
}
