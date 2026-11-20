package redact

import (
	"net/http"
	"strings"
	"testing"
)

func TestStringMasksTokensButKeepsPrefix(t *testing.T) {
	in := "failed with cch_auth_live_ABC123_s3cr3t-Value and cch_llm_live_XYZ_abc; Authorization: Bearer abcdefghijklmnop"
	out := String(in)
	for _, secret := range []string{"s3cr3t-Value", "_abc;", "abcdefghijklmnop"} {
		if strings.Contains(out, secret) {
			t.Fatalf("secret %q survived: %s", secret, out)
		}
	}
	if !strings.Contains(out, "cch_auth_live_ABC123_"+Mask) || !strings.Contains(out, "cch_llm_live_XYZ_"+Mask) {
		t.Fatalf("token prefixes should stay visible: %s", out)
	}
}

func TestHeaderMasksCredentialHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer cch_auth_live_A_b")
	h.Set("Cookie", "session=1")
	h.Set("X-CSRF-Token", "t")
	h.Set("X-CCH-Challenge-Proof", "p")
	h.Set("Content-Type", "application/json")
	m := HeaderMap(h)
	if m["Authorization"] != "Bearer "+Mask || m["Cookie"] != Mask || m["X-Csrf-Token"] != Mask || m["X-Cch-Challenge-Proof"] != Mask {
		t.Fatalf("got %v", m)
	}
	if m["Content-Type"] != "application/json" {
		t.Fatalf("plain headers must stay: %v", m)
	}
}

func TestJSONMasksSecretFieldsAtAnyDepth(t *testing.T) {
	in := `{"name":"p","api_key":"sk-1","nested":{"clientSecret":"cs","list":[{"password":"pw","plaintext":"pt"}]},"token":{"id":"t1"},"count":3,"apiKey":"","codes":["A","B"]}`
	out, err := JSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, secret := range []string{"sk-1", `"cs"`, `"pw"`, `"pt"`, `"A"`} {
		if strings.Contains(s, secret) {
			t.Fatalf("secret %s survived: %s", secret, s)
		}
	}
	for _, kept := range []string{`"name":"p"`, `"id":"t1"`, `"count":3`, `"apiKey":""`} {
		if !strings.Contains(s, kept) {
			t.Fatalf("%s should stay: %s", kept, s)
		}
	}
}

// Credential fields of CCHP request bodies: storage keys, payment channel keys,
// new and temporary passwords, invitation, session and management tokens.
func TestJSONMasksRequestCredentialFields(t *testing.T) {
	in := `{"s3_access_key_id":"AKIA1","s3_secret_access_key":"sak1","secrets":{"secretAccessKey":"sak2","langfuseSecretKey":"lsk"},` +
		`"config":{"secretKey":"sk2"},"new_password":"np","temporaryPassword":"tp","invitation_token":"it1","invitationToken":"it2",` +
		`"new_api_management_token":"mt","token":"t","proxies":[{"password":"pp","hasPassword":true,"host":"h"}],` +
		`"tokenEndpoint":"https://idp.example/token","tokenBasis":"input","apiKeyId":"k1","secret_prefix":"sp"}`
	out, err := JSON([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, secret := range []string{`"sak1"`, `"sak2"`, `"lsk"`, `"sk2"`, `"np"`, `"tp"`, `"it1"`, `"it2"`, `"mt"`, `"t"`, `"pp"`} {
		if strings.Contains(s, secret) {
			t.Fatalf("secret %s survived: %s", secret, s)
		}
	}
	for _, kept := range []string{`"s3_access_key_id":"AKIA1"`, `"hasPassword":true`, `"host":"h"`, `"tokenEndpoint":"https://idp.example/token"`,
		`"tokenBasis":"input"`, `"apiKeyId":"k1"`, `"secret_prefix":"sp"`} {
		if !strings.Contains(s, kept) {
			t.Fatalf("%s should stay: %s", kept, s)
		}
	}
}

func TestJSONMasksExtraFieldsAndKeepsNumbersExact(t *testing.T) {
	out, err := JSON([]byte(`{"webhook":"w","big":12345678901234567890,"n":null,"f":false}`), "webhook")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"big":12345678901234567890,"f":false,"n":null,"webhook":"[redacted]"}` {
		t.Fatalf("got %s", out)
	}
}

func TestJSONRejectsNonJSON(t *testing.T) {
	if _, err := JSON([]byte("not json")); err == nil {
		t.Fatal("want an error")
	}
}
