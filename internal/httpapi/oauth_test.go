package httpapi

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/BrOrlandi/whatsapp-mcp/internal/auth"
	"github.com/BrOrlandi/whatsapp-mcp/internal/evolution"
	"github.com/BrOrlandi/whatsapp-mcp/internal/health"
	"github.com/BrOrlandi/whatsapp-mcp/internal/mcp"
	"github.com/BrOrlandi/whatsapp-mcp/internal/mcphttp"
	"github.com/BrOrlandi/whatsapp-mcp/internal/store"
)

const testIssuer = "https://mcp.example"
const testCallback = "https://chatgpt.com/connector_platform_oauth_redirect"
const testOtherCallback = "https://chatgpt.com/connector/oauth/other"
const testVerifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"

func oauthQuery() url.Values {
	sum := sha256.Sum256([]byte(testVerifier))
	return url.Values{"client_id": {"chatgpt"}, "redirect_uri": {testCallback}, "response_type": {"code"},
		"resource": {testIssuer + "/mcp"}, "scope": {"whatsapp"}, "state": {"state with spaces & symbols"},
		"code_challenge_method": {"S256"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}}
}

func TestOAuthRejectsUnregisteredCallbacksAndMissingPKCE(t *testing.T) {
	o := &oauthServer{issuer: testIssuer, clientID: "chatgpt", redirects: []string{testCallback}}
	if _, err := o.authorization(oauthQuery()); err != nil {
		t.Fatal(err)
	}
	for field, bad := range map[string]string{"redirect_uri": "https://attacker.example/callback", "client_id": "other",
		"resource": "https://other.example/mcp", "response_type": "token", "code_challenge_method": "plain",
		"code_challenge": "short", "scope": "whatsapp admin"} {
		q := oauthQuery()
		q.Set(field, bad)
		if _, err := o.authorization(q); err == nil {
			t.Fatalf("accepted invalid %s", field)
		}
	}
	q := oauthQuery()
	q.Add("resource", testIssuer+"/mcp")
	if _, err := o.authorization(q); err == nil {
		t.Fatal("accepted duplicate resource")
	}
	for _, invalid := range []string{"", "short", strings.Repeat("a", 129), strings.Repeat("a", 43) + "!"} {
		if validVerifier(invalid) {
			t.Fatal("accepted invalid verifier")
		}
	}
	for _, invalid := range []string{"https://attacker.example/oauth/authorize", "//attacker.example/oauth/authorize", "/login", "/oauth/authorize#fragment", "/%6fauth/authorize"} {
		if oauthReturn(invalid) != "" {
			t.Fatalf("accepted login redirect %q", invalid)
		}
	}
	if oauthReturn("/oauth/authorize?state=ok") == "" {
		t.Fatal("lost the legitimate login return path")
	}
	for _, origin := range []string{"http://mcp.example", "https://mcp.example/path", "https://u:p@mcp.example", "https://mcp.example?q=x", "https://mcp.example#x"} {
		if _, err := WithOAuth(nil, origin, "chatgpt", []string{testCallback}); err == nil {
			t.Fatalf("accepted invalid issuer %q", origin)
		}
	}
	for _, callback := range []string{"http://attacker.example", "https://u:p@attacker.example", "https://chatgpt.com/#fragment"} {
		if _, err := WithOAuth(nil, testIssuer, "chatgpt", []string{callback}); err == nil {
			t.Fatalf("accepted invalid callback %q", callback)
		}
	}
}

type oauthTestAuth struct{ db *store.Store }

func (a oauthTestAuth) Authenticate(ctx context.Context, secret string) (string, error) {
	key, err := a.db.ResolveAPIKey(ctx, secret, testIssuer+"/mcp")
	return key.InstanceID, err
}

// Use an isolated schema in an explicitly provided test database. The default
// suite still checks validation; this test exercises real PostgreSQL locking.
func oauthDatabase(t *testing.T) (*store.Store, string) {
	t.Helper()
	dsn := os.Getenv("OAUTH_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set OAUTH_TEST_DATABASE_URL to run the PostgreSQL OAuth integration check")
	}
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := fmt.Sprintf("oauth_test_%d", time.Now().UnixNano())
	if _, err := admin.Exec("CREATE SCHEMA " + schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec("DROP SCHEMA " + schema + " CASCADE"); err != nil {
			t.Error(err)
		}
		admin.Close()
	})
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	db, err := store.Open(context.Background(), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db, u.String()
}

func TestOAuthEndToEndPostgreSQL(t *testing.T) {
	db, dsn := oauthDatabase(t)
	ctx := context.Background()
	hash, err := auth.HashPassword("test-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateAdmin(ctx, "admin@example.com", hash); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"one", "two"} {
		if err := db.SaveInstance(ctx, id, "Conta "+id, "internal-token"); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SelectInstance(ctx, "one"); err != nil {
		t.Fatal(err)
	}
	option, err := WithOAuth(db, testIssuer, "chatgpt", []string{testCallback, testOtherCallback})
	if err != nil {
		t.Fatal(err)
	}
	state := health.NewState()
	web := NewWebHandler(db, &fakeEvolution{}, state, testSessionKey(), testIssuer, "", false, "", time.Minute, nil, option)
	server := mcp.New(db, evolution.New("http://127.0.0.1:1", "", time.Second), state, time.Minute)
	remote := mcphttp.New(server, oauthTestAuth{db}, nil, testIssuer+"/.well-known/oauth-protected-resource/mcp")
	ts := httptest.NewServer(FullHandler(state, time.Minute, web, remote, nil))
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	client := &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	request := func(method, path string, values url.Values, token string, want int) (http.Header, string) {
		t.Helper()
		var body io.Reader
		if values != nil {
			body = strings.NewReader(values.Encode())
		}
		req, err := http.NewRequest(method, ts.URL+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if values != nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want {
			t.Fatalf("%s %s got %d, want %d: %s", method, path, resp.StatusCode, want, data)
		}
		return resp.Header, string(data)
	}
	for _, path := range []string{"/.well-known/oauth-protected-resource", "/.well-known/oauth-protected-resource/mcp"} {
		_, body := request("GET", path, nil, "", 200)
		var metadata map[string]any
		if err := json.Unmarshal([]byte(body), &metadata); err != nil {
			t.Fatal(err)
		}
		if metadata["resource"] != testIssuer+"/mcp" {
			t.Fatal("incorrect resource metadata")
		}
	}
	_, metadata := request("GET", "/.well-known/oauth-authorization-server", nil, "", 200)
	if !strings.Contains(metadata, `"code_challenge_methods_supported":["S256"]`) || !strings.Contains(metadata, `"authorization_response_iss_parameter_supported":true`) {
		t.Fatal("missing PKCE / issuer metadata")
	}
	headers, _ := request("GET", "/mcp", nil, "", 401)
	if !strings.Contains(headers.Get("WWW-Authenticate"), `resource_metadata="`+testIssuer+"/.well-known/oauth-protected-resource/mcp") {
		t.Fatal("missing auth discovery challenge")
	}

	path := "/oauth/authorize?" + oauthQuery().Encode()
	headers, _ = request("GET", path, nil, "", 303)
	login := headers.Get("Location")
	if !strings.HasPrefix(login, "/login?next=") {
		t.Fatal("did not preserve authorization across login")
	}
	headers, _ = request("POST", login, url.Values{"username": {"admin@example.com"}, "password": {"test-password"}}, "", 303)
	if headers.Get("Location") != path {
		t.Fatal("login lost the authorization request")
	}

	consent := func() url.Values {
		t.Helper()
		_, body := request("GET", path, nil, "", 200)
		values := url.Values{}
		for _, pair := range regexp.MustCompile(`name="([^"]+)" value="([^"]*)"`).FindAllStringSubmatch(body, -1) {
			values.Set(pair[1], html.UnescapeString(pair[2]))
		}
		if values.Get("csrf") == "" {
			t.Fatal("missing consent CSRF token")
		}
		values.Set("decision", "allow")
		return values
	}
	approve := func() string {
		t.Helper()
		headers, _ := request("POST", path, consent(), "", 303)
		u, err := url.Parse(headers.Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		if u.Query().Get("iss") != testIssuer || u.Query().Get("state") != oauthQuery().Get("state") || u.Query().Get("code") == "" {
			t.Fatal("callback lost code, issuer or state")
		}
		return u.Query().Get("code")
	}

	values := consent()
	values.Set("csrf", "tampered")
	request("POST", path, values, "", 403)
	values = consent()
	values.Set("instance_id", "two")
	request("POST", path, values, "", 403)
	values = consent()
	values.Set("decision", "deny")
	headers, _ = request("POST", path, values, "", 303)
	denied, _ := url.Parse(headers.Get("Location"))
	if denied.Query().Get("error") != "access_denied" || denied.Query().Get("iss") != testIssuer {
		t.Fatal("invalid denial callback")
	}
	values = consent()
	if err := db.SelectInstance(ctx, "two"); err != nil {
		t.Fatal(err)
	}
	headers, _ = request("POST", path, values, "", 303)
	callback, _ := url.Parse(headers.Get("Location"))
	code := callback.Query().Get("code")
	if code == "" {
		t.Fatal("no authorization code")
	}

	codeForm := func(code string) url.Values {
		return url.Values{"client_id": {"chatgpt"}, "grant_type": {"authorization_code"}, "code": {code},
			"redirect_uri": {testCallback}, "resource": {testIssuer + "/mcp"}, "code_verifier": {testVerifier}}
	}
	refreshForm := func(secret string) url.Values {
		return url.Values{"client_id": {"chatgpt"}, "grant_type": {"refresh_token"}, "refresh_token": {secret}, "resource": {testIssuer + "/mcp"}}
	}
	decodeToken := func(body string) store.OAuthToken {
		t.Helper()
		var token store.OAuthToken
		if err := json.Unmarshal([]byte(body), &token); err != nil {
			t.Fatal(err)
		}
		if token.AccessToken == "" || token.RefreshToken == "" || token.TokenType != "Bearer" || token.ExpiresIn <= 0 || token.ExpiresIn > 3600 {
			t.Fatal("invalid token response")
		}
		return token
	}
	bad := codeForm(code)
	bad.Set("code_verifier", strings.Repeat("x", 43))
	request("POST", "/oauth/token", bad, "", 400)
	bad = codeForm(code)
	bad.Set("redirect_uri", testOtherCallback)
	request("POST", "/oauth/token", bad, "", 400)
	bad = codeForm(code)
	bad.Set("resource", "https://other.example/mcp")
	request("POST", "/oauth/token", bad, "", 400)
	headers, body := request("POST", "/oauth/token", codeForm(code), "", 200)
	if headers.Get("Cache-Control") != "no-store" {
		t.Fatal("token response may be cached")
	}
	token := decodeToken(body)
	request("POST", "/oauth/token", codeForm(code), "", 400)
	key, err := db.ResolveAPIKey(ctx, token.AccessToken, testIssuer+"/mcp")
	if err != nil || key.InstanceID != "one" {
		t.Fatal("token was bound to a changed selection instead of the consented instance")
	}
	if _, err := db.ResolveAPIKey(ctx, token.AccessToken, "https://other.example/mcp"); !errors.Is(err, store.ErrKeyUnknown) {
		t.Fatal("token accepted for the wrong audience")
	}
	// A second connection sees the grant too; persistence is not in-process.
	reopened, err := store.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.ResolveAPIKey(ctx, token.AccessToken, testIssuer+"/mcp"); err != nil {
		t.Fatal(err)
	}
	reopened.Close()

	mcpCall := func(access string, want int) {
		t.Helper()
		req, _ := http.NewRequest("POST", ts.URL+"/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
		req.Header.Set("Authorization", "Bearer "+access)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != want || want == 200 && !strings.Contains(string(data), `"tools"`) {
			t.Fatalf("MCP got %d: %s", resp.StatusCode, data)
		}
	}
	mcpCall(token.AccessToken, 200)
	legacy, digest, prefix, _ := store.NewAPIKey()
	if err := db.CreateAPIKey(ctx, "existing client", "one", digest, prefix); err != nil {
		t.Fatal(err)
	}
	mcpCall(legacy, 200)
	if _, err := db.DB.Exec(`UPDATE api_keys SET expires_at=now()-interval '1 second' WHERE id=$1`, key.ID); err != nil {
		t.Fatal(err)
	}
	mcpCall(token.AccessToken, 401)
	_, body = request("POST", "/oauth/token", refreshForm(token.RefreshToken), "", 200)
	rotated := decodeToken(body)
	mcpCall(rotated.AccessToken, 200)
	mcpCall(token.AccessToken, 401)
	request("POST", "/oauth/token", refreshForm(token.RefreshToken), "", 400)
	mcpCall(rotated.AccessToken, 401)
	request("POST", "/oauth/token", refreshForm(rotated.RefreshToken), "", 400)

	_, body = request("POST", "/oauth/token", codeForm(approve()), "", 200)
	token = decodeToken(body)
	request("POST", "/oauth/revoke", url.Values{"client_id": {"chatgpt"}, "token": {"unknown"}}, "", 200)
	mcpCall(token.AccessToken, 200)
	request("POST", "/oauth/revoke", url.Values{"client_id": {"chatgpt"}, "token": {token.RefreshToken}}, "", 200)
	mcpCall(token.AccessToken, 401)
	request("POST", "/oauth/token", refreshForm(token.RefreshToken), "", 400)
	_, body = request("POST", "/oauth/token", codeForm(approve()), "", 200)
	token = decodeToken(body)
	key, err = db.ResolveAPIKey(ctx, token.AccessToken, testIssuer+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.RevokeAPIKey(ctx, key.ID); err != nil {
		t.Fatal(err)
	}
	mcpCall(token.AccessToken, 401)
	request("POST", "/oauth/token", refreshForm(token.RefreshToken), "", 400)

	_, body = request("POST", "/oauth/token", codeForm(approve()), "", 200)
	token = decodeToken(body)
	key, err = db.ResolveAPIKey(ctx, token.AccessToken, testIssuer+"/mcp")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE oauth_grants SET expires_at=now()-interval '1 second' WHERE key_id=$1`, key.ID); err != nil {
		t.Fatal(err)
	}
	mcpCall(token.AccessToken, 401)
	request("POST", "/oauth/token", refreshForm(token.RefreshToken), "", 400)

	// Concurrent code redemption succeeds exactly once at the database boundary.
	grant := store.OAuthAuthorization{ClientID: "chatgpt", RedirectURI: testCallback, Resource: testIssuer + "/mcp", Challenge: oauthQuery().Get("code_challenge"), InstanceID: "one"}
	code, err = db.CreateOAuthCode(ctx, grant)
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := db.ExchangeOAuthToken(ctx, store.OAuthExchange{GrantType: "authorization_code", Credential: code, ClientID: grant.ClientID, RedirectURI: grant.RedirectURI, Resource: grant.Resource, Challenge: grant.Challenge})
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, store.ErrOAuthGrant) {
			t.Fatal(err)
		}
	}
	if successes != 1 {
		t.Fatalf("code was redeemed %d times", successes)
	}
	code, err = db.CreateOAuthCode(ctx, grant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.DB.Exec(`UPDATE oauth_grants SET code_expires_at=now()-interval '1 second' WHERE code_hash=$1`, store.HashAPIKey(code)); err != nil {
		t.Fatal(err)
	}
	request("POST", "/oauth/token", codeForm(code), "", 400)
}
