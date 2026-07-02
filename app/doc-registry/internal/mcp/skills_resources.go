package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/specgate/doc-registry/internal/skills"
)

const (
	skillListURI           = "specgate://skills"
	skillDetailURITemplate = "specgate://skills/{id}"
)

// skillListItem is the JSON shape for MCP resources/list (no prompt payload).
type skillListItem struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func skillListItemFromSkill(s skills.Skill) skillListItem {
	return skillListItem{
		ID:          s.ID,
		Name:        s.Name,
		Description: s.Description,
		UpdatedAt:   s.UpdatedAt,
	}
}

func marshalIndentedJSON(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// RegisterSkillResources registers list + detail resources on the MCP server.
// No-op when svc is nil.
func RegisterSkillResources(s *mcpsdk.Server, svc *skills.Service) {
	if svc == nil {
		return
	}

	s.AddResource(&mcpsdk.Resource{
		URI:         skillListURI,
		Name:        "skills",
		Title:       "Doc Registry skills (list)",
		Description: "JSON array of user-defined skills: id, name, description, updated_at (no prompt).",
		MIMEType:    "application/json",
	}, skillListResourceHandler(svc))

	s.AddResourceTemplate(&mcpsdk.ResourceTemplate{
		URITemplate: skillDetailURITemplate,
		Name:        "skill",
		Title:       "Doc Registry skill (detail)",
		Description: "JSON object for one skill including prompt and timestamps.",
		MIMEType:    "application/json",
	}, skillDetailResourceHandler(svc))
}

func skillListResourceHandler(svc *skills.Service) mcpsdk.ResourceHandler {
	return func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		if req.Params.URI != skillListURI {
			return nil, mcpsdk.ResourceNotFoundError(req.Params.URI)
		}
		all, err := svc.List(ctx)
		if err != nil {
			return nil, err
		}
		items := make([]skillListItem, 0, len(all))
		for i := range all {
			items = append(items, skillListItemFromSkill(all[i]))
		}
		text, err := marshalIndentedJSON(items)
		if err != nil {
			return nil, err
		}
		return &mcpsdk.ReadResourceResult{
			Contents: []*mcpsdk.ResourceContents{{
				URI:      skillListURI,
				MIMEType: "application/json",
				Text:     text,
			}},
		}, nil
	}
}

func skillDetailResourceHandler(svc *skills.Service) mcpsdk.ResourceHandler {
	return func(ctx context.Context, req *mcpsdk.ReadResourceRequest) (*mcpsdk.ReadResourceResult, error) {
		id, ok := parseSkillDetailURI(req.Params.URI)
		if !ok {
			return nil, mcpsdk.ResourceNotFoundError(req.Params.URI)
		}
		sk, err := svc.Get(ctx, id)
		if err != nil {
			if skills.IsNotFound(err) {
				return nil, mcpsdk.ResourceNotFoundError(req.Params.URI)
			}
			return nil, err
		}
		text, err := marshalIndentedJSON(sk)
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

func parseSkillDetailURI(uri string) (id string, ok bool) {
	const prefix = "specgate://skills/"
	if !strings.HasPrefix(uri, prefix) {
		return "", false
	}
	id = strings.TrimPrefix(uri, prefix)
	if id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}
