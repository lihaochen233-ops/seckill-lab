// Package auth 提供教学用 HMAC 令牌；不是 JWT，不包含注册/登录/吊销系统。
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

var ErrUnauthorized = errors.New("invalid or expired token")

type Signer struct{ secret []byte }

func New(secret string) *Signer { return &Signer{secret: []byte(secret)} }
func (s *Signer) signature(payload string) []byte {
	h := hmac.New(sha256.New, s.secret)
	h.Write([]byte(payload))
	return h.Sum(nil)
}
func (s *Signer) Mint(user int64, expires time.Time) string {
	payload := strconv.FormatInt(user, 10) + "." + strconv.FormatInt(expires.Unix(), 10)
	return payload + "." + base64.RawURLEncoding.EncodeToString(s.signature(payload))
}
func (s *Signer) Verify(token string, now time.Time) (int64, error) {
	if len(token) > 160 {
		return 0, ErrUnauthorized
	}
	p := strings.Split(token, ".")
	if len(p) != 3 {
		return 0, ErrUnauthorized
	}
	sig, err := base64.RawURLEncoding.DecodeString(p[2])
	if err != nil || !hmac.Equal(sig, s.signature(p[0]+"."+p[1])) {
		return 0, ErrUnauthorized
	}
	user, err := strconv.ParseInt(p[0], 10, 64)
	if err != nil || user <= 0 {
		return 0, ErrUnauthorized
	}
	exp, err := strconv.ParseInt(p[1], 10, 64)
	if err != nil || now.Unix() >= exp {
		return 0, ErrUnauthorized
	}
	return user, nil
}
