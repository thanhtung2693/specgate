package mcp

import (
	"context"
	"errors"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/workboard"
)

const (
	contextPackURITemplate         = "specgate://context-pack/{change_request_id}"
	contextPackLaneURITplFE        = "specgate://context-pack/{change_request_id}/fe"
	contextPackLaneURITplBE        = "specgate://context-pack/{change_request_id}/be"
	contextPackArtifactURITemplate = "specgate://context-pack/artifact/{artifact_id}"
	contextPackURIPrefix           = "specgate://context-pack/"
)

// contextPackRef is the typed result of parsing a context-pack URI.
type contextPackRef struct {
	Kind string // "change_request" | "artifact"
	ID   string
	Lane string // "" | "fe" | "be" (change_request only)
}

// Type aliases kept for the wiring layer (MCPServerDeps / MCPHandlerOptions) so
// callers don't need to import governanceops directly.
type ContextPackAttachmentReader = governanceops.ContextPackAttachmentReader
type ContextPackKnowledgeReader = governanceops.ContextPackKnowledgeReader
type ContextPackSkillReader = governanceops.ContextPackSkillReader

// RegisterContextPackResources registers the specgate://context-pack/* resource
// templates. The resource handler delegates assembly to svc.ContextPack.
func RegisterContextPackResources(s *mcpsdk.Server, svc *governanceops.Service) {
	if svc == nil || svc.WorkBoard == nil {
		return
	}
	handler := contextPackResourceHandler(svc)
	for _, tpl := range []struct{ uri, name, desc string }{
		{contextPackURITemplate, "context-pack", "Assembled ChangeRequest+Feature handoff for one work item (full)."},
		{contextPackLaneURITplFE, "context-pack-fe", "Frontend-lane handoff for one work item (omits backend tasks)."},
		{contextPackLaneURITplBE, "context-pack-be", "Backend-lane handoff for one work item (omits frontend tasks)."},
		{contextPackArtifactURITemplate, "context-pack-artifact", "Role-organized artifact handoff (full pack, no lane scoping)."},
	} {
		s.AddResourceTemplate(&mcpsdk.ResourceTemplate{
			URITemplate: tpl.uri,
			Name:        tpl.name,
			Title:       "Governance context pack",
			Description: tpl.desc,
			MIMEType:    "application/json",
		}, handler)
	}
}

func contextPackResourceHandler(svc *governanceops.Service) mcpsdk.ResourceHandler {
	return func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		ref, ok := parseContextPackURI(req.Params.URI)
		if !ok {
			return nil, mcpsdk.ResourceNotFoundError(req.Params.URI)
		}
		result, err := svc.ContextPack(ctx, governanceops.ContextPackInput{
			Kind: ref.Kind,
			ID:   ref.ID,
			Lane: ref.Lane,
		})
		if err != nil {
			if errors.Is(err, workboard.ErrNotFound) {
				return nil, mcpsdk.ResourceNotFoundError(req.Params.URI)
			}
			return nil, err
		}
		text, err := marshalIndentedJSON(result)
		if err != nil {
			return nil, err
		}
		return &mcpsdk.ReadResourceResult{
			Contents: []*mcpsdk.ResourceContents{{
				URI:      req.Params.URI,
				MIMEType: "application/json",
				Text:     text,
			}},
		}, nil
	}
}

// parseContextPackURI accepts:
//   - specgate://context-pack/{cr}              → Kind="change_request"
//   - specgate://context-pack/{cr}/fe|be        → Kind="change_request", Lane="fe"|"be"
//   - specgate://context-pack/artifact/{id}     → Kind="artifact"
//
// Any other form returns ok=false.
func parseContextPackURI(uri string) (ref contextPackRef, ok bool) {
	if !strings.HasPrefix(uri, contextPackURIPrefix) {
		return contextPackRef{}, false
	}
	rest := strings.TrimPrefix(uri, contextPackURIPrefix)
	if rest == "" {
		return contextPackRef{}, false
	}
	parts := strings.Split(rest, "/")
	switch len(parts) {
	case 1:
		if parts[0] == "" || parts[0] == "artifact" {
			return contextPackRef{}, false
		}
		return contextPackRef{Kind: "change_request", ID: parts[0]}, true
	case 2:
		if parts[0] == "artifact" {
			if parts[1] == "" {
				return contextPackRef{}, false
			}
			return contextPackRef{Kind: "artifact", ID: parts[1]}, true
		}
		if parts[0] == "" || (parts[1] != "fe" && parts[1] != "be") {
			return contextPackRef{}, false
		}
		return contextPackRef{Kind: "change_request", ID: parts[0], Lane: parts[1]}, true
	default:
		return contextPackRef{}, false
	}
}
