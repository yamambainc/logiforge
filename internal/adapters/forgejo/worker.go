package forgejo

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/yamambainc/logiforge/internal/ports"
)

// WorkerSource は、worker が読む repository の一覧と git の URL を、読むだけのユーザーのトークンで求める。
type WorkerSource struct {
	c *client
	// Login と Token は、worker の読むだけのユーザー（read:repository）。
	Login, Token string
}

// NewWorkerSource は、worker のための WorkerSource を作る。cfg の書き込み主体のトークンは使わない。
func NewWorkerSource(cfg Config, login, token string) *WorkerSource {
	cfg.WriterToken = ""
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

// MergedByWriter は、sha が、書き込み主体が merge した pull request の merge のコミットかを返す（design/backend/broken.md の「反映かの判定」）。
// GET /repos/{owner}/{repo}/commits/{sha}/pull は、そのコミットを merge した pull request を返し、なければ 404 を返す。
func (s *WorkerSource) MergedByWriter(ctx context.Context, gitForgeRef, sha string) (bool, error) {
	if s.c.cfg.Writer == "" {
		return false, errors.New("forgejo: 書き込み主体のログイン名（LOGIFORGE_WRITER）がない")
	}
	rp, err := repoPath(gitForgeRef)
	if err != nil {
		return false, err
	}
	var p struct {
		Merged         bool   `json:"merged"`
		MergeCommitSHA string `json:"merge_commit_sha"`
		MergedBy       *user  `json:"merged_by"`
	}
	if err := s.c.do(ctx, s.Token, http.MethodGet, rp+"/commits/"+url.PathEscape(sha)+"/pull", nil, &p); err != nil {
		if statusOf(err) == http.StatusNotFound {
			return false, nil
		}
		return false, err
	}
	return p.Merged && p.MergeCommitSHA == sha && p.MergedBy != nil && strings.EqualFold(p.MergedBy.Login, s.c.cfg.Writer), nil
}
