package forgejo

import (
	"context"
	"net/url"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/adapters/pullreq"

	"github.com/yamambainc/logiforge/internal/ports"
)

// WorkerSource は、worker が読む repository の一覧と git の URL を、読むだけのユーザーのトークンで求める。
type WorkerSource struct {
	c *client
	// Login と Token は、worker の読むだけのユーザー（read:repository、read:organization、read:issue）。
	Login, Token string
}

// NewWorkerSource は、worker のための WorkerSource を作る。cfg の書き込み主体のトークンは使わない。
func NewWorkerSource(cfg Config, login, token string) *WorkerSource {
	return &WorkerSource{c: &client{cfg: cfg}, Login: login, Token: token}
}

// ListRepositories は、organization の repository を列挙する。
func (s *WorkerSource) ListRepositories(ctx context.Context) ([]ports.Repository, error) {
	return repos{s.c}.list(ctx, s.Token)
}

// GitForgeRef は、名前から gitforge_ref（<organization>/<名前>）を求める。Forgejo に問い合わせない。
func (s *WorkerSource) GitForgeRef(_ context.Context, name string) (string, error) {
	return s.c.cfg.Org + "/" + name, nil
}

// GitURL は、git で読む URL を返す。資格情報は、URL に入れず、GitEnv の credential helper で渡す。
func (s *WorkerSource) GitURL(gitForgeRef string) string {
	return strings.TrimRight(s.c.cfg.URL, "/") + "/" + gitForgeRef + ".git"
}

// GitEnv は、git に資格情報を渡す環境変数を返す。トークンは、コマンドの引数や設定ファイルに書かず、
// credential helper が環境変数から読む（design/adapters/git.md の「資格情報」）。
func (s *WorkerSource) GitEnv() []string {
	return []string{
		"GIT_CONFIG_COUNT=1",
		// helper は、Forgejo の URL にだけ使う。ほかの URL に、トークンを渡さないため。
		"GIT_CONFIG_KEY_0=credential." + strings.TrimRight(s.c.cfg.URL, "/") + ".helper",
		`GIT_CONFIG_VALUE_0=!f() { if [ "$1" = get ]; then echo username="$LOGIFORGE_GIT_USERNAME"; echo password="$LOGIFORGE_GIT_PASSWORD"; fi; }; f`,
		"LOGIFORGE_GIT_USERNAME=" + s.Login,
		"LOGIFORGE_GIT_PASSWORD=" + s.Token,
	}
}

// RepoID は、記録の署名の場所に使う、repository の id を返す。
func (s *WorkerSource) RepoID(ctx context.Context, gitForgeRef string) (string, error) {
	return api{s.c}.RepoID(ports.WithToken(ctx, s.Token), gitForgeRef)
}

// CommentsSince は、repository のコメントのうち、since のあとに作られたか変わったものを返す（記録の控え。design/backend/verify.md）。
func (s *WorkerSource) CommentsSince(ctx context.Context, gitForgeRef string, since time.Time) ([]pullreq.Comment, error) {
	rp, err := repoPath(gitForgeRef)
	if err != nil {
		return nil, err
	}
	path := rp + "/issues/comments"
	if !since.IsZero() {
		path += "?since=" + url.QueryEscape(since.UTC().Format(time.RFC3339))
	}
	cs, err := list[commentJSON](ctx, s.c, s.Token, path)
	if err != nil {
		return nil, err
	}
	out := make([]pullreq.Comment, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.comment())
	}
	return out, nil
}

// Comments は、pull request か issue のコメントを返す（反映かの判定で、今の GitForge の記録を読む）。
func (s *WorkerSource) Comments(ctx context.Context, gitForgeRef string, number int) ([]pullreq.Comment, error) {
	return api{s.c}.ListComments(ports.WithToken(ctx, s.Token), gitForgeRef, number)
}
