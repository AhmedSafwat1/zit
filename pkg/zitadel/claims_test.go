package zitadel

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return b
}

func TestParseClaims_Fixtures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name             string
		fixture          string
		wantSub          string
		wantNickname     string
		wantRoles        []string
		wantSubAccountOf []string
		wantStatus       string
		wantAllowed      bool
		wantCount        int
	}{
		{
			name: "admin with map roles and arabic nickname", fixture: "userinfo_admin.json",
			wantSub: "355646255235338042", wantNickname: "شركة روازن الدولية",
			wantRoles:        []string{"ADMIN", "jawaly_ai_super_admin"},
			wantSubAccountOf: []string{"355646368934527195", "355646755028599003"},
			wantStatus:       "Active", wantAllowed: true, wantCount: 34,
		},
		{
			name: "sub account with array roles and array main_accounts", fixture: "userinfo_sub_account.json",
			wantSub: "355646999999999999", wantNickname: "فرع جدة",
			wantRoles:        []string{"REPORTS", "USER"},
			wantSubAccountOf: []string{"355646368934527195", "355646755028599003"},
			wantStatus:       "Active", wantAllowed: false, wantCount: 0,
		},
		{
			name: "suspended account without sub_account metadata", fixture: "userinfo_suspended.json",
			wantSub: "355646111111111111", wantNickname: "حساب موقوف",
			wantRoles:        []string{"USER"},
			wantSubAccountOf: []string{},
			wantStatus:       "Suspended", wantAllowed: false, wantCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			body := loadFixture(t, tt.fixture)
			c, err := ParseClaims(body)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSub, c.Sub)
			assert.Equal(t, tt.wantNickname, c.Nickname)
			assert.Equal(t, tt.wantRoles, c.Roles)
			assert.Equal(t, tt.wantSubAccountOf, c.SubAccountOf)
			assert.Equal(t, tt.wantStatus, c.Meta(MetaStatusName))
			assert.Equal(t, tt.wantAllowed, c.MetaBool(MetaAddSubAccountAllowed))
			assert.Equal(t, tt.wantCount, c.MetaInt(MetaSubAccountsCount))
			assert.JSONEq(t, string(body), string(c.Raw), "raw body is preserved")
		})
	}
}

func TestParseClaims_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
	}{
		{name: "malformed json", body: `{"sub": `},
		{name: "missing sub", body: `{"email": "x@y"}`},
		{name: "empty body", body: ``},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := ParseClaims([]byte(tt.body))
			require.Error(t, err)
		})
	}
}

func TestParseRoles(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "map form yields sorted keys", raw: `{"b": {"1": "x"}, "a": {"1": "x"}}`, want: []string{"a", "b"}},
		{name: "array form yields sorted deduped names", raw: `["z", "a", "z"]`, want: []string{"a", "z"}},
		{name: "array ignores non strings", raw: `["a", 1, null]`, want: []string{"a"}},
		{name: "missing claim yields empty slice", raw: ``, want: []string{}},
		{name: "null claim yields empty slice", raw: `null`, want: []string{}},
		{name: "garbage yields empty slice", raw: `"str"`, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ParseRoles(json.RawMessage(tt.raw)))
		})
	}
}

func TestDecodeBase64(t *testing.T) {
	t.Parallel()
	std := base64.StdEncoding.EncodeToString([]byte("Active"))
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "padded standard", in: std, want: "Active"},
		{name: "padding stripped is repaired", in: "QWN0aXZl", want: "Active"},
		{name: "one char short of padding", in: "MQ", want: "1"},
		{name: "arabic utf8 round trips", in: base64.StdEncoding.EncodeToString([]byte("شركة")), want: "شركة"},
		{name: "url alphabet falls back", in: base64.RawURLEncoding.EncodeToString([]byte{0xfb, 0xff}), want: string([]byte{0xfb, 0xff})},
		{name: "invalid keeps raw string", in: "***not base64***", want: "***not base64***"},
		{name: "empty stays empty", in: "", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, DecodeBase64(tt.in))
		})
	}
}

func TestParseMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  string
		want map[string]string
	}{
		{name: "decodes every value", raw: `{"a": "MQ==", "b": "QWN0aXZl"}`, want: map[string]string{"a": "1", "b": "Active"}},
		{name: "undecodable value kept raw", raw: `{"a": "***"}`, want: map[string]string{"a": "***"}},
		{name: "non string value is json encoded", raw: `{"a": 5, "b": null}`, want: map[string]string{"a": "5", "b": ""}},
		{name: "missing claim is empty map", raw: ``, want: map[string]string{}},
		{name: "wrong shape is empty map", raw: `[1,2]`, want: map[string]string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ParseMetadata(json.RawMessage(tt.raw)))
		})
	}
}

func TestParseSubAccount(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "map values in document order", in: `{"main_accounts": {"main_2": "b", "main_1": "a"}}`, want: []string{"b", "a"}},
		{name: "duplicates removed preserving first", in: `{"main_accounts": {"m1": "a", "m2": "b", "m3": "a"}}`, want: []string{"a", "b"}},
		{name: "array form accepted", in: `{"main_accounts": ["a", "b"]}`, want: []string{"a", "b"}},
		{name: "numeric values become strings without float loss", in: `{"main_accounts": [355646755028599003]}`, want: []string{"355646755028599003"}},
		{name: "empty values skipped", in: `{"main_accounts": {"m1": "", "m2": "a"}}`, want: []string{"a"}},
		{name: "missing main_accounts is empty", in: `{}`, want: []string{}},
		{name: "not json is empty", in: `nope`, want: []string{}},
		{name: "empty string is empty", in: ``, want: []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ParseSubAccount(tt.in))
		})
	}
}

func TestTruthy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want bool
	}{
		{name: "1", in: "1", want: true},
		{name: "yes", in: "yes", want: true},
		{name: "TRUE upper", in: "TRUE", want: true},
		{name: "y", in: "y", want: true},
		{name: "On mixed", in: "On", want: true},
		{name: "enabled padded", in: " enabled ", want: true},
		{name: "0", in: "0", want: false},
		{name: "no", in: "no", want: false},
		{name: "empty", in: "", want: false},
		{name: "random", in: "maybe", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, Truthy(tt.in))
		})
	}
}
