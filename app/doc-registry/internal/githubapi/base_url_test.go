package githubapi

import "testing"

func TestAPIURL(t *testing.T) {
	t.Parallel()

	for input, want := range map[string]string{
		"":                           "https://api.github.com",
		"https://github.com":         "https://api.github.com",
		"http://www.github.com/":     "https://api.github.com",
		"https://github.com.evil":    "https://github.com.evil/api/v3",
		"https://ghe.example/":       "https://ghe.example/api/v3",
		"https://ghe.example/prefix": "https://ghe.example/prefix/api/v3",
	} {
		if got := APIURL(input); got != want {
			t.Errorf("APIURL(%q) = %q, want %q", input, got, want)
		}
	}
}
