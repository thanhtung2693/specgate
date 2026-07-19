package api

import (
	"net/http"

	"github.com/specgate/doc-registry/internal/buildinfo"
	"github.com/specgate/doc-registry/internal/clipackages"
)

// serveCLIInstallScript renders the instance-aware CLI installer wrapper and
// writes it as a shell script. The script downloads the public installer for
// the server's current version and configures it to point at this instance.
func serveCLIInstallScript(w http.ResponseWriter, r *http.Request) {
	serverURL := requestBaseURL(r)
	body, err := clipackages.RenderInstallScript(serverURL, buildinfo.Version)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	_, _ = w.Write([]byte(body))
}
