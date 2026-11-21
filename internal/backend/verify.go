package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/store"
	"github.com/yamambainc/logiforge/internal/tree"
)

// ErrReviewUnverified は、決める review に、書く権限を持つかを読めないものがあり、反映かを決められないことを表す。worker は、次の見回りでやり直す。
var ErrReviewUnverified = errors.New("書く権限を持つかを確かめられない review がある")

// VerifyMerge は、main のコミット sha（親は parents、最初の親が 1 つ前の main のコミット）が、
// apply request の反映によるものかを、中身だけで決める（design/backend/verify/README.md の「apply request の反映のコミット」）。
// files は sha の木、broken は 1 つ前の main までの壊れていることの記録である。worker が呼ぶ。
// 1 つ前の main のコミットのスナップショットツリーと、先頭のコミットのスナップショットは、置き場から読む。
//
// prevVersion は、1 つ前の版（git リポジトリ全体の repository では parents[0] と同じ。ディレクトリの repository では、
// ディレクトリを変えた 1 つ前の main のコミット）で、その版のスナップショットツリーに重ねる（design/directory/history.md の「反映かの判定」）。
//
// committed は、sha の committer date で、反映した pull request の一覧を読むのをやめる位置に使う（design/backend/list-reads.md）。
func (b *Backend) VerifyMerge(ctx context.Context, repo, ref, sha string, parents []string, prevVersion string, committed time.Time, files tree.Files, broken *store.Broken) (ports.MergeVerdict, error) {
	no := func(format string, a ...any) (ports.MergeVerdict, error) {
		return ports.MergeVerdict{Detail: fmt.Sprintf(format, a...)}, nil
	}
	if len(parents) == 0 {
		return no("根のコミットでないのに、親がない")
	}
	// 1. C を merge した pull request。
	ar, err := b.Forge.MergedApplyRequest(ctx, ref, sha, committed)
	if errors.Is(err, ports.ErrNotFound) {
		return ports.MergeVerdict{Pending: true, Detail: "merge した pull request が見つからない"}, nil
	}
	if err != nil {
		return ports.MergeVerdict{}, err
	}
	h := ar.HeadRevision
	// 2. C の形。squash merge か、merge のコミット。
	switch {
	case len(parents) == 1:
	case len(parents) == 2 && parents[1] == h:
	default:
		return no("コミットの形が、squash merge でも、先頭のコミット %s との merge のコミットでもない", h)
	}
	// 3. その pull request の review（その場で読む）。
	d, err := b.applyRequest(ctx, repo, ref, ar, readVerify, nil)
	if err != nil {
		return ports.MergeVerdict{}, err
	}
	if d.branch == nil {
		return ports.MergeVerdict{MissingHead: h}, nil
	}
	if d.current.Commit != h {
		return no("pull request の先頭のコミット %s が、apply request %s の先頭のコミットの一覧の最後 %s でない", h, ar.ID, d.current.Commit)
	}
	// 4. レビュー。決める review に、書く権限を持つかを読めないものがあれば、API が答えないときと同じに、決めずに次の見回りでやり直す。
	// なぜ：読めないものを数えないまま「反映によるものでない」とすると、読めなかっただけで repository を壊れた状態にしてしまうから。
	// Pending にしないのは、Pending は、pull request が見つからないときの待ち時間を過ぎると、反映によらないと決めるからである。
	if d.reviews.Unverified > 0 {
		return ports.MergeVerdict{}, fmt.Errorf("%w：apply request %s に、書く権限を持つかを確かめられない review が %d 個ある", ErrReviewUnverified, ar.ID, d.reviews.Unverified)
	}
	if p := approvalProblem(d); p != "" {
		return no("apply request %s：%s", ar.ID, p)
	}
	// 5. P の木に H の差分を重ね、C の木と同じか。
	onto, err := b.mainAt(ctx, repo, prevVersion)
	if err != nil {
		return ports.MergeVerdict{}, err
	}
	head, conflicts, err := snapshot.Overlay(onto.files, d.branch.Diff)
	if err != nil {
		return ports.MergeVerdict{}, err
	}
	if len(conflicts) > 0 {
		return no("apply request %s の差分を、1 つ前の main に重ねられない：%s", ar.ID, strings.Join(conflicts, "、"))
	}
	if !tree.SameFiles(head, files) {
		return no("木が、1 つ前の main に apply request %s の差分を重ねたものと同じでない", ar.ID)
	}
	ok := ports.MergeVerdict{Authorized: true, ApplyRequest: ar.ID, Kind: ar.Kind, MergedHead: h}
	// 6. 直すための apply request は、直す条件を確かめる。ほかは、手順 3 の検査を、P を今の main として行う。
	if ar.Kind != "" {
		problem, err := b.repairProblem(ctx, repo, ar.Kind, d.branch, onto, broken)
		if err != nil {
			return ports.MergeVerdict{}, err
		}
		if problem != "" {
			return no("直すための apply request %s：%s", ar.ID, problem)
		}
		return ok, nil
	}
	if broken != nil {
		return no("壊れているあいだに、直すための apply request でない %s が反映された", ar.ID)
	}
	ops, _, err := b.operations(ctx, repo, d.branch)
	if err != nil {
		return ports.MergeVerdict{}, err
	}
	reason, detail, err := b.changeProblem(ctx, onto, d.branch, ops)
	if err != nil {
		return ports.MergeVerdict{}, err
	}
	if reason != "" {
		return no("apply request %s：%s（%s）", ar.ID, detail, reason)
	}
	return ok, nil
}
