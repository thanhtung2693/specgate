package mcp

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/specgate/doc-registry/internal/agentsclient"
	"github.com/specgate/doc-registry/internal/artifact"
	"github.com/specgate/doc-registry/internal/artifactedit"
	"github.com/specgate/doc-registry/internal/governanceops"
	"github.com/specgate/doc-registry/internal/governanceprofile"
	"github.com/specgate/doc-registry/internal/mcp/repo"
	"github.com/specgate/doc-registry/internal/mcp/tools"
	"github.com/specgate/doc-registry/internal/notifications"
	"github.com/specgate/doc-registry/internal/skills"
	"github.com/specgate/doc-registry/internal/workboard"
)

// ReadinessRunner is re-exported from the tools package so the api wiring layer
// can hold the dependency without importing tools directly.
type ReadinessRunner = tools.ReadinessRunner

// LLMGatesRunner is re-exported from the tools package so the api wiring layer
// can hold the dependency without importing tools directly.
type LLMGatesRunner = tools.LLMGatesRunner

// DeliveryReviewTrigger is re-exported from the tools package so the api wiring
// layer can hold the dependency without importing tools directly.
type DeliveryReviewTrigger = tools.DeliveryReviewTrigger

// QuickWorkItemCreator is re-exported from the tools package so the api wiring
// layer can hold the dependency without importing tools directly.
type QuickWorkItemCreator = tools.QuickWorkItemCreator

type MCPServerDeps struct {
	RepoProviders       map[string]repo.RepoProvider
	RepoDefaultRefs     map[string]string
	Knowledge           tools.KnowledgeSearcher
	Artifacts           artifact.Service
	ArtifactEdit        artifactedit.Store
	WorkBoard           workboard.Store
	Skills              *skills.Service
	Feedback            tools.FeedbackStore
	TrackerLinks        tools.WorkItemTrackerReader
	Profiles            *governanceprofile.Service
	Attachments         ContextPackAttachmentReader
	KnowledgeProvenance ContextPackKnowledgeReader
	// Events (nil-safe) publishes a compact invalidation signal when a coding
	// agent reports feedback. Polling clients can ignore it; future adapters can
	// use it to refresh narrow views.
	Events       notifications.Publisher
	APIKey       string
	Budget       *BudgetTracker
	MaxRepoCalls int
	MaxRepoBytes int
	// AppBaseURL is the public SpecGate UI origin used to build review_url in
	// specgate_publish results (e.g. "https://app.example.com").
	AppBaseURL string
	// Readiness delegates the in-IDE readiness check to the agents service.
	// When nil (AGENTS_BASE_URL unset), specgate_check_readiness is not
	// registered. Soft edge: failures surface as a clear tool error.
	Readiness tools.ReadinessRunner
	// LLMGates delegates running LLM quality gates for a CR to the agents
	// service. When nil (AGENTS_BASE_URL unset), run_llm_gates is not registered.
	LLMGates tools.LLMGatesRunner
	// DeliveryReview delegates triggering the delivery review for a CR to the
	// agents service. When nil (AGENTS_BASE_URL unset), trigger_delivery_review
	// is not registered.
	DeliveryReview tools.DeliveryReviewTrigger
	// QuickWorkItem delegates quick-route CR creation from issue content to the
	// agents service. When nil (AGENTS_BASE_URL unset), create_quick_work_item
	// is not registered.
	QuickWorkItem tools.QuickWorkItemCreator
}

func jsonStringToObject(s string) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	return m, nil
}

func (d MCPServerDeps) budgetJSON() string {
	msg := fmt.Sprintf(
		"Repository read budget exceeded (%d calls or %d bytes). Summarize findings or ask the user to continue.",
		d.MaxRepoCalls,
		d.MaxRepoBytes,
	)
	b, _ := json.Marshal(map[string]any{
		"error":   "repo_budget_exceeded",
		"message": msg,
	})
	return string(b)
}

func (d MCPServerDeps) runRepoTool(ctx context.Context, req *mcpsdk.CallToolRequest, fn func(context.Context) (string, error)) (string, error) {
	if d.Budget != nil {
		rid := runIDFromRequest(req, d.APIKey)
		if err := d.Budget.Before(rid); err != nil {
			return d.budgetJSON(), nil
		}
		out, err := fn(ctx)
		if err != nil {
			return "", err
		}
		if err := d.Budget.After(rid, len(out)); err != nil {
			return d.budgetJSON(), nil
		}
		return out, nil
	}
	return fn(ctx)
}

// addRepoTool registers a repo tool that runs through budget tracking and returns JSON.
func addRepoTool[In any](s *mcpsdk.Server, deps MCPServerDeps, name, description string, fn func(context.Context, In) (string, error)) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        name,
		Description: description,
	}, func(ctx context.Context, req *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, map[string]any, error) {
		out, err := deps.runRepoTool(ctx, req, func(c context.Context) (string, error) {
			return fn(c, in)
		})
		if err != nil {
			return nil, nil, err
		}
		m, err := jsonStringToObject(out)
		if err != nil {
			return nil, nil, err
		}
		return nil, m, nil
	})
}

// addJSONTool registers a tool that returns a JSON string (no budget tracking).
func addJSONTool[In any](s *mcpsdk.Server, name, description string, fn func(context.Context, In) (string, error)) {
	mcpsdk.AddTool(s, &mcpsdk.Tool{
		Name:        name,
		Description: description,
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in In) (*mcpsdk.CallToolResult, map[string]any, error) {
		out, err := fn(ctx, in)
		if err != nil {
			return nil, nil, err
		}
		m, err := jsonStringToObject(out)
		if err != nil {
			return nil, nil, err
		}
		return nil, m, nil
	})
}

// feedbackNotify adapts a notification publisher into the feedback handler's
// notify callback. nil publisher → nil callback (the handler skips notification).
func feedbackNotify(events notifications.Publisher) func(changeRequestID, eventType string) {
	if events == nil {
		return nil
	}
	return func(changeRequestID, eventType string) {
		events.Publish(notifications.Event{
			Type: "feedback.recorded",
			Data: map[string]any{"change_request_id": changeRequestID, "event_type": eventType},
		})
	}
}

// NewMCPServer registers all tools; repo_* tools are omitted when deps.RepoProviders is empty.
func NewMCPServer(deps MCPServerDeps) *mcpsdk.Server {
	s := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "doc-registry-mcp", Version: "1.0.0"}, nil)

	// Resolve packSkills before governance construction — a typed-nil *skills.Service
	// satisfies the interface at compile time but panics at runtime.
	var packSkills ContextPackSkillReader
	if deps.Skills != nil {
		packSkills = deps.Skills
	}

	// agentsRunner combines the four separately-typed agents-service deps into the
	// single AgentsRunner interface that governanceops.Service expects.
	var agentsRunner governanceops.AgentsRunner
	if deps.Readiness != nil || deps.LLMGates != nil || deps.DeliveryReview != nil || deps.QuickWorkItem != nil {
		agentsRunner = &agentsAdapter{
			readiness:      deps.Readiness,
			llmGates:       deps.LLMGates,
			deliveryReview: deps.DeliveryReview,
			quickWorkItem:  deps.QuickWorkItem,
		}
	}

	// Governance service shared by all status/work-item tools, context-pack assembly,
	// and governance write operations (feedback, publish, draft, agents-backed ops).
	governance := &governanceops.Service{
		WorkBoard:   deps.WorkBoard,
		Trackers:    deps.TrackerLinks,
		AppBaseURL:  deps.AppBaseURL,
		Artifacts:   deps.Artifacts,
		Attachments: deps.Attachments,
		Skills:      packSkills,
		Knowledge:   deps.KnowledgeProvenance,
		// Write surfaces
		FeedbackStore:   deps.Feedback,
		FeedbackNotify:  feedbackNotify(deps.Events),
		ArtifactWriter:  deps.Artifacts,
		FeatureUpserter: deps.WorkBoard,
		ProfileResolver: deps.Profiles,
		DraftArtifacts:  deps.Artifacts,
		EditStore:       deps.ArtifactEdit,
		AgentsRunner:    agentsRunner,
	}

	knowledgeSearch := tools.NewKnowledgeSearchHandler(deps.Knowledge)
	artifactSearch := tools.NewArtifactSearchHandler(deps.Artifacts)
	artifactBundleRead := tools.NewArtifactBundleReadHandler(deps.Artifacts)
	artifactCreate := tools.NewArtifactCreateHandler(deps.Artifacts)
	resolveWorkItem := tools.NewResolveWorkItemHandler(governance)
	listWorkItems := tools.NewListWorkItemsHandler(governance)
	readDeliveryReview := tools.NewReadDeliveryReviewHandler(governance)
	readClarification := tools.NewReadClarificationHandler(governance)
	getWorkStatus := tools.NewGetWorkStatusHandler(governance)
	getGateHistory := tools.NewGetGateHistoryHandler(governance)

	if len(deps.RepoProviders) > 0 {
		repoTools := repo.NewToolHandlers(deps.RepoProviders, deps.RepoDefaultRefs)
		addRepoTool(s, deps, "repo_search",
			"Search code in the configured project by keyword. Returns file paths, line numbers, and snippets.",
			repoTools.RepoSearch)
		addRepoTool(s, deps, "repo_context_pack",
			"Return a compact cached repo context bundle for a query: README excerpt, top matches, and likely files.",
			repoTools.RepoContextPack)
		addRepoTool(s, deps, "repo_list_files",
			"List files and directories at a path in the repository.",
			repoTools.RepoListFiles)
		addRepoTool(s, deps, "repo_list_symbols",
			"List function/class/type symbols in a file without returning their bodies.",
			repoTools.RepoListSymbols)
		addRepoTool(s, deps, "repo_get_symbol",
			"Get the full body of a specific symbol (function, type, etc.) from a file.",
			repoTools.RepoGetSymbol)
		addRepoTool(s, deps, "repo_get_snippet",
			"Get lines around a specific location in a file. Supports files up to 10 MB.",
			repoTools.RepoGetSnippet)
		addRepoTool(s, deps, "repo_related_tests",
			"Find test files related to a source file using naming conventions.",
			repoTools.RepoRelatedTests)
		addRepoTool(s, deps, "repo_read_file",
			"Read a small file (docs, configs, specs). Not for source code. Capped at 16KB.",
			repoTools.RepoReadFile)
	}

	// toolCount tracks every non-repo tool registered on this server so that
	// specgate_whoami can report the live count. Increment toolCount alongside
	// each addJSONTool call; repo_* tools are excluded because they are
	// gated on a separate budget and are not always present.
	var toolCount int

	addJSONTool(s, "search_knowledge",
		"Search indexed Governance Knowledge documents for business rules, SRS, and supporting docs.",
		knowledgeSearch)
	toolCount++
	addJSONTool(s, "search_artifacts",
		"Search published planning artifacts by feature, status, or service.",
		artifactSearch)
	toolCount++
	addJSONTool(s, "artifact_read_bundle",
		"Read selected markdown files from an artifact bundle by artifact_id.",
		artifactBundleRead)
	toolCount++
	addJSONTool(s, "artifact_create",
		"Publish a new planning artifact bundle (same as POST /artifacts). Body matches the REST request body.",
		artifactCreate)
	toolCount++
	if deps.WorkBoard != nil {
		addJSONTool(s, "resolve_work_item",
			"Resolve a tracker issue (provider + issue key or URL) to its SpecGate work item and Context Pack URI.",
			resolveWorkItem)
		toolCount++
		addJSONTool(s, "list_work_items",
			"List ready or handed-off work items with their Context Pack URIs.",
			listWorkItems)
		toolCount++
		addJSONTool(s, "read_delivery_review",
			"Read the latest persisted delivery-review verdict for one work item, including per-criterion detail and outstanding feedback.",
			readDeliveryReview)
		toolCount++
		addJSONTool(s, "get_work_status",
			"Get a compact status snapshot for one work item: gate summary, AC progress, latest delivery review verdict, and pending human actions with web UI links. "+
				"Input: {change_request_id}. Call at session start when resuming in-flight work.",
			getWorkStatus)
		toolCount++
		addJSONTool(s, "get_gate_history",
			"Get the gate run history for a work item, optionally filtered to a specific gate. "+
				"Input: {change_request_id, gate?, limit?}. Useful for debugging gate regressions.",
			getGateHistory)
		toolCount++
		addJSONTool(s, "get_governance_status",
			"Return an aggregate governance snapshot: phase counts, stale warnings, and work items needing attention. "+
				"No input required. Call once at session start to orient before picking up a work item.",
			tools.NewGetGovernanceStatusHandler(governance))
		toolCount++
	}
	if deps.Feedback != nil {
		addJSONTool(s, "read_clarification",
			"Read human clarification outcomes for coding-agent blocked-ambiguity feedback on one work item.",
			readClarification)
		toolCount++
	}
	if deps.ArtifactEdit != nil {
		addJSONTool(s, "draft_artifact_update",
			"Open a draft-only artifact update proposal for a coding agent. Creates a sourced artifact-edit session and never auto-applies it.",
			tools.NewDraftArtifactUpdateHandler(governance))
		toolCount++
	}
	if deps.Feedback != nil {
		addJSONTool(s, "report_implementation_feedback",
			"Report coding-agent implementation feedback for a SpecGate work item. Use for blocking ambiguity, completion evidence, and docs-updated evidence. "+
				"On `coding_agent.completed`, include `checks` (automated results: {name: tests|types|lint|build, status: pass|fail|skipped, detail?}) and `criteria` "+
				"(per acceptance criterion: {criterion_id? correlating to the work item's AC id, text?, claim: satisfied|partial|not_done, evidence?}) so SpecGate can verify the work against the acceptance criteria. Both optional but strongly recommended on completion.",
			tools.NewReportImplementationFeedbackHandler(governance))
		toolCount++
	}

	if deps.WorkBoard != nil {
		addJSONTool(s, "specgate_publish",
			"Publish a spec package to SpecGate by stable feature_key (create-or-reference the feature, auto-version, lineage). Lands a draft for human review.",
			tools.NewSpecgatePublishHandler(governance))
		toolCount++
		if deps.Profiles != nil {
			addJSONTool(s, "specgate_list_profiles",
				"List built-in and imported SpecGate governance profiles available to IDE plugins.",
				tools.NewSpecgateListProfilesHandler(deps.Profiles))
			toolCount++
			addJSONTool(s, "specgate_import_profiles",
				"Import immutable team governance profiles into SpecGate for later publish-by-key use.",
				tools.NewSpecgateImportProfilesHandler(deps.Profiles))
			toolCount++
		}
		if deps.Readiness != nil {
			addJSONTool(s, "specgate_check_readiness",
				"Run readiness gates for an artifact and return covered-vs-missing topics + gate verdict — check in-IDE before handoff. "+
					"Input: {artifact_id}. Returns the per-gate readiness runs plus a derived aggregate (fail > needs_human_review > warn > pass > not_run).",
				tools.NewSpecgateCheckReadinessHandler(governance))
			toolCount++
		}
		if deps.LLMGates != nil {
			addJSONTool(s, "run_llm_gates",
				"Run all LLM quality gates for a change request's lead artifact and post verdicts to Doc Registry. "+
					"Input: {change_request_id}. Returns gate verdicts and persists them for later readback. "+
					"Poll specgate_check_readiness or read_delivery_review for the persisted result.",
				tools.NewRunLLMGatesHandler(governance))
			toolCount++
		}
		if deps.DeliveryReview != nil {
			addJSONTool(s, "trigger_delivery_review",
				"Trigger the delivery review for a change request. "+
					"Input: {change_request_id}. Judges the latest coding-agent completion against the CR's acceptance criteria and persists the verdict. "+
					"Follow with read_delivery_review to get the verdict.",
				tools.NewTriggerDeliveryReviewHandler(governance))
			toolCount++
		}
		if deps.QuickWorkItem != nil {
			addJSONTool(s, "create_quick_work_item",
				"Create a quick-route change request from tracker issue content (bugfix or small CR). "+
					"Input: {title, description, issue_url?, issue_key?, feature_key?, feature_name?}. "+
					"LLM auto-drafts acceptance criteria from the issue description. "+
					"Returns: {change_request_id, change_request_key, feature_id, context_pack_uri, acceptance_criteria, phase}. "+
					"After creation, read the context pack via the returned context_pack_uri and proceed with implementation.",
				tools.NewCreateQuickWorkItemHandler(governance))
			toolCount++
		}
	}

	// specgate_whoami is always registered last so toolCount reflects every
	// tool above. Pass toolCount+1 so the reported count includes whoami itself.
	addJSONTool(s, "specgate_whoami",
		"Health/identity check — confirm the IDE is connected to SpecGate and how many tools/resources are available.",
		tools.NewSpecgateWhoamiHandler(toolCount+1, 3, "1.0.0"))

	RegisterSkillResources(s, deps.Skills)
	RegisterContextPackResources(s, governance)

	return s
}

// agentsAdapter adapts four separately-typed agents-service deps into the single
// governanceops.AgentsRunner interface. Fields may be nil — the service guards
// each operation and returns a clear error when the dep is absent.
type agentsAdapter struct {
	readiness      tools.ReadinessRunner
	llmGates       tools.LLMGatesRunner
	deliveryReview tools.DeliveryReviewTrigger
	quickWorkItem  tools.QuickWorkItemCreator
}

func (a *agentsAdapter) RunReadiness(ctx context.Context, artifactID string) (*agentsclient.Verdict, error) {
	if a.readiness == nil {
		return nil, fmt.Errorf("readiness unavailable: agents service not configured")
	}
	return a.readiness.RunReadiness(ctx, artifactID)
}

func (a *agentsAdapter) RunLLMGates(ctx context.Context, changeRequestID string) (map[string]any, error) {
	if a.llmGates == nil {
		return nil, fmt.Errorf("run-llm-gates unavailable: agents service not configured")
	}
	return a.llmGates.RunLLMGates(ctx, changeRequestID)
}

func (a *agentsAdapter) ReviewDelivery(ctx context.Context, changeRequestID string) (map[string]any, error) {
	if a.deliveryReview == nil {
		return nil, fmt.Errorf("trigger-delivery-review unavailable: agents service not configured")
	}
	return a.deliveryReview.ReviewDelivery(ctx, changeRequestID)
}

func (a *agentsAdapter) CreateQuickWorkItem(ctx context.Context, title, description, issueURL, issueKey, featureKey, featureName string, acceptanceCriteria []string, createdBy string, workspaceID string) (map[string]any, error) {
	if a.quickWorkItem == nil {
		return nil, fmt.Errorf("create-quick-work-item unavailable: agents service not configured")
	}
	return a.quickWorkItem.CreateQuickWorkItem(ctx, title, description, issueURL, issueKey, featureKey, featureName, acceptanceCriteria, createdBy, workspaceID)
}

// SettingsProvider is the subset of settings.Service needed for dynamic MCP.
type SettingsProvider interface {
	Get(key string) string
	GetBool(key string) bool
	GetInt(key string, def int) int
	ConfigHash() string
}

// GitLabRepoConfig is one repo-reading provider derived from a GitLab
// integration + one of its project resources. It mirrors
// integrations.GitLabRepoConfig but lives in the mcp package so this package
// never imports integrations (avoids an import cycle); the api wiring layer
// adapts between the two.
type GitLabRepoConfig struct {
	ProjectID  string
	APIURL     string
	Token      string
	DefaultRef string
	// Bearer is true when Token is an OAuth access token (Authorization: Bearer)
	// rather than a PAT (PRIVATE-TOKEN).
	Bearer bool
}

// IntegrationRepoSource exposes GitLab repo providers derived from connected
// GitLab integrations (the unified source that also backs the tracker),
// alongside a hash so the dynamic MCP handler can rebuild when integrations
// change. Implemented by an adapter over *integrations.Service in the api
// package — defined here so mcp stays free of an integrations import.
type IntegrationRepoSource interface {
	GitLabRepoConfigs(ctx context.Context) ([]GitLabRepoConfig, error)
	IntegrationsHash() string
}

// MCPHandlerOptions configures a dynamic MCP HTTP handler.
type MCPHandlerOptions struct {
	Settings            SettingsProvider
	IntegrationRepos    IntegrationRepoSource
	Knowledge           tools.KnowledgeSearcher
	Artifacts           artifact.Service
	ArtifactEdit        artifactedit.Store
	WorkBoard           workboard.Store
	Skills              *skills.Service
	Feedback            tools.FeedbackStore
	TrackerLinks        tools.WorkItemTrackerReader
	Profiles            *governanceprofile.Service
	Attachments         ContextPackAttachmentReader
	KnowledgeProvenance ContextPackKnowledgeReader
	Events              notifications.Publisher
	AppBaseURL          string
	Readiness           tools.ReadinessRunner
	LLMGates            tools.LLMGatesRunner
	DeliveryReview      tools.DeliveryReviewTrigger
	QuickWorkItem       tools.QuickWorkItemCreator
}

// NewDynamicMCPHandler creates an HTTP handler that reads MCP configuration from
// a SettingsProvider on each request. It rebuilds the MCP stack only when the
// settings or integration-repo hash changes.
func NewDynamicMCPHandler(opts MCPHandlerOptions) http.Handler {
	sp := opts.Settings
	integrationRepos := opts.IntegrationRepos
	knowledge := opts.Knowledge
	artifacts := opts.Artifacts
	artifactEdit := opts.ArtifactEdit
	workBoard := opts.WorkBoard
	skillsSvc := opts.Skills
	feedback := opts.Feedback
	trackerLinks := opts.TrackerLinks
	profiles := opts.Profiles
	attachments := opts.Attachments
	knowledgeProvenance := opts.KnowledgeProvenance
	events := opts.Events
	appBaseURL := opts.AppBaseURL
	readiness := opts.Readiness
	llmGates := opts.LLMGates
	deliveryReview := opts.DeliveryReview
	quickWorkItem := opts.QuickWorkItem
	var (
		mu       sync.Mutex
		lastHash string
		inner    http.Handler
	)

	rebuild := func(ctx context.Context) http.Handler {
		mu.Lock()
		defer mu.Unlock()

		hash := sp.ConfigHash()
		if integrationRepos != nil {
			hash += "|" + integrationRepos.IntegrationsHash()
		}
		if inner != nil && hash == lastHash {
			return inner
		}

		repoProviders, defaultRefs := buildGitLabRepoProviders(ctx, integrationRepos)
		maxCalls := sp.GetInt("mcp.budget_max_repo_calls", 50)
		maxBytes := sp.GetInt("mcp.budget_max_bytes_returned", 524288)
		budget := NewBudgetTracker(maxCalls, maxBytes, 2*time.Hour)

		deps := MCPServerDeps{
			RepoProviders:       repoProviders,
			RepoDefaultRefs:     defaultRefs,
			Knowledge:           knowledge,
			Artifacts:           artifacts,
			ArtifactEdit:        artifactEdit,
			WorkBoard:           workBoard,
			Skills:              skillsSvc,
			Feedback:            feedback,
			TrackerLinks:        trackerLinks,
			Profiles:            profiles,
			Attachments:         attachments,
			KnowledgeProvenance: knowledgeProvenance,
			Events:              events,
			APIKey:              ResolveAPIKey(sp),
			Budget:              budget,
			MaxRepoCalls:        maxCalls,
			MaxRepoBytes:        maxBytes,
			AppBaseURL:          appBaseURL,
			Readiness:           readiness,
			LLMGates:            llmGates,
			DeliveryReview:      deliveryReview,
			QuickWorkItem:       quickWorkItem,
		}
		srv := NewMCPServer(deps)
		h := mcpsdk.NewStreamableHTTPHandler(func(_ *http.Request) *mcpsdk.Server {
			return srv
		}, nil)
		inner = h
		lastHash = hash
		return inner
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiKey := ResolveAPIKey(sp)
		if apiKey == "" {
			http.Error(w, "MCP not configured", http.StatusServiceUnavailable)
			return
		}
		token := []byte(parseBearerToken(r.Header.Get("Authorization")))
		if subtle.ConstantTimeCompare(token, []byte(apiKey)) != 1 {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		handler := rebuild(r.Context())
		handler.ServeHTTP(w, r)
	})
}

// buildGitLabRepoProviders builds the repo-keyed provider map solely from
// connected GitLab integrations — the unified source that also backs the
// tracker. integrationRepos may be nil (yields an empty map).
func buildGitLabRepoProviders(ctx context.Context, integrationRepos IntegrationRepoSource) (map[string]repo.RepoProvider, map[string]string) {
	providers := map[string]repo.RepoProvider{}
	defaultRefs := map[string]string{}

	if integrationRepos != nil {
		if configs, err := integrationRepos.GitLabRepoConfigs(ctx); err == nil {
			for _, cfg := range configs {
				repoID := strings.TrimSpace(cfg.ProjectID)
				token := strings.TrimSpace(cfg.Token)
				apiURL := normalizeGitLabAPIURL(cfg.APIURL)
				if repoID == "" || token == "" || apiURL == "" {
					continue
				}
				providers[repoID] = repo.NewGitLabProvider(repo.GitLabConfig{
					APIURL:     apiURL,
					Token:      token,
					ProjectID:  repoID,
					DefaultRef: cfg.DefaultRef,
					Bearer:     cfg.Bearer,
				})
				defaultRefs[repoID] = cfg.DefaultRef
			}
		}
	}

	return providers, defaultRefs
}
