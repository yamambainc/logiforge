package cli

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yamambainc/logiforge/internal/tree"
)

// 直すための apply request のブランチを作るコマンド（design/backend/broken.md の「直すための apply request」）。
// どちらも、新しいブランチに、セッションの trailer を付けたコミットを作るだけである。push と ar create は、利用者が行う。

// repairRevert は、木の全体を、最後に正しかった時点のファイル（getRepairFiles）にしたコミットを作る。
// 戻す木は、git でなく API から読む。書き換えのあと、そのコミットが GitForge にないことがあるから。
func (a *App) repairRevert(ctx context.Context, args []string) error {
	return a.repair(ctx, args, "repair.revert")
}

// repairAccept は、今の main の先頭から分けたブランチに、空のコミットを作る。
func (a *App) repairAccept(ctx context.Context, args []string) error {
	return a.repair(ctx, args, "repair.accept")
}

func (a *App) repair(ctx context.Context, args []string, kind string) error {
	f := newFlags(strings.ReplaceAll(kind, ".", " "))
	repo, branch := repoFlag(f), f.String("branch", "", "作るブランチの名前")
	if err := f.parse(args); err != nil {
		return err
	}
	name, err := a.repo(*repo)
	if err != nil {
		return err
	}
	var r struct {
		Broken *struct {
			FirstCommit    string `json:"first_commit"`
			LastGoodCommit string `json:"last_good_commit"`
		} `json:"broken"`
	}
	if err := a.getJSON(ctx, repoPath(name), nil, &r); err != nil {
		return err
	}
	// このコマンドは、壊れた repository で使うものなので、壊れていることを誤りにしない。
	a.broken = ""
	if r.Broken == nil {
		return fmt.Errorf("repository %s は壊れていない", name)
	}
	b := *branch
	if b == "" {
		b = fmt.Sprintf("logiforge-%s-%s", strings.ReplaceAll(kind, ".", "-"), time.Now().UTC().Format("20060102150405"))
	}
	if err := a.gitPassthrough("fetch", "--quiet", "origin", "main"); err != nil {
		return err
	}
	if _, err := a.git("switch", "--quiet", "-c", b, "origin/main"); err != nil {
		return err
	}
	msg := fmt.Sprintf("最後に正しかった時点 %s に戻す", r.Broken.LastGoodCommit)
	if kind == "repair.accept" {
		msg = fmt.Sprintf("%s からの、反映によらない変更を認める", r.Broken.FirstCommit)
		// 認めた記録を、artifact として 1 つ加える。空のコミットの pull request は、Forgejo で squash merge できないから。
		mainHead, err := a.git("rev-parse", "origin/main")
		if err != nil {
			return err
		}
		record := fmt.Sprintf("Logiforge：反映によらない変更を認めた\nrepository: %s\nfirst: %s\nlast_good: %s\nmain: %s\n",
			name, r.Broken.FirstCommit, r.Broken.LastGoodCommit, mainHead)
		if err := a.addArtifact([]byte(record)); err != nil {
			return err
		}
	} else if err := a.restoreFiles(ctx, name); err != nil {
		return err
	}
	commitArgs := []string{"commit", "--quiet", "-m", msg}
	a.broken = ""
	if _, err := a.git(commitArgs...); err != nil {
		return err
	}
	head, err := a.git("rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if a.json {
		return json.NewEncoder(a.Stdout).Encode(map[string]string{"branch": b, "commit": head, "kind": kind})
	}
	fmt.Fprintf(a.Stdout, "ブランチ %s にコミット %s を作った。次に：\n  git push -u origin %s\n  logiforge ar create --branch %s --kind %s\n", b, head, b, b, kind)
	return nil
}

// restoreFiles は、作業ツリーの追跡しているファイルをすべて消し、最後に正しかった時点のファイルを置いて、git の index に入れる。
func (a *App) restoreFiles(ctx context.Context, name string) error {
	var rf struct {
		Commit string `json:"commit"`
		Files  []struct {
			Path          string `json:"path"`
			Mode          string `json:"mode"`
			ContentBase64 string `json:"content_base64"`
		} `json:"files"`
	}
	if err := a.getJSON(ctx, repoPath(name)+"/repair-files", nil, &rf); err != nil {
		return err
	}
	root, scope, err := a.repoRoot()
	if err != nil {
		return err
	}
	if _, err := a.git("-C", a.gitTop(root, scope), "rm", "-r", "--quiet", "--ignore-unmatch", "--", scope); err != nil {
		return err
	}
	for _, f := range rf.Files {
		data, err := base64.StdEncoding.DecodeString(f.ContentBase64)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Path, err)
		}
		p := filepath.Join(root, filepath.FromSlash(f.Path))
		if !strings.HasPrefix(p, root+string(filepath.Separator)) {
			return fmt.Errorf("ファイルのパスが、作業ツリーの外を指す：%s", f.Path)
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return err
		}
	}
	_, err = a.git("-C", a.gitTop(root, scope), "add", "-A", "--", scope)
	return err
}

// repoRoot は、repository の根の絶対パスと、git の作業ツリーの根から見たそのパス（git に渡す pathspec）を返す。
// ディレクトリの repository では、根はそのディレクトリである（design/directory/README.md）。
func (a *App) repoRoot() (root, scope string, err error) {
	top, err := a.git("rev-parse", "--show-toplevel")
	if err != nil {
		return "", "", err
	}
	if dir := a.directory(); dir != "" {
		return filepath.Join(top, filepath.FromSlash(dir)), dir, nil
	}
	return top, ".", nil
}

// gitTop は、repoRoot の root と scope から、git の作業ツリーの根を返す。
func (a *App) gitTop(root, scope string) string {
	if scope == "." {
		return root
	}
	return strings.TrimSuffix(root, string(filepath.Separator)+filepath.FromSlash(scope))
}

// addArtifact は、中身を artifact のファイル（artifacts/sha256/…）として置き、git の index に入れる。
func (a *App) addArtifact(content []byte) error {
	root, scope, err := a.repoRoot()
	if err != nil {
		return err
	}
	sum := sha256.Sum256(content)
	rel := tree.ArtifactPath(hex.EncodeToString(sum[:]))
	p := filepath.Join(root, filepath.FromSlash(rel))
	if scope != "." {
		rel = scope + "/" + rel
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p, content, 0o644); err != nil {
		return err
	}
	_, err = a.git("-C", a.gitTop(root, scope), "add", "--", rel)
	return err
}
