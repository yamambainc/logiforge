package github

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"
	"github.com/yamambainc/logiforge/internal/ports"
)

// WorkerSource は、worker が読む repository の一覧と git の URL と資格情報である（design/adapters/git.md）。
// 一覧は設定から引き、GitHub に問い合わせない。
//
// Token は、読むだけのトークン（fine-grained personal access token の、Contents、Issues、Pull requests の Read）である。
// 空なら、トークンなしで読む（public のリポジトリだけ）。そのときの REST の上限は、1 時間に 60 回である（design/adapters/github.md）。
type WorkerSource struct {
	cfg   Config
	c     *client
	Token string
}

// NewWorkerSource は、worker のための WorkerSource を作る。token は空でもよい（public のリポジトリだけを読むとき）。
func NewWorkerSource(cfg Config, token string) (*WorkerSource, error) {
	if len(cfg.Repositories) == 0 {
		return nil, errors.New("github: repository が 1 つも登録されていない")
	}
	if err := cfg.check(); err != nil {
		return nil, err
	}
	return &WorkerSource{cfg: cfg, c: newClient(cfg.APIURL, cfg.HTTP), Token: token}, nil
}

// Context は、ctx に worker のトークンを入れる。GitForge のアダプタ（pullreq.Adapter）を、worker のトークンで呼ぶときに使う。
func (s *WorkerSource) Context(ctx context.Context) context.Context {
	return ports.WithToken(ctx, s.Token)
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

// GitEnv は、git に資格情報を渡す環境変数を返す。トークンは、コマンドの引数や設定ファイルに書かず、
// credential helper が環境変数から読む（design/adapters/git.md の「資格情報」）。helper は、GitHub の URL にだけ使う。
// トークンがなければ、何も渡さない（public のリポジトリを、認証なしで読む）。
func (s *WorkerSource) GitEnv() []string {
	if s.Token == "" {
		return nil
	}
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential." + s.cfg.webURL() + ".helper",
		`GIT_CONFIG_VALUE_0=!f() { if [ "$1" = get ]; then echo username=x-access-token; echo password="$LOGIFORGE_GIT_PASSWORD"; fi; }; f`,
		"LOGIFORGE_GIT_PASSWORD=" + s.Token,
	}
}

// RepoID は、記録の署名の場所に使う、repository の node_id を返す。
func (s *WorkerSource) RepoID(ctx context.Context, gitForgeRef string) (string, error) {
	return api{&forge{cfg: s.cfg, c: s.c}}.RepoID(s.Context(ctx), gitForgeRef)
}

// CommentsSince は、repository のコメント（issue と pull request）のうち、since のあとに作られたか変わったものを返す
// （記録の控え。design/backend/verify.md）。
func (s *WorkerSource) CommentsSince(ctx context.Context, gitForgeRef string, since time.Time) ([]pullreq.Comment, error) {
	rp, err := repoPath(gitForgeRef)
	if err != nil {
		return nil, err
	}
	path := rp + "/issues/comments?sort=updated&direction=asc"
	if !since.IsZero() {
		path += "&since=" + url.QueryEscape(since.UTC().Format(time.RFC3339))
	}
	cs, err := list[commentJSON](ctx, s.c, s.Token, path)
	if err != nil {
		if statusOf(err) == http.StatusNotFound {
			return nil, ports.ErrNotFound
		}
		return nil, err
	}
	out := make([]pullreq.Comment, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.comment())
	}
	return out, nil
}
