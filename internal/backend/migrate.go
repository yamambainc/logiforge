package backend

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/yamambainc/logiforge/internal/ids"
	"github.com/yamambainc/logiforge/internal/ports"
	"github.com/yamambainc/logiforge/internal/snapshot"
	"github.com/yamambainc/logiforge/internal/tree"
)

// 別リポジトリ方式から移す（design/directory/migrate.md）。

// migratedFrom は、repository が別リポジトリ方式から移したものなら、元の repository と移したコミットを返す。
// 版の 1 番（用意のコミット）の trailer Logiforge-Migrated-From: を、元の repository の migrated_to と突き合わせて決める。
// trailer はだれでも書けるので、worker が置き場で確かめて書いた元の migrated_to が、この repository のときだけ返す（design/directory/migrate.md）。
func (b *Backend) migratedFrom(ctx context.Context, repo string) (*MigratedFrom, error) {
	sha, err := b.Snap.MainAt(ctx, repo, 1)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c, err := b.Snap.Commit(ctx, repo, sha)
	if errors.Is(err, ports.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	v := trailerValue(c.Trailers, snapshot.TrailerMigratedFrom)
	name, commit, ok := strings.Cut(v, " ")
	if !ok || name == "" || commit == "" {
		return nil, nil
	}
	if to, err := b.Snap.MigratedTo(ctx, name); err != nil || to != repo {
		return nil, err
	}
	return &MigratedFrom{Repository: name, Commit: commit}, nil
}

// notMigrated は、repository がほかの repository に移した元なら、書く操作を not_open で断る（読むことだけができる）。
func (b *Backend) notMigrated(ctx context.Context, repo string) error {
	to, err := b.Snap.MigratedTo(ctx, repo)
	if err != nil {
		return err
	}
	if to != "" {
		return fail(CodeNotOpen, "repository %s は %s に移したので、読むことだけができる", repo, to)
	}
	return nil
}

// MigrateRepository は、別リポジトリ方式の repository from の、今の main のスナップショットツリーを、ディレクトリの repository name に移す
// （design/directory/migrate.md の「手順」）。移すのは tree/ と artifacts/ だけで、用意のコミットとして置く。
func (b *Backend) MigrateRepository(ctx context.Context, c Caller, name, from string) (Written[Repository], error) {
	var out Written[Repository]
	if err := requireUser(c); err != nil {
		return out, err
	}
	if !ids.RepositoryNamePattern.MatchString(name) {
		return out, fail(CodeValidationFailed, "repository の名前は、小文字の英数字とハイフンで、63 文字以下")
	}
	ref, err := b.Forge.GitForgeRef(ctx, name)
	if errors.Is(err, ports.ErrNotRegistered) {
		return out, fail(CodeRepositoryNotRegistered, "repository %q は、サーバの設定に登録されていない", name)
	}
	if err != nil {
		return out, err
	}
	if err := b.requireUserOn(ctx, c, name); err != nil {
		return out, err
	}
	if _, dir := ports.SplitRef(ref); dir == "" {
		return out, fail(CodeValidationFailed, "移した先 %s は、ディレクトリの repository として登録されていない", name)
	}
	srcRef, err := b.access(ctx, c, from)
	if err != nil {
		return out, err
	}
	if _, dir := ports.SplitRef(srcRef); dir != "" {
		return out, fail(CodeValidationFailed, "元の repository %s は、別リポジトリ方式の repository でない", from)
	}
	// 送り直し：すでに移していれば、移した先を返す。
	if done, err := b.Snap.MigratedTo(ctx, from); err != nil {
		return out, err
	} else if done == name {
		repo, err := b.GetRepository(ctx, c, name)
		out.Value = repo
		return out, err
	} else if done != "" {
		return out, fail(CodeValidationFailed, "元の repository %s は、すでに %s に移した", from, done)
	}
	src, err := b.currentMain(ctx, from)
	if err != nil {
		return out, err
	}
	// 移すのは、元の GitForge の main の先頭の木である。worker がまだ取り込んでいなければ（archive の直前の反映など）、待ってもらう。
	if head, err := b.Forge.MainHead(ctx, srcRef); err != nil {
		return out, err
	} else if head != src.revision() {
		return out, fail(CodeSnapshotPending, "元の repository の main %s を、worker がまだ取り込んでいない。少し待って送り直す", head)
	}
	var problems []string
	if broken, err := b.Snap.Broken(ctx, from); err != nil {
		return out, err
	} else if broken != nil {
		problems = append(problems, "元の repository が壊れている")
	}
	ars, err := b.Forge.ListApplyRequests(ctx, srcRef, "open")
	if err != nil {
		return out, err
	}
	for _, ar := range ars {
		if ar.State == ports.StateOpen {
			problems = append(problems, "open の apply request "+ar.ID+" がある")
		}
	}
	hrs, err := b.Forge.ListHumanRequests(ctx, srcRef)
	if err != nil {
		return out, err
	}
	for _, hr := range hrs {
		if h := b.toHumanRequest(from, hr); h.State == "open" {
			problems = append(problems, "open の人への依頼 "+hr.ID+" がある")
		}
	}
	archived, err := b.Forge.Archived(ctx, srcRef)
	if err != nil {
		return out, err
	}
	if !archived {
		problems = append(problems, "元の git リポジトリが archive でない（repository の admin が、先に archive にする）")
	}
	if len(problems) > 0 {
		return out, fail(CodeValidationFailed, "移せない：%s", strings.Join(problems, "、"))
	}
	// tree/ と artifacts/ のファイルを集める。artifact の中身は、置き場から読む。
	var files []ports.File
	for _, p := range sortedKeys(src.files) {
		if !strings.HasPrefix(p, tree.TreeDir+"/") && !strings.HasPrefix(p, "artifacts/") {
			continue
		}
		f := src.files[p]
		content := f.Content
		if content == nil && f.SHA256 != "" {
			if content, err = b.Snap.Artifact(ctx, from, f.SHA256); err != nil {
				return out, fmt.Errorf("artifact %s: %w", p, err)
			}
		}
		files = append(files, ports.File{Path: p, Content: content})
	}
	message := fmt.Sprintf("repository %s から移した\n\nLogiforge-Init: 1\n%s: %s %s", from, snapshot.TrailerMigratedFrom, from, src.revision())
	if err := b.Forge.SetupRepository(ctx, ref, files, message); err != nil && !errors.Is(err, ports.ErrMainExists) {
		return out, err
	}
	if !b.waitLatest(ctx, name, func(l string) bool { return l != "" }) {
		_, dir := ports.SplitRef(ref)
		out.Value = Repository{Name: name, GitURL: b.Forge.GitURL(ref), Directory: dir}
		out.Pending = "main"
		return out, nil
	}
	mf, err := b.migratedFrom(ctx, name)
	if err != nil {
		return out, err
	}
	if mf == nil || mf.Repository != from {
		return out, fail(CodeRepositoryExists, "ディレクトリ %s には、すでに別の repository がある", ref)
	}
	repo, err := b.GetRepository(ctx, c, name)
	out.Value = repo
	return out, err
}
