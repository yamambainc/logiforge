package tree

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"testing"
)

// fb は、テスト用にファイルの一覧を組み立てる。
type fb struct{ files Files }

func newFB() *fb { return &fb{files: Files{}} }

func (b *fb) clone() *fb {
	c := newFB()
	for k, v := range b.files {
		c.files[k] = v
	}
	return c
}

func (b *fb) put(path string, content []byte) *fb {
	b.files[path] = File{Path: path, Mode: RegularMode, SHA: blobSHA(content), Size: int64(len(content)), Content: content}
	return b
}

// blobSHA は、git の blob の SHA-1 を返す。
func blobSHA(content []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(content))
	h.Write(content)
	return hex.EncodeToString(h.Sum(nil))
}

func (b *fb) del(path string) *fb { delete(b.files, path); return b }

// hyp は、親のディレクトリの下に仮説を置き、そのディレクトリを返す。
func (b *fb) hyp(parentDir, id, statement string) string {
	dir := parentDir + "/" + id
	b.put(dir+"/"+HypothesisFile, MarshalHypothesis(HypothesisContent{ID: id, Statement: statement}))
	return dir
}

func (b *fb) story(parentDir, id, title string) string {
	dir := parentDir + "/" + id
	b.put(dir+"/"+StoryFile, MarshalStory(StoryContent{ID: id, Title: title, Summary: "概要"}))
	return dir
}

func (b *fb) verdict(hypDir, id string, value bool, supersedes string, arts ...ArtifactRef) {
	b.put(hypDir+"/"+VerdictsDir+"/"+id+".md", MarshalVerdict(VerdictContent{ID: id, Value: value, Supersedes: supersedes, Artifacts: arts, Report: "レポート\n"}))
}

func (b *fb) inactivate(storyDir, reason string) {
	b.put(storyDir+"/"+InactivatedFile, []byte(reason))
}

func (b *fb) artifact(content string) ArtifactRef {
	sha := SHA256Hex([]byte(content))
	b.put(ArtifactPath(sha), []byte(content))
	return ArtifactRef{Kind: "file", Name: "data.txt", SHA256: sha}
}

// 識別子（10 文字の Crockford base32）。
const (
	H0 = "h-0000000000"
	S1 = "s-1111111111"
	S2 = "s-2222222222"
	H1 = "h-3333333333"
	H2 = "h-4444444444"
	H3 = "h-5555555555"
	H4 = "h-6666666666"
	V1 = "v-7777777777"
	V2 = "v-8888888888"
	S3 = "s-9999999999"
	H5 = "h-aaaaaaaaaa"
)

// base は、根 H0 の下にストーリー S1（子仮説 H1、H2）がある木を返す。
func base() (*fb, map[string]string) {
	b := newFB()
	d := map[string]string{}
	d[H0] = b.hyp(TreeDir, H0, "根")
	d[S1] = b.story(d[H0], S1, "ストーリー1")
	d[H1] = b.hyp(d[S1], H1, "子1")
	d[H2] = b.hyp(d[S1], H2, "子2")
	return b, d
}

func rules(vs []Violation) []Rule {
	var out []Rule
	for _, v := range vs {
		out = append(out, v.Rule)
	}
	return out
}

func hasRule(vs []Violation, r Rule) bool {
	for _, v := range vs {
		if v.Rule == r {
			return true
		}
	}
	return false
}

func mustCheck(t *testing.T, base, head *fb, ctx Context) Result {
	t.Helper()
	res, err := CheckChange(base.files, head.files, ctx)
	if err != nil {
		t.Fatal(err)
	}
	return res
}
