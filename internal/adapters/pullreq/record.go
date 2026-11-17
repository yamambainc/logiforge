package pullreq

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// 記録のコメントの trailer（design/adapters/pull-requests.md の「記録のコメント」）。
const (
	KeyRecord        = "Logiforge-Record"
	KeyMac           = "Logiforge-Record-Mac"
	KeyApplyRequest  = "Logiforge-Apply-Request"
	KeyFirstRevision = "Logiforge-First-Revision"
	// KeyApplyRequestKind は、直すための apply request の種類（repair.revert か repair.accept）。ふつうの apply request には書かない。
	KeyApplyRequestKind = "Logiforge-Apply-Request-Kind"
	KeyPrincipal        = "Logiforge-Principal"
	KeySession          = "Logiforge-Session-Id"
	KeyHumanRequest     = "Logiforge-Human-Request"
	KeyHumanKind        = "Logiforge-Human-Request-Kind"
	KeyTargetNode       = "Logiforge-Target-Node"
	KeyAnsweredBy       = "Logiforge-Answered-By"
	KeyAttachment       = "Logiforge-Attachment"
	KeyMergeFailure     = "Logiforge-Merge-Failure"
	KeyRevision         = "Logiforge-Revision"
	KeyReviewID         = "Logiforge-Review-Id"
	KeyReviewKind       = "Logiforge-Review-Kind"
	KeyPreviousReview   = "Logiforge-Previous-Review"
	KeyReviewRevision   = "Logiforge-Review-Revision"
	KeyPromptRevision   = "Logiforge-Prompt-Revision"
)

// 記録の種類。
const (
	RecordApplyRequest = "apply_request"
	RecordClose        = "close"
	RecordMergeFailure = "merge_failure"
	RecordHumanRequest = "human_request"
	RecordAnswer       = "answer"
	RecordReview       = "review"
)

// Trailer は、記録の 1 行である。
type Trailer struct{ Key, Value string }

// Record は、書き込み主体が書く記録である。本文と trailer からなる。
type Record struct {
	Body     string
	Trailers []Trailer
}

// Get は、key の最初の値を返す。
func (r Record) Get(key string) string {
	for _, t := range r.Trailers {
		if t.Key == key {
			return t.Value
		}
	}
	return ""
}

// All は、key のすべての値を返す。
func (r Record) All(key string) []string {
	var out []string
	for _, t := range r.Trailers {
		if t.Key == key {
			out = append(out, t.Value)
		}
	}
	return out
}

// Keyring は、MAC の鍵である。書くときは Current の鍵を使い、確かめるときは鍵の ID で選ぶ。
// 鍵を取り替えるときは、新しい鍵を Current にし、古い鍵も Keys に残す。
type Keyring struct {
	Current string
	Keys    map[string][]byte
}

var (
	keyLine   = regexp.MustCompile(`^(Logiforge-[A-Za-z-]+): (.*)$`)
	keyIDForm = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
)

// ErrNoRecord は、本文が記録の形でないことを表す。
var ErrNoRecord = errors.New("記録の形でない")

// ErrBadMac は、MAC が合わないことを表す（あとから編集された、など）。
var ErrBadMac = errors.New("記録の MAC が合わない")

// Scope は、記録を置いた場所である。MAC に含め、記録をほかの pull request や issue に写して使い回せないようにする。
type Scope struct {
	// RepoID は、GitForge の中で変わらない repository の識別子（Forgejo の repository の id、GitHub の node_id）。
	// 名前（owner/name）は使わない。名前を変えたり、別の organization に移したりしても、記録が壊れないようにするため。
	RepoID string
	// Number は、記録のコメントを置いた pull request か issue の番号。
	Number int
}

// Format は、記録を、MAC を付けた本文にする。本文の CRLF は LF にし、末尾の改行を落とす。値の改行は、空白にする。
func (k Keyring) Format(r Record, at Scope) (string, error) {
	key, ok := k.Keys[k.Current]
	if !ok || !keyIDForm.MatchString(k.Current) {
		return "", fmt.Errorf("pullreq: MAC の鍵 %q がない", k.Current)
	}
	r.Body = strings.TrimRight(strings.ReplaceAll(r.Body, "\r\n", "\n"), "\n")
	trailers := make([]Trailer, len(r.Trailers))
	for i, t := range r.Trailers {
		trailers[i] = Trailer{t.Key, strings.Join(strings.Fields(t.Value), " ")}
	}
	r.Trailers = trailers
	text := canonical(r)
	return text + "\n" + KeyMac + ": " + k.Current + ":" + mac(key, at, text), nil
}

// mac は、置いた場所と記録の本文に対する HMAC-SHA256 を返す。
func mac(key []byte, at Scope, text string) string {
	m := hmac.New(sha256.New, key)
	fmt.Fprintf(m, "%s#%d\n", at.RepoID, at.Number)
	m.Write([]byte(text))
	return hex.EncodeToString(m.Sum(nil))
}

// Parse は、コメントの本文から記録を読み、at に置かれたものとして MAC を確かめる。
// 記録の形でなければ ErrNoRecord、MAC が合わなければ（編集された、ほかの場所から写された、など）ErrBadMac を返す。
func (k Keyring) Parse(text string, at Scope) (Record, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.TrimRight(text, "\n")
	i := strings.LastIndex(text, "\n\n")
	var body, block string
	if i < 0 {
		block = text
	} else {
		body, block = text[:i], text[i+2:]
	}
	lines := strings.Split(block, "\n")
	if len(lines) < 2 {
		return Record{}, ErrNoRecord
	}
	var r Record
	r.Body = body
	for _, l := range lines[:len(lines)-1] {
		m := keyLine.FindStringSubmatch(l)
		if m == nil || m[1] == KeyMac {
			return Record{}, ErrNoRecord
		}
		r.Trailers = append(r.Trailers, Trailer{m[1], m[2]})
	}
	last := keyLine.FindStringSubmatch(lines[len(lines)-1])
	if last == nil || last[1] != KeyMac {
		return Record{}, ErrNoRecord
	}
	id, got, ok := strings.Cut(last[2], ":")
	key, known := k.Keys[id]
	if !ok || !known {
		return Record{}, ErrBadMac
	}
	if !hmac.Equal([]byte(mac(key, at, canonical(r))), []byte(got)) {
		return Record{}, ErrBadMac
	}
	return r, nil
}

func canonical(r Record) string {
	var b strings.Builder
	if r.Body != "" {
		b.WriteString(strings.TrimRight(r.Body, "\n"))
		b.WriteString("\n\n")
	}
	for i, t := range r.Trailers {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(t.Key + ": " + t.Value)
	}
	return b.String()
}
