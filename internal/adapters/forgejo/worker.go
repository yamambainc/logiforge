package forgejo

import (
	"context"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ports"
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

// GitURL は、git で読む URL を返す。資格情報は、URL に入れず、GitEnv の credential helper で渡す。
func (s *WorkerSource) GitURL(gitForgeRef string) string {
	return strings.TrimRight(s.c.cfg.URL, "/") + "/" + gitForgeRef + ".git"
}

// GitEnv は、git に資格情報を渡す環境変数を返す。トークンは、コマンドの引数や設定ファイルに書かず、
// credential helper が環境変数から読む（design/adapters/git.md の「資格情報」）。
func (s *WorkerSource) GitEnv() []string {
	return []string{
		"GIT_CONFIG_COUNT=1",
		"GIT_CONFIG_KEY_0=credential.helper",
		`GIT_CONFIG_VALUE_0=!f() { if [ "$1" = get ]; then echo username="$LOGIFORGE_GIT_USERNAME"; echo password="$LOGIFORGE_GIT_PASSWORD"; fi; }; f`,
		"LOGIFORGE_GIT_USERNAME=" + s.Login,
		"LOGIFORGE_GIT_PASSWORD=" + s.Token,
	}
}
