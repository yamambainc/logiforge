package tree

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/yamambainc/logiforge/internal/ids"
)

// ファイルとディレクトリの名前（design/files/layout.md）。
const (
	TreeDir         = "tree"
	ArtifactsDir    = "artifacts/sha256"
	HypothesisFile  = "hypothesis.md"
	BackgroundFile  = "background.md"
	StoryFile       = "story.md"
	InactivatedFile = "inactivated.md"
	VerdictsDir     = "verdicts"
	RegularMode     = "100644"
	// MaxArtifactSize は artifact のファイルの大きさの上限（docs/tree/verdict.md）。
	MaxArtifactSize = 10 << 20
)

// ArtifactRef は、検証結果が参照する artifact である（design/files/artifacts.md）。
type ArtifactRef struct {
	Kind      string `yaml:"kind" json:"kind"`
	Name      string `yaml:"name" json:"name"`
	SHA256    string `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	URI       string `yaml:"uri,omitempty" json:"uri,omitempty"`
	MediaType string `yaml:"media_type,omitempty" json:"media_type,omitempty"`
}

// Citation は、検証結果の引用である。TargetKind は artifact か subtree。
type Citation struct {
	TargetKind string `yaml:"target_kind" json:"target_kind"`
	SHA256     string `yaml:"sha256,omitempty" json:"sha256,omitempty"`
	NodeID     string `yaml:"node_id,omitempty" json:"node_id,omitempty"`
	// Commit は、部分木を引用した main のコミットの SHA。前は `revision` という名前で書いていた。読むときは、どちらの名前も受け付ける。
	Commit string `yaml:"commit,omitempty" json:"commit,omitempty"`
}

// HypothesisContent は hypothesis.md の中身である。
type HypothesisContent struct {
	ID        string `json:"id"`
	Statement string `json:"statement"`
}

// StoryContent は story.md の中身である。
type StoryContent struct {
	ID      string `json:"id"`
	Title   string `json:"title"`
	Summary string `json:"summary"`
}

// VerdictContent は verdicts/v-*.md の中身である。
type VerdictContent struct {
	ID         string        `json:"id"`
	Value      bool          `json:"value"`
	Supersedes []string      `json:"supersedes,omitempty"`
	Artifacts  []ArtifactRef `json:"artifacts,omitempty"`
	Citations  []Citation    `json:"citations,omitempty"`
	Report     string        `json:"report"`
}

// commitPattern は、git のコミットの SHA の形である（SHA-1 の 40 文字か、SHA-256 の 64 文字）。
var commitPattern = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// SHA256Hex は、中身の sha256 を 16 進で返す。
func SHA256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// ArtifactPath は、sha256 から artifact のファイルのパスを求める。
func ArtifactPath(sha string) string {
	return ArtifactsDir + "/" + sha[:2] + "/" + sha[2:]
}

// MarshalHypothesis は hypothesis.md を書き出す。
func MarshalHypothesis(c HypothesisContent) []byte {
	return joinFrontMatter([]string{"id", "statement"}, map[string]any{"id": c.ID, "statement": c.Statement}, "")
}

// MarshalStory は story.md を書き出す。
func MarshalStory(c StoryContent) []byte {
	return joinFrontMatter([]string{"id", "title", "summary"}, map[string]any{"id": c.ID, "title": c.Title, "summary": c.Summary}, "")
}

// MarshalVerdict は検証結果のファイルを書き出す。最初の検証結果では supersedes を書かない。
func MarshalVerdict(c VerdictContent) []byte {
	data := map[string]any{"id": c.ID, "value": c.Value}
	if len(c.Supersedes) > 0 {
		data["supersedes"] = c.Supersedes
	}
	arts := c.Artifacts
	if arts == nil {
		arts = []ArtifactRef{}
	}
	cits := c.Citations
	if cits == nil {
		cits = []Citation{}
	}
	data["artifacts"] = arts
	data["citations"] = cits
	return joinFrontMatter([]string{"id", "value", "supersedes", "artifacts", "citations"}, data, c.Report)
}

func onlyKeys(data map[string]any, allowed ...string) error {
	var extra []string
	for _, k := range sortedKeys(data) {
		found := false
		for _, a := range allowed {
			if k == a {
				found = true
				break
			}
		}
		if !found {
			extra = append(extra, k)
		}
	}
	if len(extra) > 0 {
		sort.Strings(extra)
		return fmt.Errorf("決まっていない項目がある: %v", extra)
	}
	return nil
}

func str(data map[string]any, key string) (string, error) {
	v, ok := data[key].(string)
	if !ok {
		return "", fmt.Errorf("%s が文字列でない", key)
	}
	return v, nil
}

// ParseHypothesis は hypothesis.md を読む。
func ParseHypothesis(content []byte) (HypothesisContent, error) {
	data, body, err := splitFrontMatter(content)
	if err != nil {
		return HypothesisContent{}, err
	}
	if data == nil {
		return HypothesisContent{}, fmt.Errorf("front matter がない")
	}
	if err := onlyKeys(data, "id", "statement"); err != nil {
		return HypothesisContent{}, err
	}
	if body != "" {
		return HypothesisContent{}, fmt.Errorf("本文があってはならない")
	}
	id, err := str(data, "id")
	if err != nil {
		return HypothesisContent{}, err
	}
	st, err := str(data, "statement")
	if err != nil {
		return HypothesisContent{}, err
	}
	return HypothesisContent{ID: id, Statement: st}, nil
}

// ParseStory は story.md を読む。
func ParseStory(content []byte) (StoryContent, error) {
	data, body, err := splitFrontMatter(content)
	if err != nil {
		return StoryContent{}, err
	}
	if data == nil {
		return StoryContent{}, fmt.Errorf("front matter がない")
	}
	if err := onlyKeys(data, "id", "title", "summary"); err != nil {
		return StoryContent{}, err
	}
	if body != "" {
		return StoryContent{}, fmt.Errorf("本文があってはならない")
	}
	var c StoryContent
	if c.ID, err = str(data, "id"); err != nil {
		return c, err
	}
	if c.Title, err = str(data, "title"); err != nil {
		return c, err
	}
	if c.Summary, err = str(data, "summary"); err != nil {
		return c, err
	}
	return c, nil
}

// ParseVerdict は検証結果のファイルを読む。
func ParseVerdict(content []byte) (VerdictContent, error) {
	data, body, err := splitFrontMatter(content)
	if err != nil {
		return VerdictContent{}, err
	}
	if data == nil {
		return VerdictContent{}, fmt.Errorf("front matter がない")
	}
	if err := onlyKeys(data, "id", "value", "supersedes", "artifacts", "citations"); err != nil {
		return VerdictContent{}, err
	}
	var c VerdictContent
	if c.ID, err = str(data, "id"); err != nil {
		return c, err
	}
	v, ok := data["value"].(bool)
	if !ok {
		return c, fmt.Errorf("value が真偽値でない")
	}
	c.Value = v
	if s, ok := data["supersedes"]; ok {
		list, ok := s.([]any)
		if !ok || len(list) == 0 {
			return c, fmt.Errorf("supersedes が、1 つ以上の識別子の一覧でない")
		}
		seen := map[string]bool{}
		for _, item := range list {
			id, ok := item.(string)
			if !ok || ids.KindOf(id) != ids.Verdict {
				return c, fmt.Errorf("supersedes に、検証結果の識別子でないものがある: %v", item)
			}
			if seen[id] {
				return c, fmt.Errorf("supersedes に、同じ識別子が重なっている: %s", id)
			}
			seen[id] = true
			c.Supersedes = append(c.Supersedes, id)
		}
	}
	if c.Artifacts, err = parseArtifacts(data["artifacts"]); err != nil {
		return c, err
	}
	if c.Citations, err = parseCitations(data["citations"]); err != nil {
		return c, err
	}
	c.Report = body
	if c.Report == "" {
		return c, fmt.Errorf("レポートの本文がない")
	}
	return c, nil
}

func parseArtifacts(v any) ([]ArtifactRef, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("artifacts が配列でない")
	}
	var out []ArtifactRef
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("artifact の形が正しくない")
		}
		if err := onlyKeys(m, "kind", "name", "sha256", "uri", "media_type"); err != nil {
			return nil, fmt.Errorf("artifact: %w", err)
		}
		var a ArtifactRef
		var err error
		if a.Kind, err = str(m, "kind"); err != nil {
			return nil, err
		}
		if a.Name, err = str(m, "name"); err != nil {
			return nil, err
		}
		if strings.TrimSpace(a.Name) == "" {
			return nil, fmt.Errorf("artifact の name が空")
		}
		for _, kv := range []struct {
			key string
			dst *string
		}{{"sha256", &a.SHA256}, {"uri", &a.URI}, {"media_type", &a.MediaType}} {
			key, dst := kv.key, kv.dst
			if raw, ok := m[key]; ok {
				s, ok := raw.(string)
				if !ok {
					return nil, fmt.Errorf("artifact の %s が文字列でない", key)
				}
				*dst = s
			}
		}
		switch a.Kind {
		case "file":
			if !ids.Sha256Pattern.MatchString(a.SHA256) {
				return nil, fmt.Errorf("file の artifact に正しい sha256 がない")
			}
		case "link":
			if a.URI == "" {
				return nil, fmt.Errorf("link の artifact に uri がない")
			}
			if a.SHA256 != "" && !ids.Sha256Pattern.MatchString(a.SHA256) {
				return nil, fmt.Errorf("link の artifact の sha256 が正しくない")
			}
		default:
			return nil, fmt.Errorf("artifact の kind が正しくない: %q", a.Kind)
		}
		out = append(out, a)
	}
	return out, nil
}

func parseCitations(v any) ([]Citation, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("citations が配列でない")
	}
	var out []Citation
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("引用の形が正しくない")
		}
		kind, _ := m["target_kind"].(string)
		switch kind {
		case "artifact":
			if err := onlyKeys(m, "target_kind", "sha256"); err != nil {
				return nil, fmt.Errorf("引用: %w", err)
			}
			sha, _ := m["sha256"].(string)
			if !ids.Sha256Pattern.MatchString(sha) {
				return nil, fmt.Errorf("artifact の引用の sha256 が正しくない")
			}
			out = append(out, Citation{TargetKind: kind, SHA256: sha})
		case "subtree":
			if err := onlyKeys(m, "target_kind", "node_id", "commit", "revision"); err != nil {
				return nil, fmt.Errorf("引用: %w", err)
			}
			node, _ := m["node_id"].(string)
			// 前の名前（revision）で書いたファイルも読む。両方は書かない。
			rev, _ := m["commit"].(string)
			if old, ok := m["revision"].(string); ok {
				if _, both := m["commit"]; both {
					return nil, fmt.Errorf("部分木の引用に、commit と revision の両方がある")
				}
				rev = old
			}
			if k := ids.KindOf(node); k != ids.Hypothesis && k != ids.Story {
				return nil, fmt.Errorf("部分木の引用の node_id が、仮説かストーリーの識別子でない: %q", node)
			}
			if !commitPattern.MatchString(rev) {
				return nil, fmt.Errorf("部分木の引用の commit が、コミットの SHA でない: %q", rev)
			}
			out = append(out, Citation{TargetKind: kind, NodeID: node, Commit: rev})
		default:
			return nil, fmt.Errorf("引用の target_kind が正しくない: %q", kind)
		}
	}
	return out, nil
}
