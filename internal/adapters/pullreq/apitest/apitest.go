// Package apitest は、GitForge ごとの REST の呼び方（pullreq.API）を、ディレクトリの repository の参照（owner/name:パス）で
// 呼ぶ、GitHub と Forgejo に共通のテストである。テストからだけ使う。
//
// なぜ：ディレクトリの参照から :パス を外し忘れると、その GitForge では、ディレクトリの repository だけが動かない。
// 流れのテストでは、どのメソッドを通るかが流れの中身に左右されるので、ここでは、すべてのメソッドを 1 回ずつ呼ぶ。
// メソッドを足して、ここに足し忘れたら、テストが落ちる。
package apitest

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"testing"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
)

// Env は、呼ぶ前に用意しておくものである。
type Env struct {
	// Ref は、ディレクトリの repository の参照（owner/name:パス）である。
	Ref string
	// Merge と Close は、main から分かれた、まだ pull request のないブランチである。Merge は衝突せずに merge できるもの。
	Merge, Close string
	// Ctx は、repository に書く権限のある主体のトークンを持つ。pull request を作り、コメントの review を出し、merge する。
	Ctx context.Context
}

// step は、メソッドを 1 回呼ぶことである。name は、pullreq.API のメソッドの名前である。
type step struct {
	name string
	run  func() error
}

// Run は、pullreq.API のすべてのメソッドを、e.Ref で 1 回ずつ呼び、どれも誤りを返さないことを確かめる。
func Run(t *testing.T, api pullreq.API, e Env) {
	t.Helper()
	ctx, ref := e.Ctx, e.Ref
	var p, closing pullreq.Pull
	var c pullreq.Comment
	var is pullreq.Issue
	var attachment string
	const label = "logiforge/apitest"
	steps := []step{
		{"EnsureLabel", func() error {
			return api.EnsureLabel(ctx, ref, pullreq.Label{Name: label, Color: "#1d76db", Description: "テスト"})
		}},
		{"CreatePull", func() (err error) {
			if p, err = api.CreatePull(ctx, ref, e.Merge, "表のテスト", "本文"); err != nil {
				return err
			}
			closing, err = api.CreatePull(ctx, ref, e.Close, "閉じる", "本文")
			return err
		}},
		{"GetPull", func() error {
			got, err := api.GetPull(ctx, ref, p.Number)
			return want(err, got.Number == p.Number && got.Head == e.Merge, "GetPull が、作った pull request を返さない")
		}},
		{"ListPulls", func() error {
			ps, err := api.ListPulls(ctx, ref, "open")
			return want(err, slices.ContainsFunc(ps, func(x pullreq.Pull) bool { return x.Number == p.Number }), "ListPulls に、作った pull request がない")
		}},
		{"EditPullBody", func() error {
			if err := api.EditPullBody(ctx, ref, p.Number, "新しい本文"); err != nil {
				return err
			}
			got, err := api.GetPull(ctx, ref, p.Number)
			return want(err, got.Body == "新しい本文", "EditPullBody のあと、本文が変わっていない")
		}},
		{"CreateReview", func() error {
			_, err := api.CreateReview(ctx, ref, p.Number, ports.ReviewComment, "コメント", p.HeadSHA)
			return err
		}},
		{"ListReviews", func() error {
			rs, err := api.ListReviews(ctx, ref, p.Number)
			return want(err, len(rs) == 1, "ListReviews が、出した review を返さない")
		}},
		{"AddLabel", func() error {
			if err := api.AddLabel(ctx, ref, p.Number, label); err != nil {
				return err
			}
			got, err := api.GetPull(ctx, ref, p.Number)
			return want(err, slices.Contains(got.Labels, label), "AddLabel のあと、ラベルが付いていない")
		}},
		// RemoveLabel は、付いていないラベルの 404 を誤りにしないので、外れたことを読んで確かめる。
		{"RemoveLabel", func() error {
			if err := api.RemoveLabel(ctx, ref, p.Number, label); err != nil {
				return err
			}
			got, err := api.GetPull(ctx, ref, p.Number)
			return want(err, !slices.Contains(got.Labels, label), "RemoveLabel のあと、ラベルが付いたまま")
		}},
		{"ListEvents", func() error {
			evs, err := api.ListEvents(ctx, ref, p.Number)
			return want(err, slices.ContainsFunc(evs, func(x pullreq.Event) bool { return x.Kind == "labeled" && x.Label == label }), "ListEvents に、ラベルを付けたことがない")
		}},
		{"CreateComment", func() (err error) { c, err = api.CreateComment(ctx, ref, p.Number, "コメント"); return err }},
		{"ListComments", func() error {
			cs, err := api.ListComments(ctx, ref, p.Number)
			return want(err, slices.ContainsFunc(cs, func(x pullreq.Comment) bool { return x.ID == c.ID }), "ListComments に、書いたコメントがない")
		}},
		{"AttachmentsSupported", func() error { api.AttachmentsSupported(); return nil }},
		{"UploadAttachment", func() (err error) {
			attachment, err = api.UploadAttachment(ctx, ref, c.ID, "a.txt", []byte("A"))
			if !api.AttachmentsSupported() {
				return want(nil, errors.Is(err, pullreq.ErrAttachmentsUnsupported), "添付を持てない GitForge が、ErrAttachmentsUnsupported を返さない")
			}
			return err
		}},
		{"DownloadAttachment", func() error {
			got, err := api.DownloadAttachment(ctx, ref, c.ID, attachment)
			if !api.AttachmentsSupported() {
				return want(nil, errors.Is(err, pullreq.ErrAttachmentsUnsupported), "添付を持てない GitForge が、ErrAttachmentsUnsupported を返さない")
			}
			return want(err, string(got) == "A", "DownloadAttachment が、添付の中身を返さない")
		}},
		{"CreateIssue", func() (err error) {
			is, err = api.CreateIssue(ctx, ref, "依頼", "本文", []string{label})
			return err
		}},
		{"ListIssues", func() error {
			iss, err := api.ListIssues(ctx, ref)
			return want(err, slices.ContainsFunc(iss, func(x pullreq.Issue) bool { return x.Number == is.Number }), "ListIssues に、作った issue がない")
		}},
		{"GetIssue", func() error {
			got, err := api.GetIssue(ctx, ref, is.Number)
			return want(err, got.Number == is.Number, "GetIssue が、作った issue を返さない")
		}},
		{"CloseIssue", func() error { return api.CloseIssue(ctx, ref, is.Number) }},
		{"ClosePull", func() error { return api.ClosePull(ctx, ref, closing.Number) }},
		{"MergePull", func() error {
			_, err := api.MergePull(ctx, ref, p.Number, p.HeadSHA, "表のテスト", "反映")
			return err
		}},
		{"ClosedPullsPage", func() error {
			ps, _, err := api.ClosedPullsPage(ctx, ref, 1)
			return want(err, slices.ContainsFunc(ps, func(x pullreq.Pull) bool { return x.Number == p.Number && x.Merged }), "ClosedPullsPage に、merge した pull request がない")
		}},
	}

	check(t, reflect.TypeFor[pullreq.API](), steps)
	for _, s := range steps {
		if err := s.run(); err != nil {
			t.Fatalf("%s（%s）：%v", s.name, ref, err)
		}
	}
}

// want は、err がなく、ok なら nil を返す。
func want(err error, ok bool, msg string) error {
	if err != nil {
		return err
	}
	if !ok {
		return errors.New(msg)
	}
	return nil
}
