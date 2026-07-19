package client

import (
	"context"
	"net/url"
	"strconv"
)

func (c *Client) ListKnowledgeDocuments(ctx context.Context, filter KnowledgeListFilter) (*KnowledgeDocumentList, error) {
	q := url.Values{}
	q.Set("workspace_id", filter.WorkspaceID)
	if filter.LinkedFeatureID != "" {
		q.Set("linked_feature_id", filter.LinkedFeatureID)
	}
	if filter.LinkedRequestID != "" {
		q.Set("linked_request_id", filter.LinkedRequestID)
	}
	if filter.DocumentType != "" {
		q.Set("document_type", filter.DocumentType)
	}
	if filter.Status != "" {
		q.Set("status", filter.Status)
	}
	if filter.IncludeHistory {
		q.Set("include_history", "true")
	}
	if filter.Limit > 0 {
		q.Set("limit", strconv.Itoa(filter.Limit))
	}
	if filter.Offset > 0 {
		q.Set("offset", strconv.Itoa(filter.Offset))
	}
	var out KnowledgeDocumentList
	if err := c.get(ctx, "/documents?"+q.Encode(), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetKnowledgeDocument(ctx context.Context, id, version string) (*KnowledgeDocumentDetail, error) {
	path := "/documents/" + url.PathEscape(id)
	q := url.Values{}
	if workspace := workspaceID(ctx); workspace != "" {
		q.Set("workspace_id", workspace)
	}
	if version != "" {
		q.Set("version", version)
	}
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var out KnowledgeDocumentDetail
	if err := c.get(ctx, path, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateTextKnowledgeDocument(ctx context.Context, in KnowledgeCreateTextInput) (*KnowledgeDocument, error) {
	var out KnowledgeDocument
	if err := c.post(ctx, "/documents/text", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CurateKnowledgeLinks(ctx context.Context, id string, in KnowledgeCurateLinksInput) (*KnowledgeDocument, error) {
	if workspace := workspaceID(ctx); workspace != "" {
		in.WorkspaceID = workspace
	}
	var out KnowledgeDocument
	if err := c.post(ctx, "/documents/"+url.PathEscape(id)+"/links", in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SearchKnowledge(ctx context.Context, in KnowledgeSearchInput) ([]KnowledgeSearchResult, error) {
	var out struct {
		Results []KnowledgeSearchResult `json:"results"`
	}
	if err := c.post(ctx, "/governance/context/search", in, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}
