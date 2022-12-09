package security

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// parseJWT parses a jwt and returns it as json bytes.
//
// This function DOES NOT VERIFY THE TOKEN. You still have to verify the token to confirm that the token holder has not
// altered the claims.
//
// This code is copied almost verbatim from go-oidc (https://github.com/coreos/go-oidc).
func parseJWT(p string) ([]byte, error) {
	parts := strings.Split(p, ".")
	if len(parts) < 2 {
		return nil, fmt.Errorf("malformed jwt, expected 3 parts got %d", len(parts))
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("malformed jwt payload: %v", err)
	}
	return payload, nil
}

// jwtWithOnlyAudClaim represents a jwt where only the "aud" claim is present. This struct allows us to unmarshal a jwt
// and be confident that the only information retrieved from that jwt is the "aud" claim.
type jwtWithOnlyAudClaim struct {
	Aud []string `json:"aud"`
}

// getUnverifiedAudClaim gets the "aud" claim from a jwt.
//
// This function DOES NOT VERIFY THE TOKEN. You still have to verify the token to confirm that the token holder has not
// altered the "aud" claim.
//
// This code is copied almost verbatim from go-oidc (https://github.com/coreos/go-oidc).
func getUnverifiedAudClaim(rawIDToken string) ([]string, error) {
	payload, err := parseJWT(rawIDToken)
	if err != nil {
		return nil, fmt.Errorf("malformed jwt: %v", err)
	}
	var token jwtWithOnlyAudClaim
	if err = json.Unmarshal(payload, &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal claims: %v", err)
	}
	return token.Aud, nil
}

// UnverifiedHasAudClaim returns whether the "aud" claim is present in the given JWT.
//
// This function DOES NOT VERIFY THE TOKEN. You still have to verify the token to confirm that the token holder has not
// altered the "aud" claim.
func UnverifiedHasAudClaim(rawIDToken string) (bool, error) {
	aud, err := getUnverifiedAudClaim(rawIDToken)
	if err != nil {
		return false, fmt.Errorf("failed to determine whether token had an audience claim: %w", err)
	}
	return aud != nil, nil
}
