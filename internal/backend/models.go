package backend

import (
	"time"

	"github.com/yamambainc/hypothesis-verification-harness/internal/tree"
)

// API の応答の形。正本は design/api/openapi.yaml である。

// Repository は、repository である。
type Repository struct {
	Name             string `json:"name"`
	GitURL           string `json:"git_url"`
	RootHypothesisID string `json:"root_hypothesis_id"`
	RootValue        *bool  `json:"root_value"`
}

// Tree は、木の概要である。
type Tree struct {
	RootHypothesisID string   `json:"root_hypothesis_id"`
	RootValue        *bool    `json:"root_value"`
	Commit           string   `json:"commit"`
	Warnings         []string `json:"warnings"`
}

// Hypothesis は、仮説のノードである。
type Hypothesis struct {
	NodeType             string   `json:"node_type"`
	ID                   string   `json:"id"`
	ParentStoryID        *string  `json:"parent_story_id"`
	Statement            string   `json:"statement"`
	Background           string   `json:"background,omitempty"`
	Value                *bool    `json:"value"`
	ValidVerdictID       *string  `json:"valid_verdict_id"`
	Leaf                 bool     `json:"leaf"`
	NeedsReconsideration bool     `json:"needs_reconsideration"`
	Inactive             bool     `json:"inactive"`
	OpenApplyRequestIDs  []string `json:"open_apply_request_ids"`
	OpenHumanRequestIDs  []string `json:"open_human_request_ids"`
}

// Story は、ストーリーのノードである。
type Story struct {
	NodeType            string   `json:"node_type"`
	ID                  string   `json:"id"`
	ParentHypothesisID  string   `json:"parent_hypothesis_id"`
	Title               string   `json:"title"`
	Summary             string   `json:"summary"`
	State               string   `json:"state"`
	Reason              *string  `json:"reason"`
	Value               *bool    `json:"value"`
	Inactive            bool     `json:"inactive"`
	OpenApplyRequestIDs []string `json:"open_apply_request_ids"`
	OpenHumanRequestIDs []string `json:"open_human_request_ids"`
}

// Node は、*Hypothesis か *Story である。
type Node any

// NodeList は、ノードの一覧である。
type NodeList struct {
	Commit string `json:"commit"`
	Nodes  []Node `json:"nodes"`
}

// NodeDetail は、ノードの詳細である。
type NodeDetail struct {
	Commit   string   `json:"commit"`
	Node     Node     `json:"node"`
	Children []Node   `json:"children"`
	Verdict  *Verdict `json:"verdict"`
	CitedBy  []string `json:"cited_by"`
}

// Verdict は、検証結果である。
type Verdict struct {
	ID               string             `json:"id"`
	HypothesisID     string             `json:"hypothesis_id"`
	Value            bool               `json:"value"`
	ReportBody       string             `json:"report_body"`
	Artifacts        []tree.ArtifactRef `json:"artifacts"`
	Citations        []tree.Citation    `json:"citations"`
	Supersedes       []string           `json:"supersedes,omitempty"`
	ApplyRequestID   string             `json:"apply_request_id,omitempty"`
	SubmittedCommit  string             `json:"submitted_commit"`
	SupersededCommit *string            `json:"superseded_commit"`
}

// NodeHistoryEntry は、ノードを変えた main のコミットである。
type NodeHistoryEntry struct {
	Commit         string    `json:"commit"`
	CommittedAt    time.Time `json:"committed_at"`
	ApplyRequestID *string   `json:"apply_request_id"`
	SessionIDs     []string  `json:"session_ids"`
	Reflected      bool      `json:"reflected"`
}

// Page は、カーソルで分けた一覧のページである。
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// Operation は、差分から復元した操作である。
type Operation struct {
	Kind     string `json:"kind"`
	Position int    `json:"position"`
	// story.create と story.inactivate
	StoryID            string          `json:"story_id,omitempty"`
	ParentHypothesisID string          `json:"parent_hypothesis_id,omitempty"`
	Title              string          `json:"title,omitempty"`
	Summary            string          `json:"summary,omitempty"`
	Hypotheses         []NewHypothesis `json:"hypotheses,omitempty"`
	Reason             string          `json:"reason,omitempty"`
	// verdict.submit
	VerdictID  string             `json:"verdict_id,omitempty"`
	Value      *bool              `json:"value,omitempty"`
	ReportBody string             `json:"report_body,omitempty"`
	Supersedes []string           `json:"supersedes,omitempty"`
	Artifacts  []tree.ArtifactRef `json:"artifacts,omitempty"`
	Citations  []tree.Citation    `json:"citations,omitempty"`
	// verdict.submit と node.edit の対象の仮説
	HypothesisID     string `json:"hypothesis_id,omitempty"`
	AppendBackground string `json:"append_background,omitempty"`
}

// NewHypothesis は、story.create で作る仮説である。
type NewHypothesis struct {
	ID         string `json:"id"`
	Statement  string `json:"statement"`
	Background string `json:"background,omitempty"`
}

// HeadCommit は、apply request のブランチに push された先頭のコミットである。
type HeadCommit struct {
	Commit string `json:"commit"`
	// BaseCommit は、そのコミットと main の分かれた点。worker がまだ取り込んでいなければ空。
	BaseCommit string `json:"base_commit"`
	// SeenAt は、worker がそのコミットを先頭として見つけた時刻（push のおおよその時刻）。worker がまだ取り込んでいなければ nil。
	SeenAt          *time.Time `json:"seen_at"`
	AuthorSessionID *string    `json:"author_session_id"`
}

// Review は、レビューである。
type Review struct {
	ID                string `json:"id"`
	Kind              string `json:"kind"`
	Commit            string `json:"commit"`
	Body              string `json:"body,omitempty"`
	ReviewerSessionID string `json:"reviewer_session_id"`
	PromptRevision    string `json:"prompt_revision,omitempty"`
	Valid             bool   `json:"valid"`
}

// MergeFailure は、反映の失敗である。
type MergeFailure struct {
	Commit string `json:"commit"`
	Reason string `json:"reason"`
	Detail string `json:"detail"`
}

// ApplyRequest は、apply request である。
type ApplyRequest struct {
	ID                string         `json:"id"`
	Branch            string         `json:"branch"`
	State             string         `json:"state"`
	HeadCommit        string         `json:"head_commit"`
	HeadCommits       []HeadCommit   `json:"head_commits"`
	Description       string         `json:"description,omitempty"`
	Operations        []Operation    `json:"operations"`
	Reviews           []Review       `json:"reviews"`
	MergeFailures     []MergeFailure `json:"merge_failures"`
	Awaiting          *string        `json:"awaiting"`
	ClosedReason      *string        `json:"closed_reason"`
	ClosedBySessionID *string        `json:"closed_by_session_id"`
	Warnings          []string       `json:"warnings"`
	MergedCommit      *string        `json:"merged_commit"`
}

// ReviewMaterial は、レビューの材料である。
type ReviewMaterial struct {
	ApplyRequestID string             `json:"apply_request_id"`
	Commit         string             `json:"commit"`
	Operations     []Operation        `json:"operations"`
	Targets        []Node             `json:"targets"`
	Ancestors      []*Hypothesis      `json:"ancestors"`
	Siblings       []*Story           `json:"siblings"`
	Citations      []MaterialCitation `json:"citations"`
}

// MaterialCitation は、レビューの材料の、引用している部分木か artifact である。
type MaterialCitation struct {
	Citation tree.Citation     `json:"citation"`
	Nodes    []Node            `json:"nodes,omitempty"`
	Artifact *tree.ArtifactRef `json:"artifact,omitempty"`
}

// WorkItem は、着手できる作業である。
type WorkItem struct {
	Kind      string `json:"kind"`
	SubjectID string `json:"subject_id"`
	NodeID    string `json:"node_id,omitempty"`
}

// WorkPage は、着手できる作業の一覧である。
type WorkPage struct {
	Commit     string         `json:"commit"`
	Counts     map[string]int `json:"counts"`
	Items      []WorkItem     `json:"items"`
	NextCursor *string        `json:"next_cursor"`
}

// Completion は、作業の終わりの判定である。
type Completion struct {
	Commit        string `json:"commit"`
	RootValue     *bool  `json:"root_value"`
	Decided       bool   `json:"decided"`
	RemainingWork int    `json:"remaining_work"`
	Done          bool   `json:"done"`
}

// HumanRequest は、人への依頼である。
type HumanRequest struct {
	ID     string  `json:"id"`
	NodeID string  `json:"node_id"`
	Kind   string  `json:"kind"`
	Body   string  `json:"body"`
	State  string  `json:"state"`
	Answer *Answer `json:"answer"`
}

// Answer は、人への依頼の回答である。
type Answer struct {
	Body        string             `json:"body"`
	Attachments []tree.ArtifactRef `json:"attachments"`
	AnsweredAt  time.Time          `json:"answered_at"`
}
