package integrations

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
)

func OAuthHostKeyForIntegration(integration Integration) (string, error) {
	switch integration.Provider {
	case ProviderGitHub:
		return "github.github_com", nil
	case ProviderGitLab:
		base := strings.TrimSpace(integration.BaseURL)
		if base == "" {
			return "", fmt.Errorf("%w: gitlab integration base_url is required for oauth", ErrValidation)
		}
		u, err := url.Parse(base)
		if err != nil || u.Host == "" {
			return "", fmt.Errorf("%w: gitlab integration base_url must be a valid absolute URL", ErrValidation)
		}
		host := strings.ToLower(strings.ReplaceAll(u.Hostname(), ".", "_"))
		return "gitlab." + host, nil
	case ProviderLinear:
		return "linear.linear_app", nil
	default:
		return "", fmt.Errorf("%w: provider does not support oauth host derivation", ErrValidation)
	}
}

func OAuthCallbackURL(base string) (string, error) {
	base = strings.TrimSpace(base)
	if base == "" {
		return "", fmt.Errorf("%w: oauth callback base URL is required", ErrValidation)
	}
	u, err := url.Parse(base)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("%w: oauth callback base URL must be an absolute URL", ErrValidation)
	}
	u.Path = path.Join(strings.TrimRight(u.Path, "/"), "/integrations/oauth-callback")
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func linearOAuthViewerIdentity(ctx context.Context, accessToken string) (id, name, email string, err error) {
	body := `{"query":"{ viewer { id name email } }"}`
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, linearGraphQLURL, strings.NewReader(body))
	if err != nil {
		return "", "", "", fmt.Errorf("%w: build linear viewer request: %v", ErrUpstream, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", oauthUserAgent)
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return "", "", "", fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", "", fmt.Errorf("%w: linear viewer api returned status %d", ErrUpstream, resp.StatusCode)
	}
	var result struct {
		Data struct {
			Viewer struct {
				ID    string `json:"id"`
				Name  string `json:"name"`
				Email string `json:"email"`
			} `json:"viewer"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := decodeOAuthAPIResponse(resp.Body, &result); err != nil {
		return "", "", "", fmt.Errorf("%w: decode linear viewer response: %v", ErrUpstream, err)
	}
	if len(result.Errors) > 0 {
		return "", "", "", fmt.Errorf("%w: linear graphql error: %s", ErrUpstream, result.Errors[0].Message)
	}
	return result.Data.Viewer.ID, result.Data.Viewer.Name, result.Data.Viewer.Email, nil
}

func (s *Service) markOAuthRefreshError(ctx context.Context, integration *Integration, cause error) error {
	_, _ = s.integrations.UpdateIntegration(ctx, Integration{
		ID:        integration.ID,
		Status:    StatusError,
		LastError: strings.TrimSpace(cause.Error()),
	})
	return cause
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func integrationHasResolvedToken(integration Integration) bool {
	switch strings.TrimSpace(integration.AuthMethod) {
	case AuthMethodOAuth:
		return integration.HasOAuthToken
	case AuthMethodPAT:
		return integration.HasAPIToken
	default:
		return integration.HasAPIToken || integration.HasOAuthToken
	}
}

func oauthScopes(app OAuthAppConfig, provider string) []string {
	if len(app.Scopes) > 0 {
		return app.Scopes
	}
	switch provider {
	case ProviderGitLab:
		return []string{"api"}
	case ProviderGitHub:
		return []string{"repo", "read:user"}
	case ProviderLinear:
		return []string{"read", "write", "issues:create"}
	}
	return nil
}

func normalizeOAuthRedirectTarget(target string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" {
		return defaultOAuthRedirect, nil
	}
	// Must be an app-relative path. Reject protocol-relative ("//host") and
	// backslash-prefixed ("/\host") forms: browsers normalize "\" to "/", so
	// both resolve to an off-site origin when emitted as the Location header
	// (open redirect).
	if !strings.HasPrefix(target, "/") || (len(target) >= 2 && (target[1] == '/' || target[1] == '\\')) {
		return "", fmt.Errorf("%w: oauth redirect target must be an app-relative path", ErrValidation)
	}
	return target, nil
}

// OAuthResultRedirect appends an oauth result indicator (key=value) to an
// app-relative redirect target so the SPA can surface a success/error toast
// after the provider round-trip. An empty target falls back to the default.
func OAuthResultRedirect(target, key, value string) string {
	target = strings.TrimSpace(target)
	if target == "" {
		target = defaultOAuthRedirect
	}
	sep := "?"
	if strings.Contains(target, "?") {
		sep = "&"
	}
	return target + sep + url.QueryEscape(key) + "=" + url.QueryEscape(value)
}

// OAuthErrorRedirect is the app-relative target used when the callback fails and
// the intended redirect can't be recovered — the user lands back in the app with
// an error indicator instead of a bare error page (no error detail is leaked).
func OAuthErrorRedirect() string {
	return OAuthResultRedirect(defaultOAuthRedirect, "oauth_error", "1")
}

func gitLabUserURL(integration Integration) (string, error) {
	base := gitLabAPIBase(integration.BaseURL)
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("%w: gitlab integration base_url must be a valid absolute URL", ErrValidation)
	}
	u.Path = "/api/v4/user"
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

func oauthAPIGetJSON(ctx context.Context, requestURL string, accessToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("%w: build oauth api request: %v", ErrUpstream, err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(accessToken))
	req.Header.Set("User-Agent", oauthUserAgent)
	resp, err := oauthHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrUpstream, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("%w: oauth api returned status %d", ErrUpstream, resp.StatusCode)
	}
	if err := decodeOAuthAPIResponse(resp.Body, out); err != nil {
		return fmt.Errorf("%w: decode oauth api response: %v", ErrUpstream, err)
	}
	return nil
}

func normalizeOAuthScope(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	raw = strings.ReplaceAll(raw, ",", " ")
	return strings.Join(strings.Fields(raw), " ")
}

func stringifyOAuthSubject(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatInt(int64(v), 10)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return v.String()
	default:
		return strings.TrimSpace(fmt.Sprint(v))
	}
}

func newOAuthStateToken() (string, error) {
	var raw [16]byte
	if _, err := io.ReadFull(rand.Reader, raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}
