// Package settings は、設定の DB（SQLite）と、その中の秘密の暗号化である（design/settings/README.md）。
//
// 起動時に渡すのは、DB の場所と、2 つの鍵（バックエンドの鍵と共有の鍵）だけである。ほかの設定は、すべて DB に置く。
// バックエンドは読み書きし、worker は読むだけで開く（design/settings/database.md）。
package settings

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// KeySize は、暗号化の鍵の長さ（バイト）である。鍵は、32 バイトの乱数を base64 にしたものとして渡す（design/settings/secrets.md の「暗号の形」）。
const KeySize = 32

// Key は、秘密を暗号化する鍵である。
type Key struct {
	id  string
	aes cipher.AEAD
}

// ParseKey は、base64 の鍵を読む。
func ParseKey(s string) (*Key, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("鍵は base64 にする：%w", err)
	}
	if len(raw) != KeySize {
		return nil, fmt.Errorf("鍵は %d バイトにする（いまは %d バイト）", KeySize, len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	return &Key{id: hex.EncodeToString(sum[:4]), aes: gcm}, nil
}

// NewKey は、新しい鍵を作り、base64 で返す。
func NewKey() string {
	b := make([]byte, KeySize)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// ID は、鍵の ID である。暗号文の頭に付け、鍵を取り替えるときに、どの鍵で書いた行かを見分ける。
func (k *Key) ID() string { return k.id }

// sealPrefix は、暗号文の形の版である。
const sealPrefix = "v1"

// seal は、plain を AES-256-GCM で暗号化する。追加の認証データに、設定の名前を入れる。
// なぜ：ある設定の暗号文を、別の設定の行に写しても、読めないようにするため（design/settings/secrets.md の「暗号の形」）。
func (k *Key) seal(name string, plain []byte) string {
	nonce := make([]byte, k.aes.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic(err)
	}
	ct := k.aes.Seal(nil, nonce, plain, []byte(name))
	return sealPrefix + ":" + k.id + ":" + base64.StdEncoding.EncodeToString(append(nonce, ct...))
}

// ErrWrongKey は、暗号文が、渡した鍵で書かれていないことである。
var ErrWrongKey = errors.New("settings: 暗号文は、ほかの鍵で書かれている")

// keyIDOf は、暗号文の鍵の ID を返す。
func keyIDOf(sealed string) (string, error) {
	parts := strings.SplitN(sealed, ":", 3)
	if len(parts) != 3 || parts[0] != sealPrefix {
		return "", errors.New("settings: 暗号文の形が正しくない")
	}
	return parts[1], nil
}

// open は、seal の逆である。名前が違えば、読めない。
func (k *Key) open(name, sealed string) ([]byte, error) {
	id, err := keyIDOf(sealed)
	if err != nil {
		return nil, err
	}
	if id != k.id {
		return nil, ErrWrongKey
	}
	raw, err := base64.StdEncoding.DecodeString(strings.SplitN(sealed, ":", 3)[2])
	if err != nil || len(raw) < k.aes.NonceSize() {
		return nil, errors.New("settings: 暗号文の形が正しくない")
	}
	plain, err := k.aes.Open(nil, raw[:k.aes.NonceSize()], raw[k.aes.NonceSize():], []byte(name))
	if err != nil {
		return nil, fmt.Errorf("settings: %s を読めない（鍵か名前が違う）", name)
	}
	return plain, nil
}
