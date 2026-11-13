// Package tree は、木のファイル配置（design/files/layout.md）の読み書きと、木の計算と検査を行う。
// git には触れない。ファイルの一覧を受け取って、木を組み立てる。
// worker、CLI の check、バックエンドが、同じコードを使う。
package tree

import (
	"path"
	"sort"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/ids"
)

// File は、git の 1 つのファイルである。
type File struct {
	Path string
	// Mode は git のモード（例："100644"）。
	Mode string
	Size int64
	// Content は中身。大きすぎて読まなかったときは nil。
	Content []byte
}

// Files は、パスからファイルを引く。
type Files map[string]File

// Hypothesis は仮説のノードである。
type Hypothesis struct {
	ID         string
	Dir        string
	Depth      int // 根が 1
	Parent     *Story
	Statement  string
	Background string
	Verdicts   []*Verdict
	Stories    []*Story
}

// Story はストーリーのノードである。
type Story struct {
	ID          string
	Dir         string
	Parent      *Hypothesis
	Title       string
	Summary     string
	Inactivated bool
	Reason      string
	Hypotheses  []*Hypothesis
}

// Verdict は検証結果である。
type Verdict struct {
	VerdictContent
	Path       string
	Hypothesis *Hypothesis
}

// Tree は、ファイルから組み立てた木である。
type Tree struct {
	// Roots は tree/ の直下の仮説。正しい木なら 1 つだけ。
	Roots      []*Hypothesis
	Hypotheses map[string]*Hypothesis
	Stories    map[string]*Story
	Verdicts   map[string]*Verdict
	// Artifacts は、artifacts/sha256/ のファイルを sha256 で引く。
	Artifacts map[string]File
	// Problems は、ファイルの形の誤りである。
	Problems []Violation
}

// Root は、根の仮説を返す。根が 1 つでなければ nil。
func (t *Tree) Root() *Hypothesis {
	if len(t.Roots) != 1 {
		return nil
	}
	return t.Roots[0]
}

type dirEntry struct {
	files map[string]File
	dirs  map[string]*dirEntry
}

func newDir() *dirEntry { return &dirEntry{files: map[string]File{}, dirs: map[string]*dirEntry{}} }

func sortedKeys[T any](m map[string]T) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Build は、ファイルから木を組み立てる。形の誤りは、例外にせず Problems に集める。
func Build(files Files) *Tree {
	t := &Tree{
		Hypotheses: map[string]*Hypothesis{},
		Stories:    map[string]*Story{},
		Verdicts:   map[string]*Verdict{},
		Artifacts:  map[string]File{},
	}
	top := newDir()
	for _, p := range sortedKeys(files) {
		f := files[p]
		if f.Mode != RegularMode {
			t.problem(RuleNotRegularFile, p, "普通のファイル（100644）でない: "+f.Mode)
			continue
		}
		parts := strings.Split(p, "/")
		cur := top
		for _, d := range parts[:len(parts)-1] {
			next, ok := cur.dirs[d]
			if !ok {
				next = newDir()
				cur.dirs[d] = next
			}
			cur = next
		}
		cur.files[parts[len(parts)-1]] = f
	}
	for name := range top.files {
		t.problem(RuleOutsideTree, name, "木の外のファイル")
	}
	for _, name := range sortedKeys(top.dirs) {
		switch name {
		case TreeDir:
		case "artifacts":
			t.readArtifacts(top.dirs[name])
		default:
			t.problem(RuleOutsideTree, name+"/", "木の外のディレクトリ")
		}
	}
	treeDir, ok := top.dirs[TreeDir]
	if !ok {
		return t
	}
	for name := range treeDir.files {
		t.problem(RuleOutsideTree, TreeDir+"/"+name, "決まっていないファイル")
	}
	for _, name := range sortedKeys(treeDir.dirs) {
		dir := TreeDir + "/" + name
		if ids.KindOf(name) != ids.Hypothesis {
			t.problem(RuleAlternation, dir, "根が仮説のディレクトリでない")
			continue
		}
		t.Roots = append(t.Roots, t.readHypothesis(name, treeDir.dirs[name], dir, nil))
	}
	return t
}

func (t *Tree) problem(rule Rule, node, detail string) {
	t.Problems = append(t.Problems, Violation{Rule: rule, Node: node, Detail: detail})
}

func (t *Tree) tooLarge(p string, f File) bool {
	if f.Content == nil || f.Size > MaxArtifactSize {
		t.problem(RuleTooLarge, p, "ファイルが大きすぎる")
		return true
	}
	return false
}

func (t *Tree) readArtifacts(dir *dirEntry) {
	for name := range dir.files {
		t.problem(RuleOutsideTree, "artifacts/"+name, "決まっていないファイル")
	}
	for _, sub := range sortedKeys(dir.dirs) {
		if sub != "sha256" {
			t.problem(RuleOutsideTree, "artifacts/"+sub+"/", "決まっていないディレクトリ")
			continue
		}
		shaDir := dir.dirs[sub]
		for name := range shaDir.files {
			t.problem(RuleArtifactName, ArtifactsDir+"/"+name, "artifact のファイル名が正しくない")
		}
		for _, prefix := range sortedKeys(shaDir.dirs) {
			pd := shaDir.dirs[prefix]
			for d := range pd.dirs {
				t.problem(RuleArtifactName, ArtifactsDir+"/"+prefix+"/"+d+"/", "artifact の置き場に、ディレクトリがある")
			}
			for _, rest := range sortedKeys(pd.files) {
				p := ArtifactsDir + "/" + prefix + "/" + rest
				f := pd.files[rest]
				sha := prefix + rest
				if !sha256Pattern.MatchString(sha) || len(prefix) != 2 {
					t.problem(RuleArtifactName, p, "artifact のファイル名が正しくない")
					continue
				}
				if f.Size > MaxArtifactSize {
					t.problem(RuleTooLarge, p, "artifact が大きすぎる")
					continue
				}
				if f.Content == nil || SHA256Hex(f.Content) != sha {
					t.problem(RuleArtifactName, p, "ファイル名が中身のハッシュと合わない")
					continue
				}
				t.Artifacts[sha] = f
			}
		}
	}
}

func (t *Tree) readHypothesis(name string, d *dirEntry, dir string, parent *Story) *Hypothesis {
	h := &Hypothesis{ID: name, Dir: dir, Parent: parent, Depth: 1}
	if parent != nil {
		h.Depth = parent.Parent.Depth + 1
	}
	if _, dup := t.Hypotheses[name]; dup {
		t.problem(RuleDuplicateID, name, "識別子が重なっている")
	}
	t.Hypotheses[name] = h
	if f, ok := d.files[HypothesisFile]; !ok {
		t.problem(RuleParse, dir+"/"+HypothesisFile, "hypothesis.md がない")
	} else if !t.tooLarge(dir+"/"+HypothesisFile, f) {
		c, err := ParseHypothesis(f.Content)
		switch {
		case err != nil:
			t.problem(RuleParse, dir+"/"+HypothesisFile, err.Error())
		case c.ID != name:
			t.problem(RuleParse, dir+"/"+HypothesisFile, "id がディレクトリ名と違う")
		default:
			h.Statement = c.Statement
		}
	}
	if f, ok := d.files[BackgroundFile]; ok && !t.tooLarge(dir+"/"+BackgroundFile, f) {
		h.Background = string(f.Content)
	}
	for _, fn := range sortedKeys(d.files) {
		if fn != HypothesisFile && fn != BackgroundFile {
			t.problem(RuleOutsideTree, dir+"/"+fn, "決まっていないファイル")
		}
	}
	for _, sub := range sortedKeys(d.dirs) {
		subDir := dir + "/" + sub
		switch {
		case sub == VerdictsDir:
			t.readVerdicts(h, d.dirs[sub], subDir)
		case ids.KindOf(sub) == ids.Story:
			h.Stories = append(h.Stories, t.readStory(sub, d.dirs[sub], subDir, h))
		default:
			t.problem(RuleAlternation, subDir, "仮説の子に、ストーリーでないディレクトリがある")
		}
	}
	return h
}

func (t *Tree) readVerdicts(h *Hypothesis, d *dirEntry, dir string) {
	for sub := range d.dirs {
		t.problem(RuleOutsideTree, dir+"/"+sub+"/", "決まっていないディレクトリ")
	}
	for _, fn := range sortedKeys(d.files) {
		p := dir + "/" + fn
		id := strings.TrimSuffix(fn, ".md")
		if path.Ext(fn) != ".md" || ids.KindOf(id) != ids.Verdict {
			t.problem(RuleOutsideTree, p, "決まっていないファイル")
			continue
		}
		f := d.files[fn]
		if t.tooLarge(p, f) {
			continue
		}
		c, err := ParseVerdict(f.Content)
		if err != nil {
			t.problem(RuleParse, p, err.Error())
			continue
		}
		if c.ID != id {
			t.problem(RuleParse, p, "id がファイル名と違う")
			continue
		}
		if _, dup := t.Verdicts[id]; dup {
			t.problem(RuleDuplicateID, id, "識別子が重なっている")
		}
		v := &Verdict{VerdictContent: c, Path: p, Hypothesis: h}
		h.Verdicts = append(h.Verdicts, v)
		t.Verdicts[id] = v
	}
}

func (t *Tree) readStory(name string, d *dirEntry, dir string, parent *Hypothesis) *Story {
	s := &Story{ID: name, Dir: dir, Parent: parent}
	if _, dup := t.Stories[name]; dup {
		t.problem(RuleDuplicateID, name, "識別子が重なっている")
	}
	t.Stories[name] = s
	if f, ok := d.files[StoryFile]; !ok {
		t.problem(RuleParse, dir+"/"+StoryFile, "story.md がない")
	} else if !t.tooLarge(dir+"/"+StoryFile, f) {
		c, err := ParseStory(f.Content)
		switch {
		case err != nil:
			t.problem(RuleParse, dir+"/"+StoryFile, err.Error())
		case c.ID != name:
			t.problem(RuleParse, dir+"/"+StoryFile, "id がディレクトリ名と違う")
		default:
			s.Title, s.Summary = c.Title, c.Summary
		}
	}
	if f, ok := d.files[InactivatedFile]; ok && !t.tooLarge(dir+"/"+InactivatedFile, f) {
		s.Inactivated = true
		s.Reason = string(f.Content)
	}
	for _, fn := range sortedKeys(d.files) {
		if fn != StoryFile && fn != InactivatedFile {
			t.problem(RuleOutsideTree, dir+"/"+fn, "決まっていないファイル")
		}
	}
	for _, sub := range sortedKeys(d.dirs) {
		subDir := dir + "/" + sub
		if ids.KindOf(sub) != ids.Hypothesis {
			t.problem(RuleAlternation, subDir, "ストーリーの子に、仮説でないディレクトリがある")
			continue
		}
		s.Hypotheses = append(s.Hypotheses, t.readHypothesis(sub, d.dirs[sub], subDir, s))
	}
	return s
}

// ValidVerdicts は、仮説の有効な検証結果を返す。ほかの検証結果の supersedes に書かれていないもの。
func (h *Hypothesis) ValidVerdicts() []*Verdict {
	superseded := map[string]bool{}
	for _, v := range h.Verdicts {
		if v.Supersedes != "" {
			superseded[v.Supersedes] = true
		}
	}
	var out []*Verdict
	for _, v := range h.Verdicts {
		if !superseded[v.ID] {
			out = append(out, v)
		}
	}
	return out
}

// ActiveStories は、inactive でない子ストーリーを返す。
func (h *Hypothesis) ActiveStories() []*Story {
	var out []*Story
	for _, s := range h.Stories {
		if !s.Inactivated {
			out = append(out, s)
		}
	}
	return out
}

// HypothesisInactive は、仮説が inactive か（祖先のストーリーに inactivated.md があるか）を返す。
func HypothesisInactive(h *Hypothesis) bool {
	for s := h.Parent; s != nil; s = s.Parent.Parent {
		if s.Inactivated {
			return true
		}
	}
	return false
}

// StoryInactive は、ストーリーが inactive か（自分か祖先のストーリーに inactivated.md があるか）を返す。
func StoryInactive(s *Story) bool {
	return s.Inactivated || HypothesisInactive(s.Parent)
}
