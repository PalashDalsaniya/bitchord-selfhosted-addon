package navidrome

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"net/url"
)

// Credential turns a username and password into the Subsonic token form:
// the token is md5(password + salt) and the salt is random. The pair keeps
// working until the password changes, so only it is stored and the
// password is not.
func Credential(username, password string) string {
	salt := make([]byte, 8)
	rand.Read(salt)
	saltHex := hex.EncodeToString(salt)
	sum := md5.Sum([]byte(password + saltHex))
	return url.Values{"u": {username}, "t": {hex.EncodeToString(sum[:])}, "s": {saltHex}}.Encode()
}

// ParseCredential reads a credential made by Credential. Anything else, such
// as a Navidrome API key, is not one.
func ParseCredential(credential string) (username, token, salt string, ok bool) {
	values, err := url.ParseQuery(credential)
	if err != nil {
		return "", "", "", false
	}
	username, token, salt = values.Get("u"), values.Get("t"), values.Get("s")
	return username, token, salt, username != "" && token != "" && salt != ""
}

type Session struct {
	Credential string
	Username   string
}

// SignIn checks a username and password with a ping and returns the
// credential to store in their place.
func SignIn(ctx context.Context, baseURL, username, password string) (Session, error) {
	credential := Credential(username, password)
	if _, err := New(Options{BaseURL: baseURL, Credential: credential}).Version(ctx); err != nil {
		return Session{}, err
	}
	return Session{Credential: credential, Username: username}, nil
}
