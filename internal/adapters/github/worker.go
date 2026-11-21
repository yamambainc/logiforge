package github

import (
	"context"
	"errors"

	"github.com/yamambainc/logiforge/internal/ports"
)

// WorkerSource は、worker が読む repository の一覧と git の URL と資格情報である（design/adapters/git.md）。
// 一覧は設定から引き、GitHub に問い合わせない。
//
// 読むトークンは、Read に絞った installation token である。tokens が nil なら、トークンなしで読む（public のリポジトリだけ）。
// そのときの REST の上限は、1 時間に 60 回である（design/adapters/github.md）。
type WorkerSource struct {
	cfg Config
	c   *client
	app TokenSource
}

// TokenSource は、gitForgeRef を読む、Read に絞った installation token を返すものである。
// バックエンドでは App が作る。worker では、バックエンドが作って設定の DB に置いたものを読む
// （design/adapters/github-app.md の「worker のトークン」）。
type TokenSource interface {
	Token(ctx context.Context, gitForgeRef string) (string, error)
}

// NewWorkerSource は、WorkerSource を作る。tokens は nil でもよい（public のリポジトリだけを読むとき）。
func NewWorkerSource(cfg Config, tokens TokenSource) (*WorkerSource, error) {
	if len(cfg.Repositories) == 0 {
		return nil, errors.New("github: repository が 1 つも登録されていない")
	}
	if err := cfg.check(); err != nil {
		return nil, err
	}
	return &WorkerSource{cfg: cfg, c: newClient(cfg.APIURL, cfg.HTTP), app: tokens}, nil
}

// ReadToken は、gitForgeRef を読むトークンを返す。App がなければ空（トークンなしで読む）。
func (s *WorkerSource) ReadToken(ctx context.Context, gitForgeRef string) (string, error) {
	if s.app == nil {
		return "", nil
	}
	return s.app.Token(ctx, gitForgeRef)
}

// ListRepositories は、設定に登録した repository を返す。
func (s *WorkerSource) ListRepositories(context.Context) ([]ports.Repository, error) {
	return sortedRepos(s.cfg.Repositories), nil
}

// GitForgeRef は、名前から gitforge_ref を、設定から引く。
func (s *WorkerSource) GitForgeRef(_ context.Context, name string) (string, error) {
	return lookup(s.cfg.Repositories, name)
}

// GitURL は、git で読む URL を返す。資格情報は、URL に入れず、GitEnv の credential helper で渡す。
func (s *WorkerSource) GitURL(gitForgeRef string) string {
	return s.cfg.webURL() + "/" + gitRef(gitForgeRef) + ".git"
}

// GitEnv は、gitForgeRef を git で読むときに、資格情報を渡す環境変数を返す。git の操作のたびに呼ぶ。
// installation token は切れるので、そのときに有効なものを渡す。トークンは、コマンドの引数や設定ファイルに書かず、
// credential helper が環境変数から読む（design/adapters/git.md の「資格情報」）。helper は、GitHub の URL にだけ使う。
// App がなければ、何も渡さない（public のリポジトリを、認証なしで読む）。
func (s *WorkerSource) GitEnv(ctx context.Context, gitForgeRef string) ([]string, error) {
	tok, err := s.ReadToken(ctx, gitForgeRef)
	if err != nil || tok == "" {
		return nil, err
	}
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential." + s.cfg.webURL() + ".helper",
		`GIT_CONFIG_VALUE_0=!f() { if [ "$1" = get ]; then echo username=x-access-token; echo password="$LOGIFORGE_GIT_PASSWORD"; fi; }; f`,
		"LOGIFORGE_GIT_PASSWORD=" + tok,
	}, nil
}
