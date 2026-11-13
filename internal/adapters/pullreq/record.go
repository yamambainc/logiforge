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
	KeyRecord         = "Logiforge-Record"
	KeyMac            = "Logiforge-Record-Mac"
	KeyApplyRequest   = "Logiforge-Apply-Request"
	KeyFirstRevision  = "Logiforge-First-Revision"
	KeyPrincipal      = "Logiforge-Principal"
	KeySession        = "Logiforge-Session-Id"
	KeyHumanRequest   = "Logiforge-Human-Request"
	KeyHumanKind      = "Logiforge-Human-Request-Kind"
	KeyTargetNode     = "Logiforge-Target-Node"
	KeyAnsweredBy     = "Logiforge-Answered-By"
	KeyAttachment     = "Logiforge-Attachment"
	KeyMergeFailure   = "Logiforge-Merge-Failure"
	KeyReviewID       = "Logiforge-Review-Id"
	KeyReviewKind     = "Logiforge-Review-Kind"
	KeyReviewRevision = "Logiforge-Review-Revision"
	KeyPromptRevision = "Logiforge-Prompt-Revision"
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

// Format は、記録を、MAC を付けた本文にする。値の改行は、空白にする。
func (k Keyring) Format(r Record) (string, error) {
	key, ok := k.Keys[k.Current]
	if !ok || !keyIDForm.MatchString(k.Current) {
		return "", fmt.Errorf("pullreq: MAC の鍵 %q がない", k.Current)
	}
	for i, t := range r.Trailers {
		r.Trailers[i].Value = strings.Join(strings.Fields(t.Value), " ")
	}
	signed := canonical(r)
	m := hmac.New(sha256.New, key)
	m.Write([]byte(signed))
	return signed + "\n" + KeyMac + ": " + k.Current + ":" + hex.EncodeToString(m.Sum(nil)), nil
}

// Parse は、コメントの本文から記録を読み、MAC を確かめる。
// 記録の形でなければ ErrNoRecord、MAC が合わなければ ErrBadMac を返す。
func (k Keyring) Parse(text string) (Record, error) {
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
	id, mac, ok := strings.Cut(last[2], ":")
	key, known := k.Keys[id]
	if !ok || !known {
		return Record{}, ErrBadMac
	}
	want, err := hex.DecodeString(mac)
	if err != nil {
		return Record{}, ErrBadMac
	}
	h := hmac.New(sha256.New, key)
	h.Write([]byte(canonical(r)))
	if !hmac.Equal(h.Sum(nil), want) {
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
