package github

// 単体のテストに使う、GitHub の REST の偽物。アダプタが呼ぶものだけを、GitHub の応答の形で返す。
// 一覧は 2 件ずつのページにし、Link の next を辿ることも確かめる。

import (
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
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
	base string
}

type fakeIssue struct {
	Number int
	Title  string
	Body   string
	State  string
	Labels []string
	Author string
}

type fakeGitHub struct {
	t   *testing.T
	srv *httptest.Server
	// private なら、トークンなしでは読めない。
	private bool
	// tree は、Git Data API で作りかけのツリー（ツリーの SHA → パス → 中身）。gitCommits は、コミットの SHA → ツリーの SHA とメッセージ。
	trees      map[string]map[string]string
	gitCommits map[string][2]string

	mu       sync.Mutex
	next     int // pull request と issue の番号（共通）
	pulls    map[int]*fakePull
	issues   map[int]*fakeIssue
	comments []*fakeComment
	files    map[string]string
	commits  int
	// users は、トークンから、ログイン名と team（org/slug）を引く。
	users map[string]fakeUser
	// readable は、トークンごとに読める repository。
	readable map[string]bool
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
}

type fakeUser struct {
	login string
	teams []string
	// noMerge なら、merge を 403 で断る（Write の権限がない）。
	noMerge bool
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	f := &fakeGitHub{t: t, next: 1, trees: map[string]map[string]string{}, gitCommits: map[string][2]string{}, pulls: map[int]*fakePull{}, issues: map[int]*fakeIssue{}, files: map[string]string{},
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
		"merged": p.Merged, "merge_commit_sha": p.MergeSHA, "merged_at": nil}
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

var (
	rePull     = regexp.MustCompile(`^/repos/acme/study/pulls/(\d+)$`)
	reMerge    = regexp.MustCompile(`^/repos/acme/study/pulls/(\d+)/merge$`)
	reIssue    = regexp.MustCompile(`^/repos/acme/study/issues/(\d+)$`)
	reComments = regexp.MustCompile(`^/repos/acme/study/issues/(\d+)/comments$`)
	reContents = regexp.MustCompile(`^/repos/acme/study/contents/(.+)$`)
)

func num(re *regexp.Regexp, p string) int {
	n, _ := strconv.Atoi(re.FindStringSubmatch(p)[1])
	return n
}

func (f *fakeGitHub) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	case p == "/user/teams":
		u := f.users[tok]
		var ts []map[string]any
		for _, t := range u.teams {
			org, slug, _ := strings.Cut(t, "/")
			ts = append(ts, map[string]any{"slug": slug, "organization": map[string]string{"login": org}})
		}
		page(w, r, ts)
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
	case strings.HasPrefix(p, "/repos/acme/study/commits/") && strings.HasSuffix(p, "/pulls") && r.Method == http.MethodGet:
		sha := strings.TrimSuffix(strings.TrimPrefix(p, "/repos/acme/study/commits/"), "/pulls")
		var out []map[string]any
		for i := 1; i < f.next; i++ {
			if x, ok := f.pulls[i]; ok && x.MergeSHA == sha {
				m := f.pullJSON(x)
				delete(m, "merged_by") // 一覧の応答には merged_by がない
				out = append(out, m)
			}
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
		pr := &fakePull{Number: f.next, Title: str("title"), Body: str("body"), Head: str("head"), HeadSHA: head, State: "open", Author: login, pending: 1, createdAt: time.Now()}
		f.pulls[f.next] = pr
		f.next++
		write(w, http.StatusCreated, f.pullJSON(pr))
	case p == "/repos/acme/study/pulls" && r.Method == http.MethodGet:
		var out []map[string]any
		for i := 1; i < f.next; i++ {
			if x, ok := f.pulls[i]; ok {
				m := f.pullJSON(x)
				delete(m, "merged") // 一覧の応答には merged がない
				out = append(out, m)
			}
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
		}
		write(w, http.StatusOK, f.pullJSON(x))
	case reMerge.MatchString(p) && r.Method == http.MethodPut:
		x := f.pulls[num(reMerge, p)]
		if f.users[tok].noMerge {
			fail(w, http.StatusForbidden, "Must have push access to repository")
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
			x.Merged, x.State, x.MergeSHA, x.mergedBy = true, "closed", "m"+x.HeadSHA, login
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
	case p == "/repos/acme/study/issues/comments" && r.Method == http.MethodGet:
		var out []map[string]any
		for _, c := range f.comments {
			out = append(out, f.commentJSON(r, c))
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
	return map[string]any{"number": is.Number, "title": is.Title, "body": is.Body, "state": is.State, "user": map[string]string{"login": is.Author}, "created_at": time.Now()}
}
