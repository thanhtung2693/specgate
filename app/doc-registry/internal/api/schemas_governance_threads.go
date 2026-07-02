package api

import "time"

type GovernanceThreadDTO struct {
	ThreadID  string    `json:"thread_id"`
	Title     string    `json:"title"`
	Preview   string    `json:"preview"`
	Archived  bool      `json:"archived"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type ListGovernanceThreadsInput struct {
	Limit  int  `query:"limit" minimum:"1" maximum:"100" default:"10"`
	Offset int  `query:"offset" minimum:"0" default:"0"`
	All    bool `query:"all" doc:"Include archived threads when true."`
}

type ListGovernanceThreadsOutput struct {
	Body struct {
		Items []GovernanceThreadDTO `json:"items"`
		Total int64                 `json:"total"`
	}
}

type UpsertGovernanceThreadInput struct {
	ThreadID string `path:"thread_id"`
	Body     struct {
		Title     string     `json:"title,omitempty"`
		Preview   string     `json:"preview,omitempty"`
		UpdatedAt *time.Time `json:"updated_at,omitempty"`
	}
}

type UpsertGovernanceThreadOutput struct {
	Body GovernanceThreadDTO
}

type DeleteGovernanceThreadInput struct {
	ThreadID string `path:"thread_id"`
}

type DeleteGovernanceThreadOutput struct{}
