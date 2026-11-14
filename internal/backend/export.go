package backend

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// ExportFormat は、書き出しの JSON の形の名前である。形を変えるときは、名前の末尾の番号（v1）を上げる。
const ExportFormat = "logiforge.export.v2"

// Export は、JSON で書き出した木である（design/api/export.md）。
type Export struct {
	Format           string          `json:"format"`
	Repository       string          `json:"repository"`
	Commit           string          `json:"commit"`
	RootHypothesisID string          `json:"root_hypothesis_id"`
	Nodes            []Node          `json:"nodes"`
	Verdicts         []ExportVerdict `json:"verdicts"`
}

// ExportVerdict は、書き出した検証結果である。差し替えられたものも含める。
type ExportVerdict struct {
	ID           string             `json:"id"`
	HypothesisID string             `json:"hypothesis_id"`
	Value        bool               `json:"value"`
	Valid        bool               `json:"valid"`
	Supersedes   []string           `json:"supersedes,omitempty"`
	ReportBody   string             `json:"report_body"`
	Artifacts    []tree.ArtifactRef `json:"artifacts"`
	Citations    []tree.Citation    `json:"citations"`
}

// ExportTree は、木を書き出す。ノードは、根から深さ優先の順に並べる。open の提案と人への依頼は含めない。
func (b *Backend) ExportTree(ctx context.Context, c Caller, repo, revision string) (Export, error) {
	if _, err := b.access(ctx, c, repo); err != nil {
		return Export{}, err
	}
	v, err := b.mainAt(ctx, repo, revision)
	if err != nil {
		return Export{}, err
	}
	root := v.tree.Root()
	if root == nil {
		return Export{}, fail(CodeNotFound, "根の仮説がない")
	}
	e := Export{Format: ExportFormat, Repository: repo, Commit: v.revision(), RootHypothesisID: root.ID,
		Nodes: v.subtree(root.ID, -1, claims{}), Verdicts: []ExportVerdict{}}
	for _, n := range e.Nodes {
		h, ok := n.(*Hypothesis)
		if !ok {
			continue
		}
		th := v.tree.Hypotheses[h.ID]
		valid := th.ValidVerdicts()
		for _, tv := range th.Verdicts {
			ev := ExportVerdict{ID: tv.ID, HypothesisID: th.ID, Value: tv.Value, Supersedes: tv.Supersedes, ReportBody: tv.Report,
				Artifacts: tv.Artifacts, Citations: tv.Citations, Valid: slices.Contains(valid, tv)}
			if ev.Artifacts == nil {
				ev.Artifacts = []tree.ArtifactRef{}
			}
			if ev.Citations == nil {
				ev.Citations = []tree.Citation{}
			}
			e.Verdicts = append(e.Verdicts, ev)
		}
	}
	return e, nil
}

// Markdown は、書き出した木を Markdown にする（design/api/export.md の「Markdown」）。
func (e Export) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n- コミット：%s\n- 根の仮説：%s\n", e.Repository, e.Commit, e.RootHypothesisID)
	verdicts := map[string][]ExportVerdict{}
	for _, v := range e.Verdicts {
		verdicts[v.HypothesisID] = append(verdicts[v.HypothesisID], v)
	}
	for _, n := range e.Nodes {
		switch x := n.(type) {
		case *Hypothesis:
			fmt.Fprintf(&b, "\n## 仮説 %s\n\n%s\n\n", x.ID, quote(x.Statement))
			parent := "なし（根）"
			if x.ParentStoryID != nil {
				parent = *x.ParentStoryID
			}
			fmt.Fprintf(&b, "- 親：%s\n- 値：%s\n", parent, valueText(x.Value))
			for _, s := range []struct {
				on   bool
				text string
			}{{x.Leaf, "葉"}, {x.NeedsReconsideration, "要再検討"}, {x.Inactive, "inactive"}} {
				if s.on {
					fmt.Fprintf(&b, "- %s\n", s.text)
				}
			}
			if x.Background != "" {
				fmt.Fprintf(&b, "\n### 背景情報\n\n%s\n", quote(x.Background))
			}
			for _, v := range verdicts[x.ID] {
				state := "差し替え済み"
				if v.Valid {
					state = "有効"
				}
				fmt.Fprintf(&b, "\n### 検証結果 %s（%s）\n\n- 値：%s\n", v.ID, state, valueText(&v.Value))
				if len(v.Supersedes) > 0 {
					fmt.Fprintf(&b, "- 差し替えたもの：%s\n", strings.Join(v.Supersedes, "、"))
				}
				for _, a := range v.Artifacts {
					where := a.SHA256
					if a.Kind == "link" {
						where = a.URI
					}
					fmt.Fprintf(&b, "- artifact：%s（%s：%s）\n", a.Name, a.Kind, where)
				}
				for _, c := range v.Citations {
					if c.TargetKind == "subtree" {
						fmt.Fprintf(&b, "- 引用：部分木 %s（%s）\n", c.NodeID, c.Commit)
					} else {
						fmt.Fprintf(&b, "- 引用：artifact %s\n", c.SHA256)
					}
				}
				fmt.Fprintf(&b, "\n%s\n", quote(v.ReportBody))
			}
		case *Story:
			fmt.Fprintf(&b, "\n## ストーリー %s：%s\n\n%s\n\n- 親：%s\n- 値：%s\n", x.ID, x.Title, quote(x.Summary), x.ParentHypothesisID, valueText(x.Value))
			if x.Reason != nil {
				fmt.Fprintf(&b, "- inactive にした理由：%s\n", strings.Join(strings.Fields(*x.Reason), " "))
			} else if x.Inactive {
				b.WriteString("- inactive（祖先のストーリーが inactive）\n")
			}
		}
	}
	return b.String()
}

// quote は、本文を引用の形にする。本文の見出しが、書き出しの見出しの構造を崩さないようにするため。
func quote(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		if l == "" {
			lines[i] = ">"
		} else {
			lines[i] = "> " + l
		}
	}
	return strings.Join(lines, "\n")
}

func valueText(v *bool) string {
	switch {
	case v == nil:
		return "未決"
	case *v:
		return "真"
	}
	return "偽"
}
