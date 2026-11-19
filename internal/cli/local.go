package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/yamambainc/logiforge/internal/gitx"
	"github.com/yamambainc/logiforge/internal/ids"
	"github.com/yamambainc/logiforge/internal/tree"
)

// abs は、作業ディレクトリからのパスにする。
func (a *App) abs(p string) string {
	if filepath.IsAbs(p) || a.Dir == "" {
		return p
	}
	return filepath.Join(a.Dir, p)
}

// ---- 認証 ----

func (a *App) authLogin(_ context.Context, args []string) error {
	f := newFlags("auth login")
	token, host := f.String("token", "", "トークン（コマンドの履歴に残るので、--with-token を勧める）"), f.String("host", "", "")
	withToken := f.Bool("with-token", false, "トークンを標準入力から読む")
	if err := f.parse(args); err != nil {
		return err
	}
	if *withToken {
		if a.Stdin == nil {
			return usagef("--with-token は、標準入力からトークンを読む")
		}
		b, err := io.ReadAll(io.LimitReader(a.Stdin, 64<<10))
		if err != nil {
			return err
		}
		*token = strings.TrimSpace(string(b))
		if *token == "" {
			return usagef("標準入力にトークンがない")
		}
	}
	if *token == "" && *host == "" {
		return usagef("auth login には --with-token か --token か --host が要る")
	}
	c, err := a.readConfig()
	if err != nil {
		return err
	}
	if *token != "" {
		c.Token = *token
	}
	if *host != "" {
		c.Host = strings.TrimRight(*host, "/")
	}
	p, err := a.configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	// 設定ファイルは、本人だけが読めるようにする。
	if err := os.WriteFile(p, b, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(p, 0o600); err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(map[string]string{"config": p})
	}
	fmt.Fprintf(a.Stdout, "%s に書いた\n", p)
	return nil
}

// authSetupGit は、git の credential helper に、この CLI を設定する。GitForge の URL にだけ使う。
// URL を指定しなければ、repository の git_url から求める。
func (a *App) authSetupGit(ctx context.Context, args []string) error {
	f := newFlags("auth setup-git")
	u := f.String("url", "", "GitForge の URL")
	insecure := f.Bool("insecure", false, "https でない URL にも、トークンを渡す")
	if err := f.parse(args); err != nil {
		return err
	}
	base := *u
	if base == "" {
		var page struct {
			Items []struct {
				GitURL string `json:"git_url"`
			} `json:"items"`
		}
		if err := a.getJSON(ctx, "/repositories", query("limit", "1"), &page); err != nil {
			return err
		}
		if len(page.Items) == 0 {
			return errors.New("repository がないので、GitForge の URL がわからない。--url で渡す")
		}
		pu, err := url.Parse(page.Items[0].GitURL)
		if err != nil {
			return err
		}
		base = pu.Scheme + "://" + pu.Host
	}
	if pu, err := url.Parse(base); err != nil || pu.Host == "" {
		return fmt.Errorf("GitForge の URL が正しくない：%s", base)
	} else if pu.Scheme != "https" && !*insecure {
		return fmt.Errorf("GitForge の URL %s が https でない。トークンが平文で流れるので、--insecure なしでは設定しない", base)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	key := "credential." + strings.TrimRight(base, "/") + ".helper"
	if _, err := a.git("config", "--global", "--replace-all", key, "!"+shellQuote(exe)+" auth git-credential"); err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(map[string]string{"url": base, "helper": key})
	}
	fmt.Fprintf(a.Stdout, "git は、%s で、logiforge のトークンを使う\n", base)
	return nil
}

func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// authGitCredential は、git の credential helper の「get」に答える。パスワードにトークンを入れる（design/cli/auth.md の「git の credential helper」）。
func (a *App) authGitCredential(ctx context.Context, args []string) error {
	if len(args) != 1 || args[0] != "get" {
		return nil // store と erase は、何もしない
	}
	// git が渡す属性のうち、host だけを使う。helper は、設定した URL にだけ使われる。
	var host string
	if a.Stdin != nil {
		sc := bufio.NewScanner(a.Stdin)
		for sc.Scan() && sc.Text() != "" {
			if v, ok := strings.CutPrefix(sc.Text(), "host="); ok {
				host = v
			}
		}
	}
	tok, err := a.token(ctx)
	if err != nil {
		return err
	}
	// gh のトークンは、その GitHub のホストにだけ渡す。
	if a.ghHost != "" && host != "" && host != a.ghHost {
		return fmt.Errorf("gh のトークンは %s にだけ渡す。%s には渡さない", a.ghHost, host)
	}
	fmt.Fprintf(a.Stdout, "username=logiforge\npassword=%s\n", tok)
	return nil
}

// ---- commit ----

// commit は、git commit に、セッションの trailer を付けて呼ぶ。
func (a *App) commit(_ context.Context, args []string) error {
	var sess string
	var rest []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--session" && i+1 < len(args):
			sess = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--session="):
			sess = strings.TrimPrefix(args[i], "--session=")
		default:
			rest = append(rest, args[i])
		}
	}
	session, err := a.session(sess)
	if err != nil {
		return err
	}
	return a.gitPassthrough(append([]string{"commit", "--trailer", "Logiforge-Session-Id: " + session}, rest...)...)
}

// ---- 手元の木 ----

// worktree は、手元の clone の作業ツリーである。
type worktree struct {
	// root は、repository の根（ディレクトリの repository では、そのディレクトリ）。gitRoot は、git の作業ツリーの根である。
	root, gitRoot string
	// dir は、ディレクトリの repository のパス。git リポジトリ全体の repository では空。
	dir   string
	files tree.Files
	tree  *tree.Tree
}

// directory は、clone の git の設定の logiforge.directory（ディレクトリの repository のパス）を返す。なければ空。
func (a *App) directory() string {
	out, err := a.git("config", "--get", "logiforge.directory")
	if err != nil {
		return ""
	}
	return strings.Trim(strings.TrimSpace(out), "/")
}

// readWorktree は、作業ツリーのファイル（追跡しているものと、無視していない追跡していないもの）を読む。
func (a *App) readWorktree() (*worktree, error) {
	root, err := a.git("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, errors.New("git の clone の中で使う")
	}
	dir := a.directory()
	out, err := a.git("-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	files := tree.Files{}
	for _, full := range strings.Split(out, "\x00") {
		if full == "" {
			continue
		}
		// ディレクトリの repository では、ディレクトリの中だけを、ディレクトリを根として読む。
		p := full
		if dir != "" {
			rest, ok := strings.CutPrefix(full, dir+"/")
			if !ok {
				continue
			}
			p = rest
		}
		full := filepath.Join(root, filepath.FromSlash(full))
		info, err := os.Lstat(full)
		if errors.Is(err, fs.ErrNotExist) {
			continue // 消したファイル
		}
		if err != nil {
			return nil, err
		}
		f := tree.File{Path: p, Size: info.Size(), Mode: tree.RegularMode}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			f.Mode = "120000"
		case info.IsDir():
			f.Mode = "160000"
		case info.Mode()&0o111 != 0:
			f.Mode = "100755"
		}
		if f.Mode == tree.RegularMode && f.Size <= tree.MaxArtifactSize {
			if f.Content, err = os.ReadFile(full); err != nil {
				return nil, err
			}
		}
		files[p] = f
	}
	w := &worktree{root: root, gitRoot: root, dir: dir, files: files, tree: tree.Build(files)}
	if dir != "" {
		w.root = filepath.Join(root, filepath.FromSlash(dir))
	}
	return w, nil
}

func (w *worktree) write(p string, content []byte) error {
	full := filepath.Join(w.root, filepath.FromSlash(p))
	if _, err := os.Stat(full); err == nil {
		return fmt.Errorf("%s はすでにある", p)
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	return os.WriteFile(full, content, 0o644)
}

// scaffold は、ひな形の結果である。
type scaffold struct {
	IDs   map[string]any `json:"ids"`
	Files []string       `json:"files"`
}

func (a *App) report(s scaffold) error {
	if a.json {
		return a.writeJSON(s)
	}
	for _, k := range sortedKeys(s.IDs) {
		fmt.Fprintf(a.Stdout, "%s：%v\n", k, s.IDs[k])
	}
	for _, p := range s.Files {
		fmt.Fprintf(a.Stdout, "作った：%s\n", p)
	}
	return nil
}

func (a *App) storyNew(_ context.Context, args []string) error {
	f := newFlags("story new")
	parent, title, summary := f.String("parent", "", ""), f.String("title", "", ""), f.String("summary", "", "")
	var hyps multi
	f.Var(&hyps, "hypothesis", "子仮説の主文（くり返せる）")
	if err := f.parse(args); err != nil {
		return err
	}
	if *parent == "" || *title == "" || len(hyps) == 0 {
		return usagef("story new には --parent、--title、--hypothesis が要る")
	}
	w, err := a.readWorktree()
	if err != nil {
		return err
	}
	h, ok := w.tree.Hypotheses[*parent]
	if !ok {
		return fmt.Errorf("仮説 %s が、手元の木にない", *parent)
	}
	sid := ids.New(ids.Story)
	sdir := h.Dir + "/" + sid
	out := scaffold{IDs: map[string]any{"story_id": sid}}
	if err := w.write(sdir+"/"+tree.StoryFile, tree.MarshalStory(tree.StoryContent{ID: sid, Title: *title, Summary: *summary})); err != nil {
		return err
	}
	out.Files = append(out.Files, sdir+"/"+tree.StoryFile)
	var hids []string
	for _, st := range hyps {
		hid := ids.New(ids.Hypothesis)
		p := sdir + "/" + hid + "/" + tree.HypothesisFile
		if err := w.write(p, tree.MarshalHypothesis(tree.HypothesisContent{ID: hid, Statement: st})); err != nil {
			return err
		}
		hids = append(hids, hid)
		out.Files = append(out.Files, p)
	}
	out.IDs["hypothesis_ids"] = hids
	return a.report(out)
}

func (a *App) storyInactivate(_ context.Context, args []string) error {
	f := newFlags("story inactivate")
	reason := f.String("reason", "", "")
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<ストーリー>"); err != nil {
		return err
	}
	if strings.TrimSpace(*reason) == "" {
		return usagef("story inactivate には --reason が要る")
	}
	w, err := a.readWorktree()
	if err != nil {
		return err
	}
	s, ok := w.tree.Stories[f.positional[0]]
	if !ok {
		return fmt.Errorf("ストーリー %s が、手元の木にない", f.positional[0])
	}
	p := s.Dir + "/" + tree.InactivatedFile
	if err := w.write(p, []byte(strings.TrimRight(*reason, "\n")+"\n")); err != nil {
		return err
	}
	return a.report(scaffold{IDs: map[string]any{"story_id": s.ID}, Files: []string{p}})
}

func (a *App) nodeEdit(_ context.Context, args []string) error {
	f := newFlags("node edit")
	file := f.String("append-background", "", "加える文章のファイル")
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<仮説>"); err != nil {
		return err
	}
	if *file == "" {
		return usagef("node edit には --append-background が要る")
	}
	add, err := os.ReadFile(a.abs(*file))
	if err != nil {
		return err
	}
	w, err := a.readWorktree()
	if err != nil {
		return err
	}
	h, ok := w.tree.Hypotheses[f.positional[0]]
	if !ok {
		return fmt.Errorf("仮説 %s が、手元の木にない", f.positional[0])
	}
	p := h.Dir + "/" + tree.BackgroundFile
	full := filepath.Join(w.root, filepath.FromSlash(p))
	cur, err := os.ReadFile(full)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(cur) > 0 && !bytes.HasSuffix(cur, []byte("\n")) {
		cur = append(cur, '\n')
	}
	if err := os.WriteFile(full, append(cur, add...), 0o644); err != nil {
		return err
	}
	return a.report(scaffold{IDs: map[string]any{"hypothesis_id": h.ID}, Files: []string{p}})
}

// verdictNew は、検証結果のひな形を作る。artifact のファイルは、中身の sha256 を求め、artifacts/sha256/ の正しい名前に写す。
// --cite answer:<人への依頼> は、回答の添付を、API の URL の link の artifact として書く（design/files/artifacts.md）。
func (a *App) verdictNew(ctx context.Context, args []string) error {
	f := newFlags("verdict new")
	repo, hyp, val, reportFile := repoFlag(f), f.String("hypothesis", "", ""), f.String("value", "", "true か false"), f.String("report", "", "レポートのファイル")
	var arts, links, cites, supersedes multi
	f.Var(&arts, "artifact", "artifact のファイル（くり返せる）")
	f.Var(&links, "link", "<名前>=<URL>（くり返せる）")
	f.Var(&cites, "cite", "subtree:<ノード>@<SHA>、artifact:<sha256>、answer:<人への依頼>（くり返せる）")
	f.Var(&supersedes, "supersedes", "差し替える検証結果（くり返せる）")
	if err := f.parse(args); err != nil {
		return err
	}
	if *hyp == "" || *reportFile == "" || (*val != "true" && *val != "false") {
		return usagef("verdict new には --hypothesis、--value true|false、--report が要る")
	}
	report, err := os.ReadFile(a.abs(*reportFile))
	if err != nil {
		return err
	}
	w, err := a.readWorktree()
	if err != nil {
		return err
	}
	h, ok := w.tree.Hypotheses[*hyp]
	if !ok {
		return fmt.Errorf("仮説 %s が、手元の木にない", *hyp)
	}
	vid := ids.New(ids.Verdict)
	vc := tree.VerdictContent{ID: vid, Value: *val == "true", Supersedes: supersedes, Report: string(report)}
	out := scaffold{IDs: map[string]any{"verdict_id": vid}}
	for _, p := range arts {
		data, err := os.ReadFile(a.abs(p))
		if err != nil {
			return err
		}
		if len(data) > tree.MaxArtifactSize {
			return fmt.Errorf("%s が、artifact の上限（10 MiB）を超える", p)
		}
		sha := tree.SHA256Hex(data)
		ap := tree.ArtifactPath(sha)
		if _, exists := w.files[ap]; !exists {
			if err := w.write(ap, data); err != nil {
				return err
			}
			out.Files = append(out.Files, ap)
		}
		vc.Artifacts = append(vc.Artifacts, tree.ArtifactRef{Kind: "file", Name: filepath.Base(p), SHA256: sha})
	}
	for _, l := range links {
		name, u, ok := strings.Cut(l, "=")
		if !ok || name == "" || u == "" {
			return usagef("--link は <名前>=<URL>")
		}
		vc.Artifacts = append(vc.Artifacts, tree.ArtifactRef{Kind: "link", Name: name, URI: u})
	}
	for _, c := range cites {
		kind, target, _ := strings.Cut(c, ":")
		switch kind {
		case "subtree":
			node, rev, ok := strings.Cut(target, "@")
			if !ok {
				return usagef("--cite subtree:<ノード>@<SHA>")
			}
			vc.Citations = append(vc.Citations, tree.Citation{TargetKind: "subtree", NodeID: node, Commit: rev})
		case "artifact":
			vc.Citations = append(vc.Citations, tree.Citation{TargetKind: "artifact", SHA256: target})
		case "answer":
			refs, err := a.answerLinks(ctx, *repo, target)
			if err != nil {
				return err
			}
			vc.Artifacts = append(vc.Artifacts, refs...)
		default:
			return usagef("--cite の種類は subtree、artifact、answer のどれか")
		}
	}
	p := h.Dir + "/" + tree.VerdictsDir + "/" + vid + ".md"
	if err := w.write(p, tree.MarshalVerdict(vc)); err != nil {
		return err
	}
	out.Files = append([]string{p}, out.Files...)
	return a.report(out)
}

// answerLinks は、人への依頼の回答の添付を、API の URL の link の artifact にする。
func (a *App) answerLinks(ctx context.Context, repoFlag, id string) ([]tree.ArtifactRef, error) {
	name, err := a.repo(repoFlag)
	if err != nil {
		return nil, err
	}
	host, err := a.host()
	if err != nil {
		return nil, err
	}
	var hr struct {
		Answer *struct {
			Attachments []struct {
				Name   string `json:"name"`
				SHA256 string `json:"sha256"`
				URI    string `json:"uri"`
			} `json:"attachments"`
		} `json:"answer"`
	}
	if err := a.getJSON(ctx, repoPath(name)+"/human-requests/"+url.PathEscape(id), nil, &hr); err != nil {
		return nil, err
	}
	if hr.Answer == nil {
		return nil, fmt.Errorf("人への依頼 %s には、まだ回答がない", id)
	}
	var out []tree.ArtifactRef
	for _, at := range hr.Answer.Attachments {
		out = append(out, tree.ArtifactRef{Kind: "link", Name: at.Name, URI: host + at.URI, SHA256: at.SHA256})
	}
	if len(out) == 0 {
		out = append(out, tree.ArtifactRef{Kind: "link", Name: "回答 " + id, URI: host + "/v1" + repoPath(name) + "/human-requests/" + id})
	}
	return out, nil
}

// ---- check ----

// checkResult は、check の結果である。
type checkResult struct {
	OK         bool             `json:"ok"`
	Base       string           `json:"base"`
	Rejected   []tree.Violation `json:"rejected"`
	Warnings   []string         `json:"warnings"`
	Operations []tree.Operation `json:"operations"`
}

// check は、手元の作業ツリーを検査する。バックエンドの差分の検査と同じコードを使い、base もサーバと同じにする（design/cli/local.md）。
//   - base は、HEAD と origin/main の分かれた点（git merge-base）である。
//   - 新しい識別子が、origin/main の木とぶつからないかも確かめる。
//   - 差分を origin/main に重ねられないとき（origin/main でも同じファイルが変わった）は、警告する。
//
// 手元でできないもの（引用したコミットの確認、inactive になる前の状態）は省く。API は呼ばない。
func (a *App) check(_ context.Context, args []string) error {
	f := newFlags("check")
	if err := f.parse(args); err != nil {
		return err
	}
	w, err := a.readWorktree()
	if err != nil {
		return err
	}
	mainSHA, err := a.git("-C", w.gitRoot, "rev-parse", "--verify", "refs/remotes/origin/main^{commit}")
	if err != nil {
		return errors.New("origin/main がない。git fetch origin を先に行う")
	}
	baseSHA, err := a.git("-C", w.gitRoot, "merge-base", "HEAD", mainSHA)
	if err != nil {
		return err
	}
	repoDir, err := a.git("-C", w.gitRoot, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return err
	}
	g := &gitx.Repo{Dir: repoDir, Prefix: w.dir}
	base, err := g.ReadFiles(context.Background(), baseSHA, tree.MaxArtifactSize)
	if err != nil {
		return err
	}
	main := base
	if mainSHA != baseSHA {
		if main, err = g.ReadFiles(context.Background(), mainSHA, tree.MaxArtifactSize); err != nil {
			return err
		}
	}
	res, err := tree.CheckChange(base, w.files, tree.Context{Others: []*tree.Tree{tree.Build(main)}})
	if err != nil {
		return err
	}
	out := checkResult{OK: res.OK(), Base: baseSHA, Rejected: res.Rejected, Operations: res.Operations, Warnings: []string{}}
	if out.Rejected == nil {
		out.Rejected = []tree.Violation{}
	}
	if out.Operations == nil {
		out.Operations = []tree.Operation{}
	}
	for _, v := range res.Warnings {
		out.Warnings = append(out.Warnings, fmt.Sprintf("%s（%s）：%s（base でも破れている）", v.Rule, v.Node, v.Detail))
	}
	// origin/main に重ねられるか。差分が変えたファイルが、base のあとに origin/main でも変わっていれば、重ねられない。
	if mainSHA != baseSHA {
		mainChanged := map[string]bool{}
		for _, p := range tree.ChangedPaths(base, main) {
			mainChanged[p] = true
		}
		for _, p := range tree.ChangedPaths(base, w.files) {
			if mainChanged[p] {
				out.Warnings = append(out.Warnings, fmt.Sprintf("%s は、origin/main でも変わっている。今の main に重ねられないので、反映で conflict になる。origin/main に合わせて直す", p))
			}
		}
	}
	if status, _ := a.git("-C", w.root, "status", "--porcelain"); status != "" {
		out.Warnings = append(out.Warnings, "コミットしていない変更がある。check は作業ツリーを見るが、push するのは HEAD である")
	}
	if trailer, _ := a.git("-C", w.root, "log", "-1", "--format=%(trailers:key=Logiforge-Session-Id,valueonly)"); strings.TrimSpace(trailer) == "" {
		out.Warnings = append(out.Warnings, `先頭のコミットに Logiforge-Session-Id: がない。approve できないので、git commit --amend --trailer "Logiforge-Session-Id: <ID>" で付ける`)
	}
	if a.json {
		if err := a.writeJSON(out); err != nil {
			return err
		}
	} else {
		for _, v := range out.Rejected {
			fmt.Fprintf(a.Stdout, "誤り：%s（%s）：%s\n", v.Rule, v.Node, v.Detail)
		}
		for _, wmsg := range out.Warnings {
			fmt.Fprintf(a.Stdout, "警告：%s\n", wmsg)
		}
		for _, op := range out.Operations {
			fmt.Fprintf(a.Stdout, "操作：%s %s%s\n", op.Kind, op.StoryID, op.HypothesisID)
		}
		if out.OK {
			fmt.Fprintf(a.Stdout, "check：通った（%d 件の操作）\n", len(out.Operations))
		}
	}
	if !out.OK {
		return &silent{fmt.Errorf("check：%d 件の誤り", len(out.Rejected))}
	}
	return nil
}

// silent は、結果をすでに書いたので、もう書かないエラーである。終了コードは 1 にする。
type silent struct{ error }
