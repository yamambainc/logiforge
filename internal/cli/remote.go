package cli

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// repoFlag は、--repo を足す。
func repoFlag(f *flags) *string { return f.String("repo", "", "repository の名前") }

// ---- repository ----

func (a *App) repoList(ctx context.Context, args []string) error {
	f := newFlags("repo list")
	if err := f.parse(args); err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: "/repositories", query: url.Values{"limit": {"200"}}})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		for _, r := range items(v) {
			fmt.Fprintf(a.Stdout, "%s\t根の値：%s\n", str(r, "name"), value(r["root_value"]))
		}
	})
}

func (a *App) repoView(ctx context.Context, args []string) error {
	f := newFlags("repo view")
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<repository>"); err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(f.positional[0])})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		fmt.Fprintf(a.Stdout, "名前：%s\ngit：%s\n根の仮説：%s（%s）\n", str(v, "name"), str(v, "git_url"), str(v, "root_hypothesis_id"), value(v["root_value"]))
	})
}

// repoClone は、repository を git で clone し、clone の git の設定に repository の名前（logiforge.repository）を書く。
func (a *App) repoClone(ctx context.Context, args []string) error {
	f := newFlags("repo clone")
	if err := f.parse(args); err != nil {
		return err
	}
	if len(f.positional) < 1 || len(f.positional) > 2 {
		return usagef("repo clone には <repository> [<ディレクトリ>] が要る")
	}
	name := f.positional[0]
	dir := name
	if len(f.positional) == 2 {
		dir = f.positional[1]
	}
	var r struct {
		GitURL string `json:"git_url"`
	}
	if err := a.getJSON(ctx, repoPath(name), nil, &r); err != nil {
		return err
	}
	if err := a.gitPassthrough("clone", "--", r.GitURL, dir); err != nil {
		return err
	}
	if _, err := a.git("-C", dir, "config", "logiforge.repository", name); err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(map[string]string{"repository": name, "directory": dir, "git_url": r.GitURL})
	}
	fmt.Fprintf(a.Stdout, "%s を %s に clone した\n", name, dir)
	return nil
}

// ---- 木 ----

func (a *App) treeView(ctx context.Context, args []string) error {
	f := newFlags("tree view")
	repo, rev := repoFlag(f), f.String("revision", "", "")
	if err := f.parse(args); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/tree", query: query("revision", *rev)})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		fmt.Fprintf(a.Stdout, "リビジョン：%s\n根の仮説：%s（%s）\n", str(v, "revision"), str(v, "root_hypothesis_id"), value(v["root_value"]))
		if ws, _ := v["warnings"].([]any); len(ws) > 0 {
			fmt.Fprintln(a.Stdout, "警告：")
			for _, w := range ws {
				fmt.Fprintf(a.Stdout, "  %v\n", w)
			}
		}
	})
}

func query(kv ...string) url.Values {
	q := url.Values{}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			q.Set(kv[i], kv[i+1])
		}
	}
	return q
}

func printNode(a *App, n map[string]any, indent string) {
	switch str(n, "node_type") {
	case "hypothesis":
		marks := ""
		if n["leaf"] == true {
			marks += " 葉"
		}
		if n["needs_reconsideration"] == true {
			marks += " 要再検討"
		}
		if n["inactive"] == true {
			marks += " inactive"
		}
		fmt.Fprintf(a.Stdout, "%s%s [%s%s] %s\n", indent, str(n, "id"), value(n["value"]), marks, str(n, "statement"))
	case "story":
		fmt.Fprintf(a.Stdout, "%s%s [%s %s] %s\n", indent, str(n, "id"), value(n["value"]), str(n, "state"), str(n, "title"))
	}
}

func (a *App) treeNodes(ctx context.Context, args []string) error {
	f := newFlags("tree nodes")
	repo, rev, root := repoFlag(f), f.String("revision", "", ""), f.String("root", "", "")
	depth := f.Int("depth", -1, "")
	if err := f.parse(args); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	q := query("revision", *rev, "root", *root)
	if *depth >= 0 {
		q.Set("depth", strconv.Itoa(*depth))
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/tree/nodes", query: q})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		parents := map[string]string{}
		nodes, _ := v["nodes"].([]any)
		for _, x := range nodes {
			n := x.(map[string]any)
			p := str(n, "parent_story_id")
			if p == "" {
				p = str(n, "parent_hypothesis_id")
			}
			parents[str(n, "id")] = p
		}
		for _, x := range nodes {
			n := x.(map[string]any)
			d := 0
			for p := parents[str(n, "id")]; p != ""; p = parents[p] {
				if _, ok := parents[p]; !ok {
					break
				}
				d++
			}
			printNode(a, n, strings.Repeat("  ", d))
		}
	})
}

func (a *App) treeCompletion(ctx context.Context, args []string) error {
	f := newFlags("tree completion")
	repo := repoFlag(f)
	if err := f.parse(args); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/tree/completion"})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		fmt.Fprintf(a.Stdout, "根の値：%s\n残りの作業：%s\n終わり：%s\n", value(v["root_value"]), str(v, "remaining_work"), str(v, "done"))
	})
}

func (a *App) treeExport(ctx context.Context, args []string) error {
	f := newFlags("tree export")
	repo, rev, format := repoFlag(f), f.String("revision", "", ""), f.String("format", "", "json か markdown")
	if err := f.parse(args); err != nil {
		return err
	}
	if *format != "json" && *format != "markdown" {
		return usagef("tree export には --format json|markdown が要る")
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/tree/export", query: query("revision", *rev, "format", *format)})
	if err != nil {
		return err
	}
	_, err = a.Stdout.Write(res.body)
	return err
}

func (a *App) nodeView(ctx context.Context, args []string) error {
	f := newFlags("node view")
	repo, rev := repoFlag(f), f.String("revision", "", "")
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<ノード>"); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/nodes/" + url.PathEscape(f.positional[0]), query: query("revision", *rev)})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		if n, ok := v["node"].(map[string]any); ok {
			printNode(a, n, "")
			if bg := str(n, "background"); bg != "" {
				fmt.Fprintf(a.Stdout, "\n背景情報：\n%s\n", bg)
			}
		}
		if cs, _ := v["children"].([]any); len(cs) > 0 {
			fmt.Fprintln(a.Stdout, "\n子：")
			for _, c := range cs {
				printNode(a, c.(map[string]any), "  ")
			}
		}
		if vd, ok := v["verdict"].(map[string]any); ok {
			fmt.Fprintf(a.Stdout, "\n検証結果 %s：%s\n%s\n", str(vd, "id"), value(vd["value"]), str(vd, "report_body"))
		}
	})
}

func (a *App) nodeHistory(ctx context.Context, args []string) error {
	f := newFlags("node history")
	repo := repoFlag(f)
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<ノード>"); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/nodes/" + url.PathEscape(f.positional[0]) + "/history", query: query("limit", "200")})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		for _, e := range items(v) {
			fmt.Fprintf(a.Stdout, "%s\t%s\t%s\n", str(e, "revision"), str(e, "committed_at"), str(e, "apply_request_id"))
		}
	})
}

func (a *App) verdictView(ctx context.Context, args []string) error {
	f := newFlags("verdict view")
	repo := repoFlag(f)
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<検証結果>"); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/verdicts/" + url.PathEscape(f.positional[0])})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		fmt.Fprintf(a.Stdout, "%s（仮説 %s）：%s\n反映：%s\n\n%s\n", str(v, "id"), str(v, "hypothesis_id"), value(v["value"]), str(v, "submitted_revision"), str(v, "report_body"))
	})
}

func (a *App) artifactGet(ctx context.Context, args []string) error {
	f := newFlags("artifact get")
	repo, rev, out := repoFlag(f), f.String("revision", "", ""), f.String("o", "", "書き出すファイル")
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<sha256>"); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/artifacts/" + url.PathEscape(f.positional[0]), query: query("revision", *rev)})
	if err != nil {
		return err
	}
	if *out == "" {
		_, err = a.Stdout.Write(res.body)
		return err
	}
	if err := os.WriteFile(a.abs(*out), res.body, 0o644); err != nil {
		return err
	}
	if a.json {
		return a.writeJSON(map[string]any{"path": *out, "size": len(res.body)})
	}
	return nil
}

// ---- apply request ----

// arCreate は、push したブランチから apply request を作る。手元の先頭と remote の先頭が同じかを確かめてから、revision に入れて送る。
// Idempotency-Key は、repository・ブランチ・先頭と、そのブランチの閉じた（反映か close された）apply request の数から作る。
//   - 同じブランチの同じ先頭なら、送り直しても同じ apply request になる。
//   - close したあと、同じブランチの同じ先頭から作り直すと、閉じた数が増えているので、新しい apply request になる。
func (a *App) arCreate(ctx context.Context, args []string) error {
	f := newFlags("ar create")
	repo, branch, desc, sess := repoFlag(f), f.String("branch", "", ""), f.String("description", "", ""), f.String("session", "", "")
	if err := f.parse(args); err != nil {
		return err
	}
	if *branch == "" {
		return usagef("ar create には --branch が要る")
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	session, err := a.session(*sess)
	if err != nil {
		return err
	}
	local, err := a.git("rev-parse", "--verify", "--end-of-options", "refs/heads/"+*branch)
	if err != nil {
		return fmt.Errorf("手元にブランチ %s がない", *branch)
	}
	remote, err := a.git("ls-remote", "--heads", "origin", "refs/heads/"+*branch)
	if err != nil {
		return err
	}
	if fields := strings.Fields(remote); len(fields) == 0 || fields[0] != local {
		return fmt.Errorf("ブランチ %s の手元の先頭 %s が、remote の先頭と違う。先に git push する", *branch, local)
	}
	closed, err := a.closedOnBranch(ctx, name, *branch)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(name + "\n" + *branch + "\n" + local + "\n" + strconv.Itoa(closed)))
	res, err := a.call(ctx, request{method: http.MethodPost, path: repoPath(name) + "/apply-requests", session: session,
		idempotency: "cli-" + hex.EncodeToString(sum[:16]),
		body:        map[string]string{"branch": *branch, "revision": local, "description": *desc}})
	if err != nil {
		return err
	}
	return a.show(res, a.printAR)
}

// closedOnBranch は、ブランチの、閉じた（反映か close された）apply request の数を返す。
func (a *App) closedOnBranch(ctx context.Context, repo, branch string) (int, error) {
	n := 0
	cursor := ""
	for {
		var page struct {
			Items []struct {
				Branch string `json:"branch"`
				State  string `json:"state"`
			} `json:"items"`
			NextCursor *string `json:"next_cursor"`
		}
		if err := a.getJSON(ctx, repoPath(repo)+"/apply-requests", query("limit", "200", "cursor", cursor), &page); err != nil {
			return 0, err
		}
		for _, ar := range page.Items {
			if ar.Branch == branch && ar.State != "open" {
				n++
			}
		}
		if page.NextCursor == nil {
			return n, nil
		}
		cursor = *page.NextCursor
	}
}

func (a *App) printAR(v map[string]any) {
	fmt.Fprintf(a.Stdout, "%s（%s）ブランチ %s、版 %s\n", str(v, "id"), str(v, "state"), str(v, "branch"), str(v, "version"))
	if aw := str(v, "awaiting"); aw != "" {
		fmt.Fprintf(a.Stdout, "待っているもの：%s\n", aw)
	}
	if ops, _ := v["operations"].([]any); len(ops) > 0 {
		fmt.Fprintln(a.Stdout, "操作：")
		for _, o := range ops {
			op := o.(map[string]any)
			fmt.Fprintf(a.Stdout, "  %s %s%s%s\n", str(op, "kind"), str(op, "story_id"), str(op, "hypothesis_id"), str(op, "verdict_id"))
		}
	}
	if rs, _ := v["reviews"].([]any); len(rs) > 0 {
		fmt.Fprintln(a.Stdout, "レビュー：")
		for _, r := range rs {
			rv := r.(map[string]any)
			fmt.Fprintf(a.Stdout, "  %s 版 %s（有効：%s）\n", str(rv, "kind"), str(rv, "version"), str(rv, "valid"))
		}
	}
	if fs, _ := v["merge_failures"].([]any); len(fs) > 0 {
		fmt.Fprintln(a.Stdout, "反映の失敗：")
		for _, x := range fs {
			mf := x.(map[string]any)
			fmt.Fprintf(a.Stdout, "  版 %s：%s %s\n", str(mf, "version"), str(mf, "reason"), str(mf, "detail"))
		}
	}
	if ws, _ := v["warnings"].([]any); len(ws) > 0 {
		fmt.Fprintln(a.Stdout, "警告：")
		for _, w := range ws {
			fmt.Fprintf(a.Stdout, "  %v\n", w)
		}
	}
}

func (a *App) arEdit(ctx context.Context, args []string) error {
	f := newFlags("ar edit")
	repo, desc, sess := repoFlag(f), f.String("description", "", ""), f.String("session", "", "")
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<apply request>"); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	session, err := a.session(*sess)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodPatch, path: repoPath(name) + "/apply-requests/" + url.PathEscape(f.positional[0]), session: session,
		body: map[string]string{"description": *desc}})
	if err != nil {
		return err
	}
	return a.show(res, a.printAR)
}

func (a *App) arView(ctx context.Context, args []string) error {
	f := newFlags("ar view")
	repo := repoFlag(f)
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<apply request>"); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/apply-requests/" + url.PathEscape(f.positional[0])})
	if err != nil {
		return err
	}
	return a.show(res, a.printAR)
}

func (a *App) arList(ctx context.Context, args []string) error {
	f := newFlags("ar list")
	repo, state := repoFlag(f), f.String("state", "", "")
	if err := f.parse(args); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/apply-requests", query: query("state", *state, "limit", "200")})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		for _, ar := range items(v) {
			fmt.Fprintf(a.Stdout, "%s\t%s\t%s\t版 %s\t%s\n", str(ar, "id"), str(ar, "state"), str(ar, "branch"), str(ar, "version"), str(ar, "awaiting"))
		}
	})
}

func (a *App) arMaterial(ctx context.Context, args []string) error {
	f := newFlags("ar material")
	repo := repoFlag(f)
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<apply request>"); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/apply-requests/" + url.PathEscape(f.positional[0]) + "/review-material"})
	if err != nil {
		return err
	}
	return a.show(res, nil)
}

// arReview は、レビューを付ける。--version の番号は、ar view で SHA に引き直してから、revision として送る。
func (a *App) arReview(ctx context.Context, args []string) error {
	f := newFlags("ar review")
	repo, sess := repoFlag(f), f.String("session", "", "")
	approve, changes, comment := f.Bool("approve", false, ""), f.Bool("request-changes", false, ""), f.Bool("comment", false, "")
	version, revision := f.Int("version", 0, ""), f.String("revision", "", "")
	body, prompt := f.String("body", "", ""), f.String("prompt-revision", "", "")
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<apply request>"); err != nil {
		return err
	}
	kinds := map[bool]string{}
	n := 0
	for _, k := range []struct {
		on   bool
		kind string
	}{{*approve, "approve"}, {*changes, "request_changes"}, {*comment, "comment"}} {
		if k.on {
			kinds[true] = k.kind
			n++
		}
	}
	if n != 1 {
		return usagef("ar review には --approve、--request-changes、--comment のどれか 1 つが要る")
	}
	if (*version == 0) == (*revision == "") {
		return usagef("ar review には --version か --revision のどちらか 1 つが要る")
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	session, err := a.session(*sess)
	if err != nil {
		return err
	}
	id := f.positional[0]
	rev := *revision
	if *version != 0 {
		var ar struct {
			Versions []struct {
				Version  int    `json:"version"`
				Revision string `json:"revision"`
			} `json:"versions"`
		}
		if err := a.getJSON(ctx, repoPath(name)+"/apply-requests/"+url.PathEscape(id), nil, &ar); err != nil {
			return err
		}
		for _, v := range ar.Versions {
			if v.Version == *version {
				rev = v.Revision
			}
		}
		if rev == "" {
			return fmt.Errorf("版 %d がない", *version)
		}
	}
	res, err := a.call(ctx, request{method: http.MethodPost, path: repoPath(name) + "/apply-requests/" + url.PathEscape(id) + "/reviews", session: session,
		body: map[string]string{"kind": kinds[true], "revision": rev, "body": *body, "prompt_revision": *prompt}})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		if ar, ok := v["apply_request"].(map[string]any); ok {
			a.printAR(ar)
		}
	})
}

func (a *App) arClose(ctx context.Context, args []string) error {
	f := newFlags("ar close")
	repo, reason, sess := repoFlag(f), f.String("reason", "", ""), f.String("session", "", "")
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<apply request>"); err != nil {
		return err
	}
	if *reason == "" {
		return usagef("ar close には --reason が要る")
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	session, err := a.session(*sess)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodPost, path: repoPath(name) + "/apply-requests/" + url.PathEscape(f.positional[0]) + "/close", session: session,
		body: map[string]string{"reason": *reason}})
	if err != nil {
		return err
	}
	return a.show(res, a.printAR)
}

// arCleanup は、反映か close された apply request のブランチのうち、手元にあるものだけを、手元と remote から消す。
func (a *App) arCleanup(ctx context.Context, args []string) error {
	f := newFlags("ar cleanup")
	repo := repoFlag(f)
	if err := f.parse(args); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	var page struct {
		Items []struct {
			Branch string `json:"branch"`
			State  string `json:"state"`
		} `json:"items"`
	}
	if err := a.getJSON(ctx, repoPath(name)+"/apply-requests", query("limit", "200"), &page); err != nil {
		return err
	}
	open := map[string]bool{}
	for _, ar := range page.Items {
		if ar.State == "open" {
			open[ar.Branch] = true
		}
	}
	current, _ := a.git("symbolic-ref", "--short", "-q", "HEAD")
	var deleted []string
	for _, ar := range page.Items {
		if ar.State == "open" || open[ar.Branch] || ar.Branch == current {
			continue
		}
		if _, err := a.git("rev-parse", "--verify", "--quiet", "--end-of-options", "refs/heads/"+ar.Branch); err != nil {
			continue // 手元にないブランチ（ほかの人のもの）は消さない
		}
		if _, err := a.git("branch", "-D", "--", ar.Branch); err != nil {
			return err
		}
		_, _ = a.git("push", "--quiet", "origin", "--delete", "--", ar.Branch)
		deleted = append(deleted, ar.Branch)
	}
	if a.json {
		if deleted == nil {
			deleted = []string{}
		}
		return a.writeJSON(map[string]any{"deleted": deleted})
	}
	for _, b := range deleted {
		fmt.Fprintf(a.Stdout, "ブランチ %s を消した\n", b)
	}
	return nil
}

// ---- 作業と人への依頼 ----

func (a *App) workList(ctx context.Context, args []string) error {
	f := newFlags("work list")
	repo, subtree, kind := repoFlag(f), f.String("subtree", "", ""), f.String("kind", "", "")
	claimed := f.Bool("include-claimed", false, "")
	if err := f.parse(args); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	q := query("subtree", *subtree, "kind", *kind, "limit", "200")
	if *claimed {
		q.Set("include_claimed", "true")
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/work", query: q})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		for _, it := range items(v) {
			fmt.Fprintf(a.Stdout, "%s\t%s\t%s\n", str(it, "kind"), str(it, "subject_id"), str(it, "node_id"))
		}
	})
}

func (a *App) hrCreate(ctx context.Context, args []string) error {
	f := newFlags("human-request create")
	repo, node, kind, bodyFile, sess := repoFlag(f), f.String("node", "", ""), f.String("kind", "", ""), f.String("body", "", "本文のファイル"), f.String("session", "", "")
	key := f.String("idempotency-key", "", "")
	if err := f.parse(args); err != nil {
		return err
	}
	if *node == "" || *kind == "" || *bodyFile == "" {
		return usagef("human-request create には --node、--kind、--body が要る")
	}
	body, err := os.ReadFile(a.abs(*bodyFile))
	if err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	session, err := a.session(*sess)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodPost, path: repoPath(name) + "/human-requests", session: session, idempotency: *key,
		body: map[string]string{"node_id": *node, "kind": *kind, "body": string(body)}})
	if err != nil {
		return err
	}
	return a.show(res, a.printHR)
}

func (a *App) printHR(v map[string]any) {
	fmt.Fprintf(a.Stdout, "%s（%s、%s）ノード %s\n%s\n", str(v, "id"), str(v, "kind"), str(v, "state"), str(v, "node_id"), str(v, "body"))
	if ans, ok := v["answer"].(map[string]any); ok {
		fmt.Fprintf(a.Stdout, "\n回答（%s）：\n%s\n", str(ans, "answered_at"), str(ans, "body"))
		if as, _ := ans["attachments"].([]any); len(as) > 0 {
			for _, x := range as {
				at := x.(map[string]any)
				fmt.Fprintf(a.Stdout, "  添付 %s：%s\n", str(at, "name"), str(at, "uri"))
			}
		}
	}
}

func (a *App) hrList(ctx context.Context, args []string) error {
	f := newFlags("human-request list")
	repo, kind, state, node := repoFlag(f), f.String("kind", "", ""), f.String("state", "", ""), f.String("node", "", "")
	if err := f.parse(args); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/human-requests", query: query("kind", *kind, "state", *state, "node_id", *node, "limit", "200")})
	if err != nil {
		return err
	}
	return a.show(res, func(v map[string]any) {
		for _, hr := range items(v) {
			fmt.Fprintf(a.Stdout, "%s\t%s\t%s\t%s\n", str(hr, "id"), str(hr, "kind"), str(hr, "state"), str(hr, "node_id"))
		}
	})
}

func (a *App) hrView(ctx context.Context, args []string) error {
	f := newFlags("human-request view")
	repo := repoFlag(f)
	if err := f.parse(args); err != nil {
		return err
	}
	if err := f.need(1, "<人への依頼>"); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	res, err := a.call(ctx, request{method: http.MethodGet, path: repoPath(name) + "/human-requests/" + url.PathEscape(f.positional[0])})
	if err != nil {
		return err
	}
	return a.show(res, a.printHR)
}
