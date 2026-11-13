// Package snapshot は、スナップショットの形と、その作り方と読み方である（design/backend/snapshot.md）。
// スナップショットの形の正本は、このパッケージの Go の型である。
// worker が git から読んだファイルで作り、バックエンドは読んで tree.Files に戻す。git には触れない。
package snapshot

import (
	"encoding/base64"
	"time"
	"unicode/utf8"

	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// FormatVersion は、スナップショットの形の版である。形を変えるときは、置き場の接頭辞ごと新しくする。
const FormatVersion = 1

// Kind は、コミットの種類である。
type Kind string

const (
	KindMain   Kind = "main"
	KindBranch Kind = "branch"
)

// Trailer は、コミットのメッセージの trailer である。
type Trailer struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// CommitInfo は、スナップショットを作るために、git から読んだコミットの情報である。
type CommitInfo struct {
	Revision    string
	CommittedAt time.Time
	Trailers    []Trailer
}

// Commit は、コミットの記録である（数 KB）。
type Commit struct {
	Revision string `json:"revision"`
	// Parent は、main では first-parent のコミット（最初のリビジョンでは nil）、branch では base。
	Parent *string `json:"parent"`
	// Number は、main のコミットだけ。最初のリビジョンを 1 とする。
	Number      int       `json:"number,omitempty"`
	Kind        Kind      `json:"kind"`
	CommittedAt time.Time `json:"committed_at"`
	Trailers    []Trailer `json:"trailers"`
	// Warnings は、main のコミットだけ。反映の記録がない、不変条件が破れている、など。
	Warnings []tree.Violation `json:"warnings,omitempty"`
	// Changes は、main のコミットだけ。親から直接変わったファイルの、ノードと種類の組。
	Changes []NodeChange `json:"changes,omitempty"`
	// Paths は、branch のコミットだけ。base から変わったファイルのパスと、変わり方。
	Paths []PathChange `json:"paths,omitempty"`
}

// FileKind は、ノードのファイルの種類である。
type FileKind string

const (
	FileHypothesis  FileKind = "hypothesis"
	FileBackground  FileKind = "background"
	FileVerdict     FileKind = "verdict"
	FileStory       FileKind = "story"
	FileInactivated FileKind = "inactivated"
)

// NodeChange は、ノードのファイルの変化である。
type NodeChange struct {
	Node string   `json:"node"`
	File FileKind `json:"file"`
}

// Status は、ファイルの変わり方である。
type Status string

const (
	Added    Status = "added"
	Modified Status = "modified"
	Deleted  Status = "deleted"
)

// PathChange は、パスの変化である。
type PathChange struct {
	Path   string `json:"path"`
	Status Status `json:"status"`
}

// File は、スナップショットに入れる、git の 1 つのファイルである。
type File struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
	// SHA は git の blob の SHA。
	SHA  string `json:"sha"`
	Size int64  `json:"size"`
	// Content は中身。UTF-8 でなければ base64 にし、Base64 を true にする。読まなかったときは nil。
	Content *string `json:"content,omitempty"`
	Base64  bool    `json:"base64,omitempty"`
	// SHA256 は、artifact の中身の sha256。artifact の中身はスナップショットに入れず、置き場に別に置く。
	SHA256 string `json:"sha256,omitempty"`
}

// Main は、main のコミットのスナップショットである。
type Main struct {
	Commit Commit `json:"commit"`
	Tree   Tree   `json:"tree"`
}

// Tree は、main のコミットの木である。すべてのノードを持つ。
type Tree struct {
	// Nodes は、ノードを識別子で引く。
	Nodes map[string]*Node `json:"nodes"`
	// Artifacts は、artifacts/sha256/ のファイルである。中身は持たない。
	Artifacts []File `json:"artifacts"`
	// Other は、どのノードにも、artifact にも当たらないファイルである（木の外のファイルなど）。
	Other []File `json:"other,omitempty"`
	// History は、最初のリビジョンからこのコミットまでの、main のコミットの一覧である（番号の順）。
	History []HistoryEntry `json:"history"`
}

// NodeKind は、ノードの種類である。
type NodeKind string

const (
	NodeHypothesis NodeKind = "hypothesis"
	NodeStory      NodeKind = "story"
)

// Node は、ノードのファイルと、計算した値である。
type Node struct {
	ID     string   `json:"id"`
	Kind   NodeKind `json:"kind"`
	Dir    string   `json:"dir"`
	Parent string   `json:"parent,omitempty"`
	// Files は、ノードのファイル（仮説では hypothesis.md、background.md、verdicts/、ストーリーでは story.md、inactivated.md）。
	Files []File `json:"files"`
	// Value は、計算した値。null は未決。
	Value *bool `json:"value"`
	// Leaf と NeedsReconsideration は、仮説だけ。
	Leaf                 bool `json:"leaf,omitempty"`
	NeedsReconsideration bool `json:"needs_reconsideration,omitempty"`
	Inactive             bool `json:"inactive,omitempty"`
	Evidence             bool `json:"evidence,omitempty"`
	// Changes は、そのノードを変えた main のコミットの一覧（ノードの変更の一覧）。番号の順。
	Changes []Rev `json:"changes"`
}

// Rev は、main のコミットの番号と SHA である。
type Rev struct {
	Number   int    `json:"number"`
	Revision string `json:"revision"`
}

// HistoryEntry は、main のコミットの一覧の 1 項目である。
type HistoryEntry struct {
	Number       int    `json:"number"`
	Revision     string `json:"revision"`
	ApplyRequest string `json:"apply_request,omitempty"`
	Warned       bool   `json:"warned,omitempty"`
}

// Branch は、利用者が push したブランチの先頭のコミットのスナップショットである。base からの差分だけを持つ。
type Branch struct {
	Commit Commit `json:"commit"`
	Diff   Diff   `json:"diff"`
}

// Diff は、base から先頭への、ファイルの差分である。
type Diff struct {
	Base    string      `json:"base"`
	Entries []DiffEntry `json:"entries"`
}

// DiffEntry は、1 つのパスの変化である。
type DiffEntry struct {
	Path   string `json:"path"`
	Status Status `json:"status"`
	// BaseSHA は、base での blob の SHA。加わったときは空。
	BaseSHA string `json:"base_sha,omitempty"`
	// File は、先頭でのファイル。消えたときは nil。
	File *File `json:"file,omitempty"`
}

// fromTreeFile は、tree.File をスナップショットのファイルにする。artifact は中身を持たず、sha256 を持つ。
func fromTreeFile(f tree.File, artifact bool) File {
	out := File{Path: f.Path, Mode: f.Mode, SHA: f.SHA, Size: f.Size, SHA256: f.SHA256}
	if artifact {
		if f.Content != nil {
			out.SHA256 = tree.SHA256Hex(f.Content)
		}
		return out
	}
	if f.Content != nil {
		if utf8.Valid(f.Content) {
			s := string(f.Content)
			out.Content = &s
		} else {
			s := base64.StdEncoding.EncodeToString(f.Content)
			out.Content, out.Base64 = &s, true
		}
	}
	return out
}

// toTreeFile は、スナップショットのファイルを tree.File に戻す。
func (f File) toTreeFile() (tree.File, error) {
	out := tree.File{Path: f.Path, Mode: f.Mode, SHA: f.SHA, Size: f.Size, SHA256: f.SHA256}
	if f.Content != nil {
		if f.Base64 {
			b, err := base64.StdEncoding.DecodeString(*f.Content)
			if err != nil {
				return out, err
			}
			out.Content = b
		} else {
			out.Content = []byte(*f.Content)
		}
	}
	return out, nil
}
