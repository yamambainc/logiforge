package github

// 単体のテストに使う、GitHub の REST の偽物。アダプタが呼ぶものだけを、GitHub の応答の形で返す。
// 一覧は 2 件ずつのページにし、Link の next を辿ることも確かめる。

import (
	"cmp"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yamambainc/logiforge/internal/ports"
)

const (
	fakeOwner = "acme"
	fakeRepo  = "acme/study"
	// fakeAgent は、テストで GitForge を呼ぶ主体のトークンと、そのログイン名である。
	fakeAgent      = "tok-agent"
	fakeAgentLogin = "agent"
	pageSize       = 2
)

type fakeComment struct {
	ID     int64
	Issue  int
	Body   string
	Author string
}

type fakePull struct {
	Number    int
	Title     string
	Body      string
	Head      string
	HeadSHA   string
	State     string
	Merged    bool
	MergeSHA  string
	Author    string
	pending   int // merge できるかを確かめ終わるまでの、GET の回数
	conflict  bool
	createdAt time.Time
	// mergedBy は、merge した主体。
	mergedBy string
	// base は、向き先。空なら main。
	base      string
	labels    []string
	updatedAt time.Time
}

type fakeReview struct {
	ID       int64
	State    string
	Body     string
	Author   string
	CommitID string
	At       time.Time
}

type fakeEvent struct {
	Event, Label, Actor string
	At                  time.Time
}

type fakeIssue struct {
	Number   int
	Title    string
	Body     string
	State    string
	Labels   []string
	Author   string
	closedAt *time.Time
}

type fakeGitHub struct {
	t   *testing.T
	srv *httptest.Server
	// private なら、トークンなしでは読めない。
	private bool
	// requireApproval なら、main を承認を必須にする設定（branch protection）で守る。GitHub と同じく、そのときだけ reviewDecision を返し、
	// 承認が足りない merge を 405 で断る。なければ reviewDecision は null で、承認がなくても merge できる。
	requireApproval bool
	// tree は、Git Data API で作りかけのツリー（ツリーの SHA → パス → 中身）。gitCommits は、コミットの SHA → ツリーの SHA とメッセージ。
	trees      map[string]map[string]string
	gitCommits map[string][2]string

	mu      sync.Mutex
	next    int // pull request と issue の番号（共通）
	pulls   map[int]*fakePull
	issues  map[int]*fakeIssue
	reviews map[int][]*fakeReview
	labels  map[string]bool
	events  map[int][]fakeEvent
	// writers は、リポジトリに書く権限を持つログイン名（review を数えるか）。
	writers  map[string]bool
	comments []*fakeComment
	files    map[string]string
	commits  int
	// users は、トークンから、ログイン名と、リポジトリの権限を引く。
	users map[string]fakeUser
	// permCalls は、リポジトリの権限を問い合わせた回数（キャッシュを確かめる）。
	permCalls int
	// readable は、トークンごとに読める repository。
	readable map[string]bool
	// userRepoCalls は、GET /user/repos の回数。hideFromUserRepos なら、読めても一覧に出さない（関わっていない public のリポジトリ）。
	userRepoCalls     int
	hideFromUserRepos bool
	extraUserRepos    []string
	served            int
	repoGets          int
	// limited は、次の要求を、制限の応答にする回数。
	limited int
	// failPut は、contents の PUT を失敗させる回数（その前の PUT は通す）：after 回通したあとに 1 回失敗させる。
	failPutAfter int
	// branchFiles は、main でないブランチに置いたファイル（ブランチ → パス → 中身）。
	branchFiles map[string]map[string]string
	// branchHead は、ブランチの先頭の SHA。branchSeq で作る。
	branchHead map[string]string
	branchSeq  int
	// mergeFails は、merge を失敗させる回数。
	mergeFails int
	// ruleBlocked なら、merge をルールセットで断る（405 の Repository rule violations）。
	ruleBlocked bool
	// ownTokens は、Logiforge の OAuth App のトークン。
	ownTokens map[string]bool
	messages  []string
	// appKey は、読むだけの GitHub App の公開鍵（App の JWT を確かめる）。appMinted は、作った installation token の数、
	// appTTL は、その有効な時間（0 なら 1 時間）。appRequests は、installation token を作る要求の中身。
	appKey      *rsa.PublicKey
	appMinted   int
	appTTL      time.Duration
	appRequests []map[string]any
}

// appJWT は、Authorization の App の JWT を、appKey で確かめる（署名、iss、期限）。
func (f *fakeGitHub) appJWT(r *http.Request) bool {
	parts := strings.Split(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), ".")
	if f.appKey == nil || len(parts) != 3 {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if rsa.VerifyPKCS1v15(f.appKey, crypto.SHA256, sum[:], sig) != nil {
		return false
	}
	b, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c struct {
		Iss      string `json:"iss"`
		Iat, Exp int64
	}
	_ = json.Unmarshal(b, &c)
	now := time.Now().Unix()
	return c.Iss == "42" && c.Iat <= now && now < c.Exp && c.Exp-c.Iat <= 600
}

type fakeUser struct {
	login string
	// push は、App が install されていて、push の権限がある GitHub リポジトリ（owner/name）。read は、読めるだけのもの。
	push, read []string
	// noMerge なら、merge を 403 で断る（Write の権限がない）。
	noMerge bool
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{t: t, next: 1, trees: map[string]map[string]string{}, gitCommits: map[string][2]string{}, pulls: map[int]*fakePull{}, issues: map[int]*fakeIssue{}, files: map[string]string{},
		reviews: map[int][]*fakeReview{}, labels: map[string]bool{}, events: map[int][]fakeEvent{}, writers: map[string]bool{"alice": true, "bob": true, fakeAgentLogin: true},
		users: map[string]fakeUser{}, readable: map[string]bool{}, failPutAfter: -1, ownTokens: map[string]bool{}, branchFiles: map[string]map[string]string{}, branchHead: map[string]string{}}
	f.users[fakeAgent] = fakeUser{login: fakeAgentLogin}
	f.readable[fakeAgent] = true
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

// mainSHA は、main の n 番目のコミットの SHA である。
func mainSHA(n int) string { return fmt.Sprintf("c%039d", n) }

func (f *fakeGitHub) config() Config {
	return Config{APIURL: f.srv.URL, WebURL: "https://github.example",
		Repositories: []ports.Repository{{Name: "study", GitForgeRef: fakeRepo}}, MergeWait: 5 * time.Second}
}

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

func fail(w http.ResponseWriter, status int, msg string) {
	write(w, status, map[string]string{"message": msg})
}

// page は、一覧を 2 件ずつに分け、Link の next を付ける。
func page[T any](w http.ResponseWriter, r *http.Request, all []T) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if p == 0 {
		p = 1
	}
	from, to := (p-1)*pageSize, p*pageSize
	if from > len(all) {
		from = len(all)
	}
	if to > len(all) {
		to = len(all)
	}
	if to < len(all) {
		q := r.URL.Query()
		q.Set("page", strconv.Itoa(p+1))
		w.Header().Set("Link", fmt.Sprintf(`<http://%s%s?%s>; rel="next"`, r.Host, r.URL.Path, q.Encode()))
	}
	out := all[from:to]
	if out == nil {
		out = []T{}
	}
	write(w, http.StatusOK, out)
}

func (f *fakeGitHub) pullJSON(p *fakePull) map[string]any {
	m := map[string]any{"number": p.Number, "title": p.Title, "body": p.Body, "state": p.State, "user": map[string]string{"login": p.Author},
		"head": map[string]string{"ref": p.Head, "sha": p.HeadSHA}, "base": map[string]string{"ref": cmp.Or(p.base, "main")}, "created_at": p.createdAt,
		"merged": p.Merged, "merge_commit_sha": p.MergeSHA, "merged_at": nil, "labels": labelObjs(p.labels), "updated_at": p.updatedAt}
	if p.Merged {
		m["merged_at"] = "2026-11-17T00:00:00Z"
		m["merged_by"] = map[string]string{"login": p.mergedBy}
	}
	switch {
	case p.Merged:
		m["mergeable"] = nil
	case p.pending > 0:
		m["mergeable"] = nil
		p.pending--
	default:
		m["mergeable"] = !p.conflict
	}
	return m
}

func labelObjs(ls []string) []map[string]string {
	out := []map[string]string{}
	for _, l := range ls {
		out = append(out, map[string]string{"name": l})
	}
	return out
}

// labelsOf は、番号の pull request か issue のラベルを返す。
func (f *fakeGitHub) labelsOf(n int) *[]string {
	if x, ok := f.pulls[n]; ok {
		return &x.labels
	}
	return &f.issues[n].Labels
}

var (
	reReviews   = regexp.MustCompile(`^/repos/acme/study/pulls/(\d+)/reviews$`)
	reIssLabels = regexp.MustCompile(`^/repos/acme/study/issues/(\d+)/labels$`)
	reIssLabel  = regexp.MustCompile(`^/repos/acme/study/issues/(\d+)/labels/(.+)$`)
	reEvents    = regexp.MustCompile(`^/repos/acme/study/issues/(\d+)/events$`)
	reLabel     = regexp.MustCompile(`^/repos/acme/study/labels/(.+)$`)
	rePull      = regexp.MustCompile(`^/repos/acme/study/pulls/(\d+)$`)
	reMerge     = regexp.MustCompile(`^/repos/acme/study/pulls/(\d+)/merge$`)
	reIssue     = regexp.MustCompile(`^/repos/acme/study/issues/(\d+)$`)
	reComments  = regexp.MustCompile(`^/repos/acme/study/issues/(\d+)/comments$`)
	reContents  = regexp.MustCompile(`^/repos/acme/study/contents/(.+)$`)
)

func num(re *regexp.Regexp, p string) int {
	n, _ := strconv.Atoi(re.FindStringSubmatch(p)[1])
	return n
}

// requests は、受けた HTTP の要求の数である（呼び出しの回数のテスト）。
func (f *fakeGitHub) requests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.served
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.served++
	if f.limited > 0 {
		f.limited--
		w.Header().Set("Retry-After", "1")
		fail(w, http.StatusForbidden, "You have exceeded a secondary rate limit")
		return
	}
	p := r.URL.Path
	var in map[string]any
	_ = json.NewDecoder(r.Body).Decode(&in)
	str := func(k string) string { s, _ := in[k].(string); return s }

	if p == "/applications/cid/token" && r.Method == http.MethodPost {
		if u, pw, ok := r.BasicAuth(); !ok || u != "cid" || pw != "csecret" {
			fail(w, http.StatusUnauthorized, "bad client")
			return
		}
		if !f.ownTokens[str("access_token")] {
			fail(w, http.StatusNotFound, "Not Found")
			return
		}
		write(w, http.StatusOK, map[string]any{"token": str("access_token")})
		return
	}
	// 読むだけの GitHub App（App の JWT で呼ぶもの）
	switch {
	case p == "/repos/acme/study/installation" && r.Method == http.MethodGet:
		if !f.appJWT(r) {
			fail(w, http.StatusUnauthorized, "bad jwt")
			return
		}
		write(w, http.StatusOK, map[string]any{"id": 7})
		return
	case p == "/app/installations/7/access_tokens" && r.Method == http.MethodPost:
		if !f.appJWT(r) {
			fail(w, http.StatusUnauthorized, "bad jwt")
			return
		}
		f.appRequests = append(f.appRequests, in)
		f.appMinted++
		tok := fmt.Sprintf("ghs_%d", f.appMinted)
		f.readable[tok] = true
		ttl := f.appTTL
		if ttl == 0 {
			ttl = time.Hour
		}
		write(w, http.StatusCreated, map[string]any{"token": tok, "expires_at": time.Now().Add(ttl).UTC().Format(time.RFC3339)})
		return
	}
	// 主体のトークンで呼ぶもの
	tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	switch {
	case p == "/user":
		u, ok := f.users[tok]
		if !ok {
			fail(w, http.StatusUnauthorized, "Bad credentials")
			return
		}
		write(w, http.StatusOK, map[string]string{"login": u.login})
		return
	case p == "/user/repos":
		f.userRepoCalls++
		var rs []map[string]any
		if f.readable[tok] && !f.hideFromUserRepos {
			rs = append(rs, map[string]any{"full_name": "acme/study"})
			for _, n := range f.extraUserRepos {
				rs = append(rs, map[string]any{"full_name": n})
			}
		}
		write(w, http.StatusOK, rs)
		return
	case p == "/user/installations":
		if _, ok := f.users[tok]; !ok {
			fail(w, http.StatusUnauthorized, "Bad credentials")
			return
		}
		write(w, http.StatusOK, map[string]any{"total_count": 1, "installations": []map[string]any{{"id": 7}}})
		return
	case p == "/user/installations/7/repositories":
		u := f.users[tok]
		var rs []map[string]any
		for _, name := range u.push {
			rs = append(rs, map[string]any{"full_name": name, "permissions": map[string]bool{"push": true, "pull": true}})
		}
		for _, name := range u.read {
			rs = append(rs, map[string]any{"full_name": name, "permissions": map[string]bool{"push": false, "pull": true}})
		}
		f.permCalls++
		write(w, http.StatusOK, map[string]any{"total_count": len(rs), "repositories": rs})
		return
	}
	// repository は、読める主体のトークンか、public ならトークンなしで読める。書くのは、読める主体だけ。
	switch {
	case f.readable[tok]:
	case tok == "" && !f.private && r.Method == http.MethodGet:
	case tok == "":
		fail(w, http.StatusUnauthorized, "Requires authentication")
		return
	default:
		fail(w, http.StatusNotFound, "Not Found")
		return
	}
	login := f.users[tok].login
	switch {
	case p == "/repos/acme/study" && r.Method == http.MethodGet:
		f.repoGets++
		write(w, http.StatusOK, map[string]string{"node_id": "R_x"})
	case p == "/repos/acme/study/git/ref/heads/main":
		if f.commits == 0 {
			fail(w, http.StatusConflict, "Git Repository is empty.")
			return
		}
		write(w, http.StatusOK, map[string]any{"object": map[string]string{"sha": mainSHA(f.commits)}})
	case p == "/repos/acme/study/commits/main" && r.Method == http.MethodGet:
		parents := []map[string]string{}
		if f.commits > 1 {
			parents = append(parents, map[string]string{"sha": mainSHA(f.commits - 1)})
		}
		write(w, http.StatusOK, map[string]any{"sha": mainSHA(f.commits), "parents": parents, "commit": map[string]string{"message": f.messages[len(f.messages)-1]}})
	case strings.HasPrefix(p, "/repos/acme/study/git/commits/") && r.Method == http.MethodGet:
		write(w, http.StatusOK, map[string]any{"tree": map[string]string{"sha": "t-" + strings.TrimPrefix(p, "/repos/acme/study/git/commits/")}})
	case p == "/repos/acme/study/git/trees" && r.Method == http.MethodPost:
		files := map[string]string{}
		for _, e := range in["tree"].([]any) {
			m := e.(map[string]any)
			files[m["path"].(string)] = m["content"].(string)
		}
		sha := fmt.Sprintf("t%039d", len(f.trees)+1)
		f.trees[sha] = files
		write(w, http.StatusCreated, map[string]string{"sha": sha})
	case p == "/repos/acme/study/git/commits" && r.Method == http.MethodPost:
		parents := in["parents"].([]any)
		if len(parents) != 1 || parents[0] != mainSHA(f.commits) {
			fail(w, http.StatusUnprocessableEntity, "親が main の先頭でない")
			return
		}
		sha := fmt.Sprintf("g%039d", len(f.gitCommits)+1)
		f.gitCommits[sha] = [2]string{str("tree"), str("message")}
		write(w, http.StatusCreated, map[string]string{"sha": sha})
	case p == "/repos/acme/study/git/refs/heads/main" && r.Method == http.MethodPatch:
		c, ok := f.gitCommits[str("sha")]
		if !ok {
			fail(w, http.StatusUnprocessableEntity, "Object does not exist")
			return
		}
		if f.failPutAfter == 0 {
			f.failPutAfter = -1
			fail(w, http.StatusBadGateway, "失敗させる")
			return
		}
		for k, v := range f.trees[c[0]] {
			f.files[k] = v
		}
		f.messages = append(f.messages, c[1])
		f.commits++
		write(w, http.StatusOK, map[string]any{"object": map[string]string{"sha": mainSHA(f.commits)}})
	case p == "/repos/acme/study/git/refs" && r.Method == http.MethodPost:
		b := strings.TrimPrefix(str("ref"), "refs/heads/")
		if _, ok := f.branchFiles[b]; ok {
			fail(w, http.StatusUnprocessableEntity, "Reference already exists")
			return
		}
		f.branchFiles[b] = map[string]string{}
		f.bump(b)
		write(w, http.StatusCreated, map[string]string{"ref": str("ref")})
	case strings.HasPrefix(p, "/repos/acme/study/git/refs/heads/") && r.Method == http.MethodPatch:
		b := strings.TrimPrefix(p, "/repos/acme/study/git/refs/heads/")
		f.branchFiles[b] = map[string]string{}
		f.bump(b)
		write(w, http.StatusOK, map[string]string{"ref": "refs/heads/" + b})
	case strings.HasPrefix(p, "/repos/acme/study/compare/main...") && r.Method == http.MethodGet:
		b := f.branchOf(strings.TrimPrefix(p, "/repos/acme/study/compare/main..."))
		if b == "" {
			fail(w, http.StatusNotFound, "Not Found")
			return
		}
		var files []map[string]string
		for k := range f.branchFiles[b] {
			files = append(files, map[string]string{"filename": k, "status": "added"})
		}
		write(w, http.StatusOK, map[string]any{"files": files})
	case strings.HasPrefix(p, "/repos/acme/study/git/refs/heads/") && r.Method == http.MethodDelete:
		delete(f.branchFiles, strings.TrimPrefix(p, "/repos/acme/study/git/refs/heads/"))
		w.WriteHeader(http.StatusNoContent)
	case reContents.MatchString(p) && r.Method == http.MethodGet && r.URL.Query().Get("ref") != "main":
		path := reContents.FindStringSubmatch(p)[1]
		ref := r.URL.Query().Get("ref")
		if b := f.branchOf(ref); b != "" {
			ref = b
		}
		if c, ok := f.branchFiles[ref][path]; ok {
			write(w, http.StatusOK, map[string]string{"type": "file", "content": base64.StdEncoding.EncodeToString([]byte(c)), "encoding": "base64"})
			return
		}
		if c, ok := f.files[path]; ok {
			write(w, http.StatusOK, map[string]string{"type": "file", "content": base64.StdEncoding.EncodeToString([]byte(c)), "encoding": "base64"})
			return
		}
		fail(w, http.StatusNotFound, "Not Found")
	case reContents.MatchString(p) && r.Method == http.MethodGet:
		path := reContents.FindStringSubmatch(p)[1]
		if c, ok := f.files[path]; ok {
			write(w, http.StatusOK, map[string]string{"type": "file", "content": base64.StdEncoding.EncodeToString([]byte(c)), "encoding": "base64"})
			return
		}
		seen := map[string]bool{}
		var entries []map[string]string
		for k := range f.files {
			if rest, ok := strings.CutPrefix(k, path+"/"); ok {
				name, _, _ := strings.Cut(rest, "/")
				if !seen[name] {
					seen[name] = true
					entries = append(entries, map[string]string{"name": name, "type": "dir"})
				}
			}
		}
		if entries == nil {
			fail(w, http.StatusNotFound, "Not Found")
			return
		}
		write(w, http.StatusOK, entries)
	case reContents.MatchString(p) && r.Method == http.MethodPut:
		if f.failPutAfter == 0 {
			f.failPutAfter = -1
			fail(w, http.StatusBadGateway, "失敗させる")
			return
		}
		f.failPutAfter--
		path := reContents.FindStringSubmatch(p)[1]
		b, _ := base64.StdEncoding.DecodeString(str("content"))
		if br := str("branch"); br != "main" {
			bf, ok := f.branchFiles[br]
			if !ok {
				fail(w, http.StatusNotFound, "Branch not found")
				return
			}
			bf[path] = string(b)
			write(w, http.StatusCreated, map[string]any{"commit": map[string]string{"sha": f.bump(br)}})
			return
		}
		f.files[path] = string(b)
		f.messages = append(f.messages, str("message"))
		f.commits++
		write(w, http.StatusCreated, map[string]any{"commit": map[string]string{"sha": mainSHA(f.commits)}})
	case p == "/graphql" && r.Method == http.MethodPost:
		// 1 件の pull request の review を、書いた人が push できるかと一緒に返す（api.go の reviewsQuery）。
		vars, _ := in["variables"].(map[string]any)
		// GitHub と同じく、ない repository は errors で返す（ディレクトリの参照の :パス を付けたまま渡すと、ここに当たる）。
		if owner, name := vars["owner"], vars["name"]; owner != "acme" || name != "study" {
			write(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": nil},
				"errors": []map[string]any{{"type": "NOT_FOUND", "message": fmt.Sprintf("Could not resolve to a Repository with the name '%v/%v'.", owner, name)}}})
			return
		}
		// 承認の状態（api.go の ReviewDecisions）：pull request の別名（p<番号>）ごとに reviewDecision を返す。
		if q, _ := in["query"].(string); strings.Contains(q, "reviewDecision") {
			repo := map[string]any{}
			for _, m := range regexp.MustCompile(`p(\d+): pullRequest`).FindAllStringSubmatch(q, -1) {
				n, _ := strconv.Atoi(m[1])
				var d any
				if f.requireApproval {
					d = f.reviewDecision(n)
				}
				repo["p"+m[1]] = map[string]any{"reviewDecision": d}
			}
			write(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": repo}})
			return
		}
		n := int(vars["number"].(float64))
		var nodes []map[string]any
		for _, rv := range f.reviews[n] {
			nodes = append(nodes, map[string]any{"databaseId": rv.ID, "state": rv.State, "body": rv.Body, "submittedAt": rv.At,
				"authorCanPushToRepository": f.writers[rv.Author], "author": map[string]string{"login": rv.Author}, "commit": map[string]string{"oid": rv.CommitID}})
		}
		write(w, http.StatusOK, map[string]any{"data": map[string]any{"repository": map[string]any{"pullRequest": map[string]any{
			"reviews": map[string]any{"pageInfo": map[string]any{"hasNextPage": false, "endCursor": ""}, "nodes": nodes}}}}})
	case reReviews.MatchString(p) && r.Method == http.MethodGet:
		var out []map[string]any
		for _, rv := range f.reviews[num(reReviews, p)] {
			out = append(out, map[string]any{"id": rv.ID, "state": rv.State, "body": rv.Body, "user": map[string]string{"login": rv.Author}, "commit_id": rv.CommitID, "submitted_at": rv.At})
		}
		page(w, r, out)
	case reReviews.MatchString(p) && r.Method == http.MethodPost:
		n := num(reReviews, p)
		state := map[string]string{"APPROVE": "APPROVED", "REQUEST_CHANGES": "CHANGES_REQUESTED", "COMMENT": "COMMENTED"}[str("event")]
		if state == "APPROVED" && f.pulls[n].Author == login {
			fail(w, http.StatusUnprocessableEntity, "Can not approve your own pull request")
			return
		}
		rv := &fakeReview{ID: int64(len(f.reviews[n]) + 1), State: state, Body: str("body"), Author: login, CommitID: str("commit_id"), At: time.Now()}
		f.reviews[n] = append(f.reviews[n], rv)
		f.pulls[n].updatedAt = time.Now()
		write(w, http.StatusOK, map[string]any{"id": rv.ID, "state": rv.State, "body": rv.Body, "user": map[string]string{"login": login}, "commit_id": rv.CommitID, "submitted_at": rv.At})
	case reLabel.MatchString(p) && r.Method == http.MethodGet:
		if !f.labels[reLabel.FindStringSubmatch(p)[1]] {
			fail(w, http.StatusNotFound, "Not Found")
			return
		}
		write(w, http.StatusOK, map[string]string{"name": reLabel.FindStringSubmatch(p)[1]})
	case p == "/repos/acme/study/labels" && r.Method == http.MethodPost:
		f.labels[str("name")] = true
		write(w, http.StatusCreated, map[string]string{"name": str("name")})
	case reIssLabels.MatchString(p) && r.Method == http.MethodPost:
		n := num(reIssLabels, p)
		ls := f.labelsOf(n)
		for _, l := range in["labels"].([]any) {
			name := l.(string)
			f.labels[name] = true
			if !slices.Contains(*ls, name) {
				*ls = append(*ls, name)
			}
			f.events[n] = append(f.events[n], fakeEvent{Event: "labeled", Label: name, Actor: login, At: time.Now()})
		}
		write(w, http.StatusOK, labelObjs(*ls))
	case reIssLabel.MatchString(p) && r.Method == http.MethodDelete:
		m := reIssLabel.FindStringSubmatch(p)
		n, _ := strconv.Atoi(m[1])
		ls := f.labelsOf(n)
		*ls = slices.DeleteFunc(*ls, func(x string) bool { return x == m[2] })
		f.events[n] = append(f.events[n], fakeEvent{Event: "unlabeled", Label: m[2], Actor: login, At: time.Now()})
		write(w, http.StatusOK, labelObjs(*ls))
	case reEvents.MatchString(p) && r.Method == http.MethodGet:
		var out []map[string]any
		for _, e := range f.events[num(reEvents, p)] {
			out = append(out, map[string]any{"event": e.Event, "label": map[string]string{"name": e.Label}, "actor": map[string]string{"login": e.Actor}, "created_at": e.At})
		}
		page(w, r, out)
	case p == "/repos/acme/study/pulls" && r.Method == http.MethodPost:
		for _, x := range f.pulls {
			if x.Head == str("head") && x.State == "open" {
				fail(w, http.StatusUnprocessableEntity, "Validation Failed: A pull request already exists for acme:"+x.Head+".")
				return
			}
		}
		head := "h1"
		if h, ok := f.branchHead[str("head")]; ok {
			head = h
		}
		pr := &fakePull{Number: f.next, Title: str("title"), Body: str("body"), Head: str("head"), HeadSHA: head, State: "open", Author: login, pending: 1, createdAt: time.Now(), updatedAt: time.Now()}
		f.pulls[f.next] = pr
		f.next++
		write(w, http.StatusCreated, f.pullJSON(pr))
	case p == "/repos/acme/study/pulls" && r.Method == http.MethodGet:
		q := r.URL.Query()
		var xs []*fakePull
		for i := 1; i < f.next; i++ {
			if x, ok := f.pulls[i]; ok && (q.Get("state") == "all" || q.Get("state") == x.State) {
				xs = append(xs, x)
			}
		}
		if q.Get("sort") == "updated" {
			slices.SortStableFunc(xs, func(a, b *fakePull) int { return b.updatedAt.Compare(a.updatedAt) })
		}
		var out []map[string]any
		for _, x := range xs {
			m := f.pullJSON(x)
			delete(m, "merged") // 一覧の応答には merged がない
			out = append(out, m)
		}
		if q.Get("per_page") != "" && q.Get("page") != "" && q.Get("sort") == "updated" {
			// 反映した pull request の一覧は、1 ページずつ読む（Link は見ない）。
			pp, _ := strconv.Atoi(q.Get("per_page"))
			pg, _ := strconv.Atoi(q.Get("page"))
			lo, hi := min((pg-1)*pp, len(out)), min(pg*pp, len(out))
			write(w, http.StatusOK, append([]map[string]any{}, out[lo:hi]...))
			return
		}
		page(w, r, out)
	case rePull.MatchString(p) && r.Method == http.MethodGet:
		x, ok := f.pulls[num(rePull, p)]
		if !ok {
			fail(w, http.StatusNotFound, "Not Found")
			return
		}
		write(w, http.StatusOK, f.pullJSON(x))
	case rePull.MatchString(p) && r.Method == http.MethodPatch:
		x := f.pulls[num(rePull, p)]
		if v, ok := in["body"].(string); ok {
			x.Body = v
		}
		if str("state") == "closed" {
			x.State = "closed"
			f.events[x.Number] = append(f.events[x.Number], fakeEvent{Event: "closed", Actor: login, At: time.Now()})
		}
		write(w, http.StatusOK, f.pullJSON(x))
	case reMerge.MatchString(p) && r.Method == http.MethodPut:
		x := f.pulls[num(reMerge, p)]
		if f.users[tok].noMerge {
			fail(w, http.StatusForbidden, "Must have push access to repository")
			return
		}
		// 承認を必須にする branch protection の応答（GitHub の文言）。
		if f.requireApproval && f.reviewDecision(x.Number) != "APPROVED" {
			fail(w, http.StatusMethodNotAllowed, "At least 1 approving review is required by reviewers with write access.")
			return
		}
		if f.ruleBlocked {
			fail(w, http.StatusMethodNotAllowed, "Repository rule violations found\n\nWaiting on required approvals from acme/gate.\n\n")
			return
		}
		if f.mergeFails > 0 {
			f.mergeFails--
			fail(w, http.StatusBadGateway, "失敗させる")
			return
		}
		switch {
		case x.Merged:
			fail(w, http.StatusMethodNotAllowed, "Pull Request is not mergeable")
		case str("sha") != x.HeadSHA:
			fail(w, http.StatusConflict, "Head branch was modified. Review and try the merge again.")
		case x.pending > 0 || x.conflict:
			fail(w, http.StatusMethodNotAllowed, "Pull Request is not mergeable")
		case str("merge_method") != "squash":
			fail(w, http.StatusUnprocessableEntity, "squash だけ")
		default:
			x.Merged, x.State, x.MergeSHA, x.mergedBy, x.updatedAt = true, "closed", "m"+x.HeadSHA, login, time.Now()
			for k, v := range f.branchFiles[x.Head] {
				f.files[k] = v
			}
			f.messages = append(f.messages, str("commit_title")+"\n\n"+str("commit_message"))
			f.commits++
			write(w, http.StatusOK, map[string]any{"sha": x.MergeSHA, "merged": true})
		}
	case reComments.MatchString(p) && r.Method == http.MethodPost:
		c := &fakeComment{ID: int64(len(f.comments) + 1), Issue: num(reComments, p), Body: str("body"), Author: login}
		f.comments = append(f.comments, c)
		write(w, http.StatusCreated, f.commentJSON(r, c))
	case reComments.MatchString(p) && r.Method == http.MethodGet:
		n := num(reComments, p)
		var out []map[string]any
		for _, c := range f.comments {
			if c.Issue == n {
				out = append(out, f.commentJSON(r, c))
			}
		}
		page(w, r, out)
	case p == "/repos/acme/study/issues" && r.Method == http.MethodPost:
		var labels []string
		for _, l := range in["labels"].([]any) {
			labels = append(labels, l.(string))
		}
		is := &fakeIssue{Number: f.next, Title: str("title"), Body: str("body"), State: "open", Labels: labels, Author: login}
		f.issues[f.next] = is
		f.next++
		write(w, http.StatusCreated, f.issueJSON(is))
	case p == "/repos/acme/study/issues" && r.Method == http.MethodGet:
		// GitHub の issue の一覧は、pull request も含む。
		var out []map[string]any
		for i := 1; i < f.next; i++ {
			if is, ok := f.issues[i]; ok {
				out = append(out, f.issueJSON(is))
			} else if x, ok := f.pulls[i]; ok {
				out = append(out, map[string]any{"number": x.Number, "title": x.Title, "state": x.State, "user": map[string]string{"login": x.Author}, "pull_request": map[string]string{}})
			}
		}
		page(w, r, out)
	case reIssue.MatchString(p) && r.Method == http.MethodGet:
		n := num(reIssue, p)
		if is, ok := f.issues[n]; ok {
			write(w, http.StatusOK, f.issueJSON(is))
		} else if _, ok := f.pulls[n]; ok {
			write(w, http.StatusOK, map[string]any{"number": n, "pull_request": map[string]string{}})
		} else {
			fail(w, http.StatusNotFound, "Not Found")
		}
	case reIssue.MatchString(p) && r.Method == http.MethodPatch:
		is := f.issues[num(reIssue, p)]
		is.State = str("state")
		if is.State == "closed" {
			now := time.Now()
			is.closedAt = &now
			f.events[is.Number] = append(f.events[is.Number], fakeEvent{Event: "closed", Actor: login, At: now})
		}
		write(w, http.StatusOK, f.issueJSON(is))
	default:
		f.t.Errorf("偽の GitHub が知らない要求：%s %s", r.Method, r.URL)
		fail(w, http.StatusNotFound, "Not Found")
	}
}

// bump は、ブランチの先頭を新しい SHA に進め、その SHA を返す。open の pull request の head も進める。
func (f *fakeGitHub) bump(branch string) string {
	f.branchSeq++
	sha := fmt.Sprintf("b%039d", f.branchSeq)
	f.branchHead[branch] = sha
	for _, x := range f.pulls {
		if x.Head == branch && x.State == "open" {
			x.HeadSHA = sha
		}
	}
	return sha
}

// branchOf は、先頭が sha のブランチを返す。
func (f *fakeGitHub) branchOf(sha string) string {
	for b, h := range f.branchHead {
		if h == sha {
			return b
		}
	}
	return ""
}

func (f *fakeGitHub) commentJSON(r *http.Request, c *fakeComment) map[string]any {
	return map[string]any{"id": c.ID, "body": c.Body, "user": map[string]string{"login": c.Author}, "created_at": time.Now(), "updated_at": time.Now(),
		"issue_url": fmt.Sprintf("http://%s/repos/acme/study/issues/%d", r.Host, c.Issue)}
}

func (f *fakeGitHub) issueJSON(is *fakeIssue) map[string]any {
	return map[string]any{"number": is.Number, "title": is.Title, "body": is.Body, "state": is.State, "user": map[string]string{"login": is.Author}, "created_at": time.Now(),
		"labels": labelObjs(is.Labels), "closed_at": is.closedAt}
}

// reviewDecision は、GitHub と同じく、書く権限のある人ごとのいちばん新しい approve か request changes から、承認の状態を決める。
// 呼ぶ側が f.mu を持つ。
func (f *fakeGitHub) reviewDecision(n int) string {
	last := map[string]string{}
	for _, rv := range f.reviews[n] {
		if (rv.State == "APPROVED" || rv.State == "CHANGES_REQUESTED") && f.writers[rv.Author] {
			last[rv.Author] = rv.State
		}
	}
	d := "REVIEW_REQUIRED"
	for _, s := range last {
		if s == "CHANGES_REQUESTED" {
			return "CHANGES_REQUESTED"
		}
		d = "APPROVED"
	}
	return d
}
