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

// ReposEnv は、RunRepos を呼ぶ前に用意しておくものである。
type ReposEnv struct {
	// Name と Ref は、設定に登録した、ディレクトリの repository の名前と参照（owner/name:パス）である。
	// owner/name の git リポジトリには、main があり、まだそのディレクトリはない。
	Name, Ref string
	// Files は、用意のコミットに置くファイルである（ディレクトリからの相対パス）。
	Files []ports.File
	// Ctx は、git リポジトリに書く権限のある主体のトークンを持つ。
	Ctx context.Context
}

// RunRepos は、pullreq.Repos のすべてのメソッドを、e.Ref で 1 回ずつ呼び、git リポジトリの参照（owner/name）で呼んだのと
// 同じ git リポジトリを指していることを確かめる。メソッドを足して、ここに足し忘れたら、テストが落ちる。
func RunRepos(t *testing.T, r pullreq.Repos, e ReposEnv) {
	t.Helper()
	ctx, ref := e.Ctx, e.Ref
	git, dir := ports.SplitRef(ref)
	if dir == "" {
		t.Fatalf("ディレクトリの参照でない：%s", ref)
	}
	steps := []step{
		{"ListRepositories", func() error {
			rs, err := r.ListRepositories(ctx)
			return want(err, slices.Contains(rs, ports.Repository{Name: e.Name, GitForgeRef: ref}), "ListRepositories に、ディレクトリの repository がない")
		}},
		{"GitForgeRef", func() error {
			got, err := r.GitForgeRef(ctx, e.Name)
			return want(err, got == ref, "GitForgeRef が、ディレクトリの参照を返さない："+got)
		}},
		{"GitURL", func() error {
			return want(nil, r.GitURL(ref) == r.GitURL(git), "GitURL が、git リポジトリの URL でない："+r.GitURL(ref))
		}},
		{"Archived", func() error {
			got, err := r.Archived(ctx, ref)
			plain, perr := r.Archived(ctx, git)
			return want(errors.Join(err, perr), got == plain, "Archived が、git リポジトリのものと違う")
		}},
		{"MainHead", func() error {
			got, err := r.MainHead(ctx, ref)
			plain, perr := r.MainHead(ctx, git)
			return want(errors.Join(err, perr), got != "" && got == plain, "MainHead が、git リポジトリの main と違う："+got)
		}},
		{"Prepared", func() error {
			got, err := r.Prepared(ctx, ref)
			return want(err, !got, "用意の前に、Prepared が true")
		}},
		{"SetupRepository", func() error {
			if err := r.SetupRepository(ctx, ref, e.Files, "用意\n\nLogiforge-Init: 1"); err != nil {
				return err
			}
			got, err := r.Prepared(ctx, ref)
			if err := want(err, got, "用意のあと、Prepared が false"); err != nil {
				return err
			}
			// 2 回目は、ディレクトリがもうあるので ErrMainExists。
			err = r.SetupRepository(ctx, ref, e.Files, "用意\n\nLogiforge-Init: 1")
			return want(nil, errors.Is(err, ports.ErrMainExists), "2 回目の SetupRepository が ErrMainExists でない")
		}},
	}
	check(t, reflect.TypeFor[pullreq.Repos](), steps)
	for _, s := range steps {
		if err := s.run(); err != nil {
			t.Fatalf("%s（%s）：%v", s.name, ref, err)
		}
	}
}

// check は、型 it のすべてのメソッドを、steps で呼んでいるかを確かめる。
func check(t *testing.T, it reflect.Type, steps []step) {
	t.Helper()
	var names []string
	for _, s := range steps {
		names = append(names, s.name)
	}
	for i := range it.NumMethod() {
		if m := it.Method(i).Name; !slices.Contains(names, m) {
			t.Fatalf("%s の %s を、apitest で呼んでいない（足す）", it, m)
		}
	}
}
