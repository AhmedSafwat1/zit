package zitadel

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Claim names as Zitadel emits them on /oidc/v1/userinfo.
const (
	ClaimRoles    = "urn:zitadel:iam:org:project:roles"
	ClaimMetadata = "urn:zitadel:iam:user:metadata"

	MetaSubAccount           = "sub_account"
	MetaAddSubAccountAllowed = "add_sub_account_allowed"
	MetaSubAccountsCount     = "sub_accounts_count"
	MetaAccountID            = "account_id"
	MetaAccountTypeName      = "account_type_name"
	MetaStatusName           = "status_name"
	MetaIsMain               = "is_main"
)

// Claims is the decoded userinfo response.
type Claims struct {
	Sub               string `json:"sub"`
	Iss               string `json:"iss,omitempty"`
	Email             string `json:"email,omitempty"`
	EmailVerified     bool   `json:"email_verified,omitempty"`
	Name              string `json:"name,omitempty"`
	Nickname          string `json:"nickname,omitempty"`
	PreferredUsername string `json:"preferred_username,omitempty"`

	// Roles are the sorted role names from the project roles claim.
	Roles []string `json:"roles"`
	// Metadata holds every metadata value base64-decoded (raw value kept on failure).
	Metadata map[string]string `json:"metadata"`
	// SubAccountOf holds the parent subjects parsed from metadata.sub_account, deduped in order.
	SubAccountOf []string `json:"sub_account_of"`

	// Raw is the unmodified userinfo body.
	Raw json.RawMessage `json:"-"`
}

// Meta returns a decoded metadata value, or "" when absent.
func (c *Claims) Meta(key string) string {
	if c == nil || c.Metadata == nil {
		return ""
	}
	return c.Metadata[key]
}

// MetaBool parses a metadata value with Truthy.
func (c *Claims) MetaBool(key string) bool { return Truthy(c.Meta(key)) }

// MetaInt parses a metadata value as an integer; non-numeric values yield 0.
func (c *Claims) MetaInt(key string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(c.Meta(key)))
	return n
}

// HasRole reports whether role is present (exact match).
func (c *Claims) HasRole(role string) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

type rawUserinfo struct {
	Sub               string          `json:"sub"`
	Iss               string          `json:"iss"`
	Email             string          `json:"email"`
	EmailVerified     bool            `json:"email_verified"`
	Name              string          `json:"name"`
	Nickname          string          `json:"nickname"`
	PreferredUsername string          `json:"preferred_username"`
	Roles             json.RawMessage `json:"urn:zitadel:iam:org:project:roles"`
	Metadata          json.RawMessage `json:"urn:zitadel:iam:user:metadata"`
}

// ParseClaims decodes a userinfo body. It fails only on malformed JSON or a
// missing sub; every nested claim degrades gracefully instead.
func ParseClaims(body []byte) (*Claims, error) {
	var raw rawUserinfo
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	if raw.Sub == "" {
		return nil, fmt.Errorf("decode userinfo: missing sub")
	}
	c := &Claims{
		Sub: raw.Sub, Iss: raw.Iss, Email: raw.Email, EmailVerified: raw.EmailVerified,
		Name: raw.Name, Nickname: raw.Nickname, PreferredUsername: raw.PreferredUsername,
		Roles:    ParseRoles(raw.Roles),
		Metadata: ParseMetadata(raw.Metadata),
		Raw:      append(json.RawMessage(nil), body...),
	}
	c.SubAccountOf = ParseSubAccount(c.Metadata[MetaSubAccount])
	return c, nil
}

// ParseRoles accepts the roles claim as a map (role -> {org_id: domain}) or
// as an array of names and returns the sorted, deduplicated role names.
func ParseRoles(raw json.RawMessage) []string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return []string{}
	}
	set := map[string]struct{}{}
	switch raw[0] {
	case '{':
		var m map[string]json.RawMessage
		if err := json.Unmarshal(raw, &m); err != nil {
			return []string{}
		}
		for k := range m {
			set[k] = struct{}{}
		}
	case '[':
		var arr []any
		if err := json.Unmarshal(raw, &arr); err != nil {
			return []string{}
		}
		for _, v := range arr {
			if s, ok := v.(string); ok && s != "" {
				set[s] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ParseMetadata base64-decodes every value of the metadata claim. A value that
// fails to decode is kept as-is rather than dropped.
func ParseMetadata(raw json.RawMessage) map[string]string {
	out := map[string]string{}
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return out
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return out
	}
	for k, v := range m {
		switch t := v.(type) {
		case string:
			out[k] = DecodeBase64(t)
		case nil:
			out[k] = ""
		default:
			b, _ := json.Marshal(t)
			out[k] = string(b)
		}
	}
	return out
}

// DecodeBase64 decodes s with padding repair, falling back to the raw
// alphabet, and returns s unchanged when nothing decodes.
func DecodeBase64(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	padded := s + strings.Repeat("=", (4-len(s)%4)%4)
	if b, err := base64.StdEncoding.DecodeString(padded); err == nil {
		return string(b)
	}
	if b, err := base64.RawStdEncoding.DecodeString(strings.TrimRight(s, "=")); err == nil {
		return string(b)
	}
	if b, err := base64.URLEncoding.DecodeString(padded); err == nil {
		return string(b)
	}
	if b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "=")); err == nil {
		return string(b)
	}
	return s
}

// ParseSubAccount parses the decoded sub_account metadata value, a JSON
// document of the shape {"main_accounts": {"main_1": "355...", ...}} where
// main_accounts may also be an array. It returns the values (the keys are
// only numbering), deduplicated and in document order.
func ParseSubAccount(decoded string) []string {
	decoded = strings.TrimSpace(decoded)
	if decoded == "" {
		return []string{}
	}
	var doc struct {
		MainAccounts json.RawMessage `json:"main_accounts"`
	}
	if err := json.Unmarshal([]byte(decoded), &doc); err != nil {
		return []string{}
	}
	return uniqueValues(doc.MainAccounts)
}

// uniqueValues collects the scalar values of a JSON object (in document
// order) or array, as strings, dropping empties and duplicates.
func uniqueValues(raw json.RawMessage) []string {
	raw = bytes.TrimSpace(raw)
	out := []string{}
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return out
	}
	seen := map[string]struct{}{}
	add := func(v any) {
		s := scalarString(v)
		if s == "" {
			return
		}
		if _, dup := seen[s]; dup {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return out
	}
	switch tok {
	case json.Delim('{'):
		for dec.More() {
			if _, err := dec.Token(); err != nil { // key
				return out
			}
			var v any
			if err := dec.Decode(&v); err != nil {
				return out
			}
			add(v)
		}
	case json.Delim('['):
		for dec.More() {
			var v any
			if err := dec.Decode(&v); err != nil {
				return out
			}
			add(v)
		}
	}
	return out
}

func scalarString(v any) string {
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case json.Number:
		return t.String()
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	default:
		return ""
	}
}

// Truthy reports whether s is one of 1, yes, true, y, on, enabled (case-insensitive).
func Truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "yes", "true", "y", "on", "enabled":
		return true
	}
	return false
}
