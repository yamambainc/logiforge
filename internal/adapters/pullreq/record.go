package pullreq

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// 記録のコメントの trailer（design/adapters/pull-requests.md の「記録のコメント」）。
const (
	KeyRecord        = "Logiforge-Record"
	KeyMac           = "Logiforge-Record-Signature"
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

// Record は、バックエンドが書く記録である。本文と trailer からなる。
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

// Keyring は、記録の署名の鍵（Ed25519）である（design/adapters/pull-requests.md の「記録のコメント」）。
// 書くときは Current の秘密鍵を使い、確かめるときは鍵の ID で公開鍵を選ぶ。
// 鍵を取り替えるときは、新しい鍵を Current にし、古い鍵の公開鍵も Public に残す。
// worker は、公開鍵だけを持つ（Private は空）。記録を作れない。
type Keyring struct {
	Current string
	Private map[string]ed25519.PrivateKey
	Public  map[string]ed25519.PublicKey
}

// NewKeyring は、秘密鍵の種（32 バイト）から、署名と確かめに使う鍵を作る。最初の ID を Current にする。
func NewKeyring(ids []string, seeds [][]byte) (Keyring, error) {
	k := Keyring{Private: map[string]ed25519.PrivateKey{}, Public: map[string]ed25519.PublicKey{}}
	for i, id := range ids {
		if !keyIDForm.MatchString(id) || len(seeds[i]) != ed25519.SeedSize {
			return Keyring{}, fmt.Errorf("pullreq: 鍵 %q の形が正しくない（ID は英数字、種は %d バイト）", id, ed25519.SeedSize)
		}
		priv := ed25519.NewKeyFromSeed(seeds[i])
		k.Private[id], k.Public[id] = priv, priv.Public().(ed25519.PublicKey)
		if i == 0 {
			k.Current = id
		}
	}
	return k, nil
}

// KeyringFromSecrets は、秘密の文字列から鍵を作る。種は、文字列の SHA-256 である。
// なぜ：設定の秘密を、長さを決めずに、乱数の文字列として作れるようにするため（scripts/compose-setup.sh）。
func KeyringFromSecrets(ids, secrets []string) (Keyring, error) {
	seeds := make([][]byte, len(secrets))
	for i, s := range secrets {
		h := sha256.Sum256([]byte(s))
		seeds[i] = h[:]
	}
	return NewKeyring(ids, seeds)
}

// ParsePublicKeys は、「鍵の ID=公開鍵（base64）,…」から、確かめるだけの Keyring を作る（worker の設定）。
func ParsePublicKeys(v string) (Keyring, error) {
	k := Keyring{Public: map[string]ed25519.PublicKey{}}
	for _, part := range strings.Split(v, ",") {
		id, b64, ok := strings.Cut(strings.TrimSpace(part), "=")
		pub, err := base64.StdEncoding.DecodeString(b64)
		if !ok || !keyIDForm.MatchString(id) || err != nil || len(pub) != ed25519.PublicKeySize {
			return Keyring{}, fmt.Errorf("pullreq: 公開鍵の形が正しくない（鍵の ID=base64,…）：%q", part)
		}
		k.Public[id] = pub
	}
	return k, nil
}

// FormatPublicKeys は、公開鍵を「鍵の ID=公開鍵（base64）,…」にする。ParsePublicKeys の逆である。
func (k Keyring) FormatPublicKeys() string {
	var parts []string
	for id, pub := range k.Public {
		parts = append(parts, id+"="+base64.StdEncoding.EncodeToString(pub))
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}

// VerifyOnly は、公開鍵だけを残した Keyring を返す。worker に渡す。
func (k Keyring) VerifyOnly() Keyring { return Keyring{Public: k.Public} }

var (
	keyLine   = regexp.MustCompile(`^(Logiforge-[A-Za-z-]+): (.*)$`)
	keyIDForm = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
)

// ErrNoRecord は、本文が記録の形でないことを表す。
var ErrNoRecord = errors.New("記録の形でない")

// ErrBadMac は、署名が合わないことを表す（あとから編集された、など）。
var ErrBadMac = errors.New("記録の署名が合わない")

// Scope は、記録を置いた場所である。MAC に含め、記録をほかの pull request や issue に写して使い回せないようにする。
type Scope struct {
	// RepoID は、GitForge の中で変わらない repository の識別子（Forgejo の repository の id、GitHub の node_id）。
	// 名前（owner/name）は使わない。名前を変えたり、別の organization に移したりしても、記録が壊れないようにするため。
	RepoID string
	// Number は、記録のコメントを置いた pull request か issue の番号。
	Number int
}

// Format は、記録を、署名を付けた本文にする。本文の CRLF は LF にし、末尾の改行を落とす。値の改行は、空白にする。
func (k Keyring) Format(r Record, at Scope) (string, error) {
	key, ok := k.Private[k.Current]
	if !ok || !keyIDForm.MatchString(k.Current) {
		return "", fmt.Errorf("pullreq: 署名の秘密鍵 %q がない", k.Current)
	}
	r.Body = strings.TrimRight(strings.ReplaceAll(r.Body, "\r\n", "\n"), "\n")
	trailers := make([]Trailer, len(r.Trailers))
	for i, t := range r.Trailers {
		trailers[i] = Trailer{t.Key, strings.Join(strings.Fields(t.Value), " ")}
	}
	r.Trailers = trailers
	text := canonical(r)
	sig := ed25519.Sign(key, signed(at, text))
	return text + "\n" + KeyMac + ": " + k.Current + ":" + base64.RawStdEncoding.EncodeToString(sig), nil
}

// signed は、署名する中身（置いた場所と、記録の本文）である。
func signed(at Scope, text string) []byte {
	return []byte(fmt.Sprintf("%s#%d\n%s", at.RepoID, at.Number, text))
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
	key, known := k.Public[id]
	if !ok || !known {
		return Record{}, ErrBadMac
	}
	sig, err := base64.RawStdEncoding.DecodeString(got)
	if err != nil || !ed25519.Verify(key, signed(at, canonical(r)), sig) {
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
