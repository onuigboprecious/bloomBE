package cards

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
)

func getSecretKey() []byte {
	secret := os.Getenv("NFC_SECRET_KEY")
	if secret == "" {
		secret = "bloom_nfc_secret_key_default_2026"
	}
	return []byte(secret)
}

// SignCardUID computes an 8-character hex HMAC-SHA256 signature for a card UID.
func SignCardUID(cardUid string) string {
	mac := hmac.New(sha256.New, getSecretKey())
	mac.Write([]byte(strings.TrimSpace(cardUid)))
	fullHex := hex.EncodeToString(mac.Sum(nil))
	if len(fullHex) > 8 {
		return fullHex[:8]
	}
	return fullHex
}

// VerifyCardSignature checks if an incoming signature matches the calculated HMAC tag for cardUid.
func VerifyCardSignature(cardUid string, signature string) bool {
	if signature == "" {
		return false
	}
	expected := SignCardUID(cardUid)
	return hmac.Equal([]byte(strings.ToLower(signature)), []byte(strings.ToLower(expected)))
}
