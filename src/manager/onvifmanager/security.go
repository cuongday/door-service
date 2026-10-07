package onvifmanager

import (
	"crypto/sha1"
	"encoding/base64"
	"html"
	"time"
)

func BuildUsernameToken(username, password string, createdAt time.Time, nonce []byte) string {
	created := createdAt.UTC().Format("2006-01-02T15:04:05.000Z")
	digestInput := make([]byte, 0, len(nonce)+len(created)+len(password))
	digestInput = append(digestInput, nonce...)
	digestInput = append(digestInput, created...)
	digestInput = append(digestInput, password...)
	digest := sha1.Sum(digestInput)

	return `<wsse:Security s:mustUnderstand="1">` +
		`<wsse:UsernameToken>` +
		`<wsse:Username>` + html.EscapeString(username) + `</wsse:Username>` +
		`<wsse:Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` +
		base64.StdEncoding.EncodeToString(digest[:]) + `</wsse:Password>` +
		`<wsse:Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">` +
		base64.StdEncoding.EncodeToString(nonce) + `</wsse:Nonce>` +
		`<wsu:Created>` + created + `</wsu:Created>` +
		`</wsse:UsernameToken>` +
		`</wsse:Security>`
}
