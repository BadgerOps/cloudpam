package api

import (
	"bytes"
	"fmt"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"cloudpam/internal/auth"
	"cloudpam/internal/domain"
	"cloudpam/internal/planning"
)

type openAPIRoute struct {
	Method              string
	Path                string
	Summary             string
	Description         string
	Tag                 string
	Security            bool
	Parameters          []openAPIParameter
	RequestSchema       string
	RequestDescription  string
	SuccessStatus       string
	ResponseSchema      string
	ResponseDescription string
	ResponseContentType string
}

type openAPIParameter struct {
	Name        string
	In          string
	Description string
	Required    bool
	Type        string
	Format      string
}

type openAPISchema struct {
	Ref                  string
	Type                 string
	Format               string
	Items                *openAPISchema
	AdditionalProperties *openAPISchema
	Properties           map[string]*openAPISchema
	Required             []string
}

type openAPISetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
}

type openAPISetupResponse struct {
	Message  string `json:"message"`
	Username string `json:"username"`
}

type openAPILoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type openAPILoginResponse struct {
	User        *auth.User `json:"user"`
	ExpiresAt   time.Time  `json:"expires_at"`
	Permissions []string   `json:"permissions"`
}

type openAPIMeResponse struct {
	AuthType         string     `json:"auth_type"`
	Role             auth.Role  `json:"role"`
	User             *auth.User `json:"user,omitempty"`
	KeyID            string     `json:"key_id,omitempty"`
	KeyName          string     `json:"key_name,omitempty"`
	AuthProvider     string     `json:"auth_provider,omitempty"`
	SessionExpiresAt string     `json:"session_expires_at,omitempty"`
	Permissions      []string   `json:"permissions,omitempty"`
}

type openAPIUserListResponse struct {
	Users []*auth.User `json:"users"`
}

type openAPICreateUserRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Role        string `json:"role,omitempty"`
	Password    string `json:"password"`
}

type openAPIUpdateUserRequest struct {
	Email       *string `json:"email,omitempty"`
	DisplayName *string `json:"display_name,omitempty"`
	Role        *string `json:"role,omitempty"`
	IsActive    *bool   `json:"is_active,omitempty"`
}

type openAPIChangePasswordRequest struct {
	CurrentPassword string `json:"current_password,omitempty"`
	NewPassword     string `json:"new_password"`
}

type openAPIStatusResponse struct {
	Status string `json:"status"`
}

type openAPIKeyCreateRequest struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type openAPIKeyCreateResponse struct {
	Key    string       `json:"key"`
	APIKey *auth.APIKey `json:"api_key"`
}

type openAPIAPIKeysResponse struct {
	Keys []auth.APIKey `json:"keys"`
}

type openAPIRoleRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Permissions []string `json:"permissions"`
}

type openAPIRoleListResponse struct {
	Roles []auth.RoleDefinition `json:"roles"`
}

type openAPIPermissionsResponse struct {
	Permissions []auth.Permission `json:"permissions"`
}

type openAPIAuditListResponse struct {
	Events []any `json:"events"`
	Total  int   `json:"total"`
	Limit  int   `json:"limit"`
	Offset int   `json:"offset"`
}

type openAPIImportResponse struct {
	Imported int      `json:"imported"`
	Skipped  int      `json:"skipped"`
	Errors   []string `json:"errors,omitempty"`
}

type openAPISchemaPlanRequest struct {
	Pools []domain.CreatePool `json:"pools"`
}

type openAPISearchResponse struct {
	Query   string `json:"query"`
	Results []any  `json:"results"`
}

type openAPILinkResourceRequest struct {
	PoolID int64 `json:"pool_id"`
}

type openAPIDiscoveryImportRequest struct {
	AccountID int64 `json:"account_id"`
}

type openAPIDiscoveryImportResponse struct {
	AccountsImported int      `json:"accounts_imported"`
	PoolsCreated     int      `json:"pools_created"`
	ResourcesLinked  int      `json:"resources_linked"`
	Skipped          int      `json:"skipped"`
	Errors           []string `json:"errors"`
}

type openAPISyncRequest struct {
	AccountID int64  `json:"account_id,omitempty"`
	AgentID   string `json:"agent_id,omitempty"`
	AllAgents bool   `json:"all_agents,omitempty"`
}

type openAPIOIDCProvidersResponse struct {
	Providers []domain.OIDCProvider `json:"providers"`
}

type openAPIPublicOIDCProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type openAPIPublicOIDCProvidersResponse struct {
	Providers []openAPIPublicOIDCProvider `json:"providers"`
}

type openAPIOIDCProviderTestResponse struct {
	Success   bool   `json:"success"`
	IssuerURL string `json:"issuer_url"`
	Message   string `json:"message,omitempty"`
}

type openAPIOIDCRefreshResponse struct {
	RedirectURL string `json:"redirect_url"`
}

type openAPIConversationListResponse struct {
	Items []domain.Conversation `json:"items"`
	Total int                   `json:"total"`
}

type openAPICreateConversationRequest struct {
	Title string `json:"title,omitempty"`
}

type openAPIApplyPlanResponse struct {
	Created    int              `json:"created"`
	Skipped    int              `json:"skipped"`
	Errors     []string         `json:"errors"`
	RootPoolID int64            `json:"root_pool_id"`
	PoolMap    map[string]int64 `json:"pool_map"`
}

type openAPIUpdateCheckResponse struct {
	CurrentVersion   string `json:"current_version,omitempty"`
	LatestVersion    string `json:"latest_version,omitempty"`
	UpdateAvailable  bool   `json:"update_available"`
	ReleaseNotes     string `json:"release_notes,omitempty"`
	ReleaseURL       string `json:"release_url,omitempty"`
	PublishedAt      string `json:"published_at,omitempty"`
	CheckedAt        string `json:"checked_at"`
	UpgradeSupported bool   `json:"upgrade_supported"`
	Warning          string `json:"warning,omitempty"`
	Error            string `json:"error,omitempty"`
}

type openAPIUpgradeRequestResponse struct {
	Status        string `json:"status"`
	UpgradeID     string `json:"upgrade_id"`
	TargetVersion string `json:"target_version"`
	Message       string `json:"message"`
}

type openAPIUpgradeStatusResponse struct {
	Status                 string         `json:"status"`
	Supported              bool           `json:"supported"`
	UpgradeID              string         `json:"upgrade_id,omitempty"`
	Acknowledged           bool           `json:"acknowledged,omitempty"`
	CompletedStatusExpired bool           `json:"completed_status_expired,omitempty"`
	LastUpgrade            map[string]any `json:"last_upgrade,omitempty"`
}

type openAPIUpgradeStatusAckResponse struct {
	Status       string         `json:"status"`
	Supported    bool           `json:"supported"`
	UpgradeID    string         `json:"upgrade_id,omitempty"`
	Acknowledged bool           `json:"acknowledged,omitempty"`
	LastUpgrade  map[string]any `json:"last_upgrade,omitempty"`
}

type openAPISystemInfoResponse struct {
	Version             string `json:"version"`
	AuthEnabled         bool   `json:"auth_enabled"`
	LocalAuthEnabled    bool   `json:"local_auth_enabled"`
	NeedsSetup          bool   `json:"needs_setup"`
	ReleaseURL          string `json:"release_url"`
	ChangelogPath       string `json:"changelog_path"`
	InAppUpgradeEnabled bool   `json:"in_app_upgrade_enabled"`
	UpgradeMode         string `json:"upgrade_mode"`
}

type openAPIHealthResponse struct {
	Status           string `json:"status"`
	AuthEnabled      bool   `json:"auth_enabled"`
	LocalAuthEnabled bool   `json:"local_auth_enabled"`
	NeedsSetup       bool   `json:"needs_setup"`
	Version          string `json:"version"`
}

func (s *Server) handleOpenAPIRoute(pattern string, h http.Handler) {
	s.mux.Handle(pattern, h)
	s.recordOpenAPIPattern(pattern)
}

func (s *Server) handleOpenAPIRouteFunc(pattern string, h func(http.ResponseWriter, *http.Request)) {
	s.mux.HandleFunc(pattern, h)
	s.recordOpenAPIPattern(pattern)
}

func (s *Server) recordOpenAPIPattern(pattern string) {
	for _, route := range openAPIRoutesForPattern(pattern) {
		if route.Path == "/" {
			continue
		}
		key := route.Method + " " + route.Path
		if s.openAPIRouteKeys == nil {
			s.openAPIRouteKeys = make(map[string]bool)
		}
		if s.openAPIRouteKeys[key] {
			continue
		}
		s.openAPIRouteKeys[key] = true
		s.openAPIRoutes = append(s.openAPIRoutes, route)
	}
}

func (s *Server) openAPISpecYAML() []byte {
	routes := append([]openAPIRoute(nil), s.openAPIRoutes...)
	sort.Slice(routes, func(i, j int) bool {
		if routes[i].Path == routes[j].Path {
			return methodOrder(routes[i].Method) < methodOrder(routes[j].Method)
		}
		return routes[i].Path < routes[j].Path
	})

	var b bytes.Buffer
	b.WriteString("openapi: 3.1.0\n")
	b.WriteString("info:\n")
	b.WriteString("  title: CloudPAM API\n")
	fmt.Fprintf(&b, "  version: %s\n", yamlQuote(cleanVersion(s.appVersion)))
	b.WriteString("  description: |\n")
	b.WriteString("    Runtime-generated REST API contract for the routes registered by this CloudPAM server.\n")
	b.WriteString("    /openapi.yaml is the machine-readable contract and /openapi is the interactive API reference.\n")
	b.WriteString("servers:\n")
	b.WriteString("  - url: http://localhost:8080\n")
	b.WriteString("    description: Local development server\n")
	b.WriteString("security:\n")
	b.WriteString("  - BearerAuth: []\n")
	writeOpenAPITags(&b, routes)
	b.WriteString("paths:\n")
	if len(routes) == 0 {
		b.WriteString("  {}\n")
	} else {
		writeOpenAPIPaths(&b, routes)
	}
	b.WriteString("components:\n")
	b.WriteString("  securitySchemes:\n")
	b.WriteString("    BearerAuth:\n")
	b.WriteString("      type: http\n")
	b.WriteString("      scheme: bearer\n")
	b.WriteString("      bearerFormat: API key\n")
	b.WriteString("    CookieAuth:\n")
	b.WriteString("      type: apiKey\n")
	b.WriteString("      in: cookie\n")
	b.WriteString("      name: session\n")
	b.WriteString("  responses:\n")
	b.WriteString("    Error:\n")
	b.WriteString("      description: Error response\n")
	b.WriteString("      content:\n")
	b.WriteString("        application/json:\n")
	b.WriteString("          schema:\n")
	b.WriteString("            $ref: '#/components/schemas/Error'\n")
	b.WriteString("  schemas:\n")
	writeOpenAPIComponentSchemas(&b)
	return b.Bytes()
}

func writeOpenAPITags(b *bytes.Buffer, routes []openAPIRoute) {
	seen := map[string]bool{}
	var tags []string
	for _, route := range routes {
		if route.Tag != "" && !seen[route.Tag] {
			seen[route.Tag] = true
			tags = append(tags, route.Tag)
		}
	}
	sort.Strings(tags)
	b.WriteString("tags:\n")
	for _, tag := range tags {
		fmt.Fprintf(b, "  - name: %s\n", yamlQuote(tag))
	}
}

func writeOpenAPIPaths(b *bytes.Buffer, routes []openAPIRoute) {
	grouped := map[string][]openAPIRoute{}
	var paths []string
	for _, route := range routes {
		if _, ok := grouped[route.Path]; !ok {
			paths = append(paths, route.Path)
		}
		grouped[route.Path] = append(grouped[route.Path], route)
	}
	sort.Strings(paths)
	for _, path := range paths {
		fmt.Fprintf(b, "  %s:\n", yamlQuote(path))
		for _, route := range grouped[path] {
			writeOpenAPIOperation(b, route)
		}
	}
}

func writeOpenAPIOperation(b *bytes.Buffer, route openAPIRoute) {
	method := strings.ToLower(route.Method)
	fmt.Fprintf(b, "    %s:\n", method)
	fmt.Fprintf(b, "      summary: %s\n", yamlQuote(route.Summary))
	if route.Description != "" {
		fmt.Fprintf(b, "      description: %s\n", yamlQuote(route.Description))
	}
	fmt.Fprintf(b, "      tags: [%s]\n", yamlQuote(route.Tag))
	if !route.Security {
		b.WriteString("      security: []\n")
	}
	if len(route.Parameters) > 0 {
		b.WriteString("      parameters:\n")
		for _, param := range route.Parameters {
			fmt.Fprintf(b, "        - name: %s\n", yamlQuote(param.Name))
			fmt.Fprintf(b, "          in: %s\n", param.In)
			if param.Description != "" {
				fmt.Fprintf(b, "          description: %s\n", yamlQuote(param.Description))
			}
			if param.Required {
				b.WriteString("          required: true\n")
			}
			b.WriteString("          schema:\n")
			fmt.Fprintf(b, "            type: %s\n", param.Type)
			if param.Format != "" {
				fmt.Fprintf(b, "            format: %s\n", param.Format)
			}
		}
	}
	if route.RequestSchema != "" {
		description := route.RequestDescription
		if description == "" {
			description = "Request body"
		}
		b.WriteString("      requestBody:\n")
		b.WriteString("        required: true\n")
		fmt.Fprintf(b, "        description: %s\n", yamlQuote(description))
		b.WriteString("        content:\n")
		b.WriteString("          application/json:\n")
		b.WriteString("            schema:\n")
		fmt.Fprintf(b, "              $ref: '#/components/schemas/%s'\n", route.RequestSchema)
	}
	b.WriteString("      responses:\n")
	status := route.SuccessStatus
	if status == "" {
		status = successStatus(route.Method)
	}
	fmt.Fprintf(b, "        \"%s\":\n", status)
	description := route.ResponseDescription
	if description == "" {
		description = "Successful response"
	}
	fmt.Fprintf(b, "          description: %s\n", yamlQuote(description))
	if route.ResponseSchema != "" && status != "204" {
		contentType := route.ResponseContentType
		if contentType == "" {
			contentType = "application/json"
		}
		b.WriteString("          content:\n")
		fmt.Fprintf(b, "            %s:\n", contentType)
		b.WriteString("              schema:\n")
		if route.ResponseSchema == "String" {
			b.WriteString("                type: string\n")
		} else {
			fmt.Fprintf(b, "                $ref: '#/components/schemas/%s'\n", route.ResponseSchema)
		}
	}
	b.WriteString("        default:\n")
	b.WriteString("          $ref: '#/components/responses/Error'\n")
}

func writeOpenAPIComponentSchemas(b *bytes.Buffer) {
	for _, def := range openAPIComponentTypes() {
		fmt.Fprintf(b, "    %s:\n", def.name)
		writeSchema(b, schemaFromType(def.typ, def.name), 6)
	}
}

type openAPIComponentType struct {
	name string
	typ  reflect.Type
}

func openAPIComponentTypes() []openAPIComponentType {
	types := []openAPIComponentType{
		{"Error", reflect.TypeOf(apiError{})},
		{"Object", reflect.TypeOf(map[string]any{})},
		{"HealthResponse", reflect.TypeOf(openAPIHealthResponse{})},
		{"ReadinessResponse", reflect.TypeOf(ReadinessResponse{})},
		{"SystemInfoResponse", reflect.TypeOf(openAPISystemInfoResponse{})},
		{"SetupRequest", reflect.TypeOf(openAPISetupRequest{})},
		{"SetupResponse", reflect.TypeOf(openAPISetupResponse{})},
		{"Pool", reflect.TypeOf(domain.Pool{})},
		{"PoolStats", reflect.TypeOf(domain.PoolStats{})},
		{"PoolWithStats", reflect.TypeOf(domain.PoolWithStats{})},
		{"CreatePool", reflect.TypeOf(domain.CreatePool{})},
		{"UpdatePool", reflect.TypeOf(domain.UpdatePool{})},
		{"Account", reflect.TypeOf(domain.Account{})},
		{"CreateAccount", reflect.TypeOf(domain.CreateAccount{})},
		{"UpdateAccount", reflect.TypeOf(domain.CreateAccount{})},
		{"ImportResponse", reflect.TypeOf(openAPIImportResponse{})},
		{"SchemaPlanRequest", reflect.TypeOf(openAPISchemaPlanRequest{})},
		{"SearchResponse", reflect.TypeOf(openAPISearchResponse{})},
		{"DiscoveredResource", reflect.TypeOf(domain.DiscoveredResource{})},
		{"DiscoveryResourcesResponse", reflect.TypeOf(domain.DiscoveryResourcesResponse{})},
		{"DiscoveryImportRequest", reflect.TypeOf(openAPIDiscoveryImportRequest{})},
		{"DiscoveryImportResponse", reflect.TypeOf(openAPIDiscoveryImportResponse{})},
		{"LinkResourceRequest", reflect.TypeOf(openAPILinkResourceRequest{})},
		{"SyncRequest", reflect.TypeOf(openAPISyncRequest{})},
		{"SyncJob", reflect.TypeOf(domain.SyncJob{})},
		{"SyncJobsResponse", reflect.TypeOf(domain.SyncJobsResponse{})},
		{"IngestRequest", reflect.TypeOf(domain.IngestRequest{})},
		{"IngestResponse", reflect.TypeOf(domain.IngestResponse{})},
		{"BulkIngestRequest", reflect.TypeOf(domain.BulkIngestRequest{})},
		{"BulkIngestResponse", reflect.TypeOf(domain.BulkIngestResponse{})},
		{"DiscoveryAgent", reflect.TypeOf(domain.DiscoveryAgent{})},
		{"DiscoveryAgentsResponse", reflect.TypeOf(domain.DiscoveryAgentsResponse{})},
		{"AgentProvisionRequest", reflect.TypeOf(domain.AgentProvisionRequest{})},
		{"AgentProvisionResponse", reflect.TypeOf(domain.AgentProvisionResponse{})},
		{"AgentRegisterRequest", reflect.TypeOf(domain.AgentRegisterRequest{})},
		{"AgentRegisterResponse", reflect.TypeOf(domain.AgentRegisterResponse{})},
		{"AgentHeartbeatRequest", reflect.TypeOf(domain.AgentHeartbeatRequest{})},
		{"AgentHeartbeatResponse", reflect.TypeOf(domain.AgentHeartbeatResponse{})},
		{"NetworkConflict", reflect.TypeOf(domain.NetworkConflict{})},
		{"NetworkConflictListResponse", reflect.TypeOf(domain.NetworkConflictListResponse{})},
		{"ResolveNetworkConflictRequest", reflect.TypeOf(domain.ResolveNetworkConflictRequest{})},
		{"NetworkConflictLinkActionRequest", reflect.TypeOf(domain.NetworkConflictLinkActionRequest{})},
		{"NetworkConflictImportActionRequest", reflect.TypeOf(domain.NetworkConflictImportActionRequest{})},
		{"NetworkConflictActionResponse", reflect.TypeOf(domain.NetworkConflictActionResponse{})},
		{"AnalysisRequest", reflect.TypeOf(planning.AnalysisRequest{})},
		{"GapAnalysisRequest", reflect.TypeOf(planning.GapAnalysisRequest{})},
		{"GenerateRecommendationsRequest", reflect.TypeOf(domain.GenerateRecommendationsRequest{})},
		{"GenerateRecommendationsResponse", reflect.TypeOf(domain.GenerateRecommendationsResponse{})},
		{"RecommendationsListResponse", reflect.TypeOf(domain.RecommendationsListResponse{})},
		{"Recommendation", reflect.TypeOf(domain.Recommendation{})},
		{"ApplyRecommendationRequest", reflect.TypeOf(domain.ApplyRecommendationRequest{})},
		{"DismissRecommendationRequest", reflect.TypeOf(domain.DismissRecommendationRequest{})},
		{"RunDriftDetectionRequest", reflect.TypeOf(domain.RunDriftDetectionRequest{})},
		{"RunDriftDetectionResponse", reflect.TypeOf(domain.RunDriftDetectionResponse{})},
		{"DriftListResponse", reflect.TypeOf(domain.DriftListResponse{})},
		{"DriftItem", reflect.TypeOf(domain.DriftItem{})},
		{"IgnoreDriftRequest", reflect.TypeOf(domain.IgnoreDriftRequest{})},
		{"SecuritySettings", reflect.TypeOf(domain.SecuritySettings{})},
		{"NetworkSchemaPolicy", reflect.TypeOf(domain.NetworkSchemaPolicy{})},
		{"LoginRequest", reflect.TypeOf(openAPILoginRequest{})},
		{"LoginResponse", reflect.TypeOf(openAPILoginResponse{})},
		{"MeResponse", reflect.TypeOf(openAPIMeResponse{})},
		{"User", reflect.TypeOf(auth.User{})},
		{"UserListResponse", reflect.TypeOf(openAPIUserListResponse{})},
		{"CreateUserRequest", reflect.TypeOf(openAPICreateUserRequest{})},
		{"UpdateUserRequest", reflect.TypeOf(openAPIUpdateUserRequest{})},
		{"ChangePasswordRequest", reflect.TypeOf(openAPIChangePasswordRequest{})},
		{"StatusResponse", reflect.TypeOf(openAPIStatusResponse{})},
		{"APIKey", reflect.TypeOf(auth.APIKey{})},
		{"APIKeyCreateRequest", reflect.TypeOf(openAPIKeyCreateRequest{})},
		{"APIKeyCreateResponse", reflect.TypeOf(openAPIKeyCreateResponse{})},
		{"APIKeysResponse", reflect.TypeOf(openAPIAPIKeysResponse{})},
		{"Permission", reflect.TypeOf(auth.Permission{})},
		{"PermissionsResponse", reflect.TypeOf(openAPIPermissionsResponse{})},
		{"Role", reflect.TypeOf(auth.RoleDefinition{})},
		{"RoleRequest", reflect.TypeOf(openAPIRoleRequest{})},
		{"RoleListResponse", reflect.TypeOf(openAPIRoleListResponse{})},
		{"AuditListResponse", reflect.TypeOf(openAPIAuditListResponse{})},
		{"OIDCProvider", reflect.TypeOf(domain.OIDCProvider{})},
		{"OIDCProvidersResponse", reflect.TypeOf(openAPIOIDCProvidersResponse{})},
		{"PublicOIDCProvidersResponse", reflect.TypeOf(openAPIPublicOIDCProvidersResponse{})},
		{"OIDCProviderTestResponse", reflect.TypeOf(openAPIOIDCProviderTestResponse{})},
		{"OIDCRefreshResponse", reflect.TypeOf(openAPIOIDCRefreshResponse{})},
		{"ChatRequest", reflect.TypeOf(domain.ChatRequest{})},
		{"Conversation", reflect.TypeOf(domain.Conversation{})},
		{"ConversationWithMessages", reflect.TypeOf(domain.ConversationWithMessages{})},
		{"ConversationListResponse", reflect.TypeOf(openAPIConversationListResponse{})},
		{"CreateConversationRequest", reflect.TypeOf(openAPICreateConversationRequest{})},
		{"ApplyPlanRequest", reflect.TypeOf(domain.ApplyPlanRequest{})},
		{"ApplyPlanResponse", reflect.TypeOf(openAPIApplyPlanResponse{})},
		{"UpdateCheckResponse", reflect.TypeOf(openAPIUpdateCheckResponse{})},
		{"UpgradeRequestResponse", reflect.TypeOf(openAPIUpgradeRequestResponse{})},
		{"UpgradeStatusResponse", reflect.TypeOf(openAPIUpgradeStatusResponse{})},
		{"UpgradeStatusAckResponse", reflect.TypeOf(openAPIUpgradeStatusAckResponse{})},
	}
	sort.Slice(types, func(i, j int) bool { return types[i].name < types[j].name })
	return types
}

func schemaFromType(t reflect.Type, componentName string) *openAPISchema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == reflect.TypeOf(time.Time{}) {
		return &openAPISchema{Type: "string", Format: "date-time"}
	}
	if t == reflect.TypeOf(uuid.UUID{}) {
		return &openAPISchema{Type: "string", Format: "uuid"}
	}
	switch t.Kind() {
	case reflect.Bool:
		return &openAPISchema{Type: "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32:
		return &openAPISchema{Type: "integer", Format: "int32"}
	case reflect.Int64, reflect.Uint64:
		return &openAPISchema{Type: "integer", Format: "int64"}
	case reflect.Float32, reflect.Float64:
		return &openAPISchema{Type: "number", Format: "double"}
	case reflect.String:
		return &openAPISchema{Type: "string"}
	case reflect.Slice, reflect.Array:
		return &openAPISchema{Type: "array", Items: schemaFromType(t.Elem(), "")}
	case reflect.Map:
		return &openAPISchema{Type: "object", AdditionalProperties: schemaFromType(t.Elem(), "")}
	case reflect.Interface:
		return &openAPISchema{Type: "object"}
	case reflect.Struct:
		if ref := componentRefForType(t); ref != "" && ref != componentName {
			return &openAPISchema{Ref: ref}
		}
		return objectSchemaFromStruct(t, componentName)
	default:
		return &openAPISchema{Type: "object"}
	}
}

func objectSchemaFromStruct(t reflect.Type, componentName string) *openAPISchema {
	props := map[string]*openAPISchema{}
	var required []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, omitEmpty, skip := jsonFieldName(field)
		if skip {
			continue
		}
		if field.Anonymous && name == "" {
			embedded := schemaFromType(field.Type, componentName)
			for prop, schema := range embedded.Properties {
				props[prop] = schema
			}
			required = append(required, embedded.Required...)
			continue
		}
		if name == "" {
			name = lowerCamel(field.Name)
		}
		props[name] = schemaFromType(field.Type, componentName)
		if !omitEmpty && field.Type.Kind() != reflect.Pointer {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	return &openAPISchema{Type: "object", Properties: props, Required: required}
}

func componentRefForType(t reflect.Type) string {
	for _, def := range openAPIComponentTypes() {
		if def.typ == t {
			return def.name
		}
	}
	return ""
}

func jsonFieldName(field reflect.StructField) (string, bool, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false, true
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	omitEmpty := false
	for _, part := range parts[1:] {
		if part == "omitempty" {
			omitEmpty = true
		}
	}
	return name, omitEmpty, false
}

func writeSchema(b *bytes.Buffer, schema *openAPISchema, indent int) {
	prefix := strings.Repeat(" ", indent)
	if schema.Ref != "" {
		fmt.Fprintf(b, "%s$ref: '#/components/schemas/%s'\n", prefix, schema.Ref)
		return
	}
	if schema.Type != "" {
		fmt.Fprintf(b, "%stype: %s\n", prefix, schema.Type)
	}
	if schema.Format != "" {
		fmt.Fprintf(b, "%sformat: %s\n", prefix, schema.Format)
	}
	if schema.Items != nil {
		fmt.Fprintf(b, "%sitems:\n", prefix)
		writeSchema(b, schema.Items, indent+2)
	}
	if schema.AdditionalProperties != nil {
		fmt.Fprintf(b, "%sadditionalProperties:\n", prefix)
		writeSchema(b, schema.AdditionalProperties, indent+2)
	}
	if len(schema.Properties) > 0 {
		fmt.Fprintf(b, "%sproperties:\n", prefix)
		names := make([]string, 0, len(schema.Properties))
		for name := range schema.Properties {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprintf(b, "%s  %s:\n", prefix, yamlQuote(name))
			writeSchema(b, schema.Properties[name], indent+4)
		}
	}
	if len(schema.Required) > 0 {
		fmt.Fprintf(b, "%srequired:\n", prefix)
		for _, name := range schema.Required {
			fmt.Fprintf(b, "%s  - %s\n", prefix, yamlQuote(name))
		}
	}
}

func openAPIRoutesForPattern(pattern string) []openAPIRoute {
	if method, path, ok := splitMethodPattern(pattern); ok {
		return routesForMethodPath(method, path)
	}
	switch pattern {
	case "/":
		return nil
	case "/openapi":
		return routesForMethodPath(http.MethodGet, "/openapi")
	case "/openapi.yaml":
		return routesForMethodPath(http.MethodGet, "/openapi.yaml")
	case "/healthz":
		return routesForMethodPath(http.MethodGet, "/healthz")
	case "/readyz":
		return routesForMethodPath(http.MethodGet, "/readyz")
	case "/metrics":
		return routesForMethodPath(http.MethodGet, "/metrics")
	case "/api/v1/test-sentry":
		return routesForMethodPath(http.MethodGet, "/api/v1/test-sentry")
	case "/api/v1/auth/setup":
		return routesForMethodPath(http.MethodPost, "/api/v1/auth/setup")
	case "/api/v1/system/info":
		return routesForMethodPath(http.MethodGet, "/api/v1/system/info")
	case "/api/v1/system/changelog":
		return routesForMethodPath(http.MethodGet, "/api/v1/system/changelog")
	case "/api/v1/pools":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPost}, "/api/v1/pools")
	case "/api/v1/pools/":
		return routesForKnown([][2]string{
			{http.MethodGet, "/api/v1/pools/hierarchy"},
			{http.MethodGet, "/api/v1/pools/{poolId}"},
			{http.MethodPatch, "/api/v1/pools/{poolId}"},
			{http.MethodDelete, "/api/v1/pools/{poolId}"},
			{http.MethodGet, "/api/v1/pools/{poolId}/blocks"},
			{http.MethodGet, "/api/v1/pools/{poolId}/stats"},
		})
	case "/api/v1/accounts":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPost}, "/api/v1/accounts")
	case "/api/v1/accounts/":
		return routesForKnown([][2]string{
			{http.MethodGet, "/api/v1/accounts/{accountId}"},
			{http.MethodPatch, "/api/v1/accounts/{accountId}"},
			{http.MethodDelete, "/api/v1/accounts/{accountId}"},
		})
	case "/api/v1/blocks":
		return routesForMethodPath(http.MethodGet, "/api/v1/blocks")
	case "/api/v1/export":
		return routesForMethodPath(http.MethodGet, "/api/v1/export")
	case "/api/v1/schema/check":
		return routesForMethodPath(http.MethodPost, "/api/v1/schema/check")
	case "/api/v1/schema/apply":
		return routesForMethodPath(http.MethodPost, "/api/v1/schema/apply")
	case "/api/v1/search":
		return routesForMethodPath(http.MethodGet, "/api/v1/search")
	case "/api/v1/discovery/resources":
		return routesForMethodPath(http.MethodGet, "/api/v1/discovery/resources")
	case "/api/v1/discovery/resources/":
		return routesForKnown([][2]string{
			{http.MethodGet, "/api/v1/discovery/resources/{resourceId}"},
			{http.MethodPost, "/api/v1/discovery/resources/{resourceId}/link"},
			{http.MethodDelete, "/api/v1/discovery/resources/{resourceId}/link"},
		})
	case "/api/v1/discovery/import":
		return routesForMethodPath(http.MethodPost, "/api/v1/discovery/import")
	case "/api/v1/discovery/sync":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPost}, "/api/v1/discovery/sync")
	case "/api/v1/discovery/sync/":
		return routesForMethodPath(http.MethodGet, "/api/v1/discovery/sync/{syncJobId}")
	case "/api/v1/discovery/ingest":
		return routesForMethodPath(http.MethodPost, "/api/v1/discovery/ingest")
	case "/api/v1/discovery/ingest/org":
		return routesForMethodPath(http.MethodPost, "/api/v1/discovery/ingest/org")
	case "/api/v1/discovery/agents":
		return routesForMethodPath(http.MethodGet, "/api/v1/discovery/agents")
	case "/api/v1/discovery/agents/":
		return routesForKnown([][2]string{
			{http.MethodGet, "/api/v1/discovery/agents/{agentId}"},
			{http.MethodDelete, "/api/v1/discovery/agents/{agentId}"},
			{http.MethodPost, "/api/v1/discovery/agents/provision"},
			{http.MethodPost, "/api/v1/discovery/agents/register"},
			{http.MethodPost, "/api/v1/discovery/agents/heartbeat"},
			{http.MethodPost, "/api/v1/discovery/agents/{agentId}/approve"},
			{http.MethodPost, "/api/v1/discovery/agents/{agentId}/reject"},
		})
	case "/api/v1/network/flat":
		return routesForMethodPath(http.MethodGet, "/api/v1/network/flat")
	case "/api/v1/network/hierarchy":
		return routesForMethodPath(http.MethodGet, "/api/v1/network/hierarchy")
	case "/api/v1/network/merged":
		return routesForMethodPath(http.MethodGet, "/api/v1/network/merged")
	case "/api/v1/network/conflicts":
		return routesForMethodPath(http.MethodGet, "/api/v1/network/conflicts")
	case "/api/v1/network/conflicts/":
		return routesForKnown([][2]string{
			{http.MethodPost, "/api/v1/network/conflicts/{conflictId}/resolve"},
			{http.MethodPost, "/api/v1/network/conflicts/{conflictId}/actions/link"},
			{http.MethodPost, "/api/v1/network/conflicts/{conflictId}/actions/import"},
			{http.MethodPost, "/api/v1/network/conflicts/{conflictId}/actions/create_placeholder_parent"},
		})
	case "/api/v1/network/objects":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPost}, "/api/v1/network/objects")
	case "/api/v1/network/objects/":
		return routesForKnown([][2]string{
			{http.MethodGet, "/api/v1/network/objects/{objectId}"},
			{http.MethodPatch, "/api/v1/network/objects/{objectId}"},
		})
	case "/api/v1/network/relationships":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPost}, "/api/v1/network/relationships")
	case "/api/v1/network/relationships/":
		return routesForKnown([][2]string{
			{http.MethodPost, "/api/v1/network/relationships/resolve"},
			{http.MethodPost, "/api/v1/network/relationships/{relationshipId}/resolve"},
		})
	case "/api/v1/analysis":
		return routesForMethodPath(http.MethodPost, "/api/v1/analysis")
	case "/api/v1/analysis/gaps":
		return routesForMethodPath(http.MethodPost, "/api/v1/analysis/gaps")
	case "/api/v1/analysis/fragmentation":
		return routesForMethodPath(http.MethodPost, "/api/v1/analysis/fragmentation")
	case "/api/v1/analysis/compliance":
		return routesForMethodPath(http.MethodPost, "/api/v1/analysis/compliance")
	case "/api/v1/recommendations/generate":
		return routesForMethodPath(http.MethodPost, "/api/v1/recommendations/generate")
	case "/api/v1/recommendations":
		return routesForMethodPath(http.MethodGet, "/api/v1/recommendations")
	case "/api/v1/recommendations/":
		return routesForKnown([][2]string{
			{http.MethodGet, "/api/v1/recommendations/{recommendationId}"},
			{http.MethodPost, "/api/v1/recommendations/{recommendationId}/apply"},
			{http.MethodPost, "/api/v1/recommendations/{recommendationId}/dismiss"},
		})
	case "/api/v1/drift/detect":
		return routesForMethodPath(http.MethodPost, "/api/v1/drift/detect")
	case "/api/v1/drift":
		return routesForMethodPath(http.MethodGet, "/api/v1/drift")
	case "/api/v1/drift/":
		return routesForKnown([][2]string{
			{http.MethodGet, "/api/v1/drift/{driftItemId}"},
			{http.MethodPost, "/api/v1/drift/{driftItemId}/resolve"},
			{http.MethodPost, "/api/v1/drift/{driftItemId}/ignore"},
		})
	case "/api/v1/settings/security":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPatch}, "/api/v1/settings/security")
	case "/api/v1/settings/network-schema-policy":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPatch}, "/api/v1/settings/network-schema-policy")
	case "/api/v1/settings/oidc/providers":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPost}, "/api/v1/settings/oidc/providers")
	case "/api/v1/settings/oidc/providers/{id}":
		return routesForKnown([][2]string{
			{http.MethodGet, "/api/v1/settings/oidc/providers/{providerId}"},
			{http.MethodPatch, "/api/v1/settings/oidc/providers/{providerId}"},
			{http.MethodDelete, "/api/v1/settings/oidc/providers/{providerId}"},
		})
	case "/api/v1/settings/oidc/providers/{id}/test":
		return routesForMethodPath(http.MethodPost, "/api/v1/settings/oidc/providers/{providerId}/test")
	case "/api/v1/auth/login":
		return routesForMethodPath(http.MethodPost, "/api/v1/auth/login")
	case "/api/v1/auth/logout":
		return routesForMethodPath(http.MethodPost, "/api/v1/auth/logout")
	case "/api/v1/auth/me":
		return routesForMethodPath(http.MethodGet, "/api/v1/auth/me")
	case "/api/v1/auth/users":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPost}, "/api/v1/auth/users")
	case "/api/v1/auth/users/":
		return routesForKnown([][2]string{
			{http.MethodGet, "/api/v1/auth/users/{userId}"},
			{http.MethodPatch, "/api/v1/auth/users/{userId}"},
			{http.MethodDelete, "/api/v1/auth/users/{userId}"},
			{http.MethodPatch, "/api/v1/auth/users/{userId}/password"},
			{http.MethodPost, "/api/v1/auth/users/{userId}/revoke-sessions"},
			{http.MethodPost, "/api/v1/auth/users/{userId}/unlock"},
		})
	case "/api/v1/auth/keys":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPost}, "/api/v1/auth/keys")
	case "/api/v1/auth/keys/":
		return routesForMethodPath(http.MethodDelete, "/api/v1/auth/keys/{keyId}")
	case "/api/v1/auth/permissions":
		return routesForMethodPath(http.MethodGet, "/api/v1/auth/permissions")
	case "/api/v1/auth/roles":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPost}, "/api/v1/auth/roles")
	case "/api/v1/auth/roles/":
		return routesForKnown([][2]string{
			{http.MethodGet, "/api/v1/auth/roles/{roleName}"},
			{http.MethodPatch, "/api/v1/auth/roles/{roleName}"},
			{http.MethodDelete, "/api/v1/auth/roles/{roleName}"},
		})
	case "/api/v1/audit":
		return routesForMethodPath(http.MethodGet, "/api/v1/audit")
	case "/api/v1/auth/oidc/login":
		return routesForMethodPath(http.MethodGet, "/api/v1/auth/oidc/login")
	case "/api/v1/auth/oidc/callback":
		return routesForMethodPath(http.MethodGet, "/api/v1/auth/oidc/callback")
	case "/api/v1/auth/oidc/refresh":
		return routesForMethodPath(http.MethodPost, "/api/v1/auth/oidc/refresh")
	case "/api/v1/auth/oidc/providers":
		return routesForMethodPath(http.MethodGet, "/api/v1/auth/oidc/providers")
	case "/api/v1/ai/chat":
		return routesForMethodPath(http.MethodPost, "/api/v1/ai/chat")
	case "/api/v1/ai/sessions":
		return routesForMethodsPath([]string{http.MethodGet, http.MethodPost}, "/api/v1/ai/sessions")
	case "/api/v1/ai/sessions/":
		return routesForKnown([][2]string{
			{http.MethodGet, "/api/v1/ai/sessions/{sessionId}"},
			{http.MethodDelete, "/api/v1/ai/sessions/{sessionId}"},
			{http.MethodPost, "/api/v1/ai/sessions/{sessionId}/apply-plan"},
		})
	case "/api/v1/updates":
		return routesForMethodPath(http.MethodGet, "/api/v1/updates")
	case "/api/v1/updates/upgrade":
		return routesForMethodPath(http.MethodPost, "/api/v1/updates/upgrade")
	case "/api/v1/updates/status":
		return routesForMethodPath(http.MethodGet, "/api/v1/updates/status")
	case "/api/v1/updates/status/ack":
		return routesForMethodPath(http.MethodPost, "/api/v1/updates/status/ack")
	default:
		if strings.HasPrefix(pattern, "GET ") || strings.HasPrefix(pattern, "POST ") ||
			strings.HasPrefix(pattern, "PATCH ") || strings.HasPrefix(pattern, "DELETE ") {
			method, path, _ := splitMethodPattern(pattern)
			return routesForMethodPath(method, path)
		}
		return []openAPIRoute{inferOpenAPIRoute(http.MethodGet, pattern)}
	}
}

func splitMethodPattern(pattern string) (string, string, bool) {
	parts := strings.SplitN(pattern, " ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	rawPath := strings.TrimSpace(parts[1])
	path := normalizeOpenAPIPath(rawPath)
	switch path {
	case "/api/v1/ai/sessions/{id}/apply-plan":
		path = "/api/v1/ai/sessions/{sessionId}/apply-plan"
	case "/api/v1/network/objects":
		if strings.HasSuffix(rawPath, "/") {
			path = "/api/v1/network/objects/{objectId}"
		}
	case "/api/v1/network/conflicts/{id}/resolve":
		path = "/api/v1/network/conflicts/{conflictId}/resolve"
	case "/api/v1/network/conflicts/{id}/actions/link":
		path = "/api/v1/network/conflicts/{conflictId}/actions/link"
	case "/api/v1/network/conflicts/{id}/actions/import":
		path = "/api/v1/network/conflicts/{conflictId}/actions/import"
	case "/api/v1/network/conflicts/{id}/actions/create_placeholder_parent":
		path = "/api/v1/network/conflicts/{conflictId}/actions/create_placeholder_parent"
	case "/api/v1/settings/oidc/providers/{id}":
		path = "/api/v1/settings/oidc/providers/{providerId}"
	case "/api/v1/settings/oidc/providers/{id}/test":
		path = "/api/v1/settings/oidc/providers/{providerId}/test"
	}
	switch parts[0] {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return parts[0], path, true
	default:
		return "", "", false
	}
}

func routesForMethodsPath(methods []string, path string) []openAPIRoute {
	routes := make([]openAPIRoute, 0, len(methods))
	for _, method := range methods {
		routes = append(routes, routesForMethodPath(method, path)...)
	}
	return routes
}

func routesForKnown(pairs [][2]string) []openAPIRoute {
	routes := make([]openAPIRoute, 0, len(pairs))
	for _, pair := range pairs {
		routes = append(routes, routesForMethodPath(pair[0], pair[1])...)
	}
	return routes
}

func routesForMethodPath(method, path string) []openAPIRoute {
	path = normalizeOpenAPIPath(path)
	if route, ok := openAPIOperationCatalog()[method+" "+path]; ok {
		return []openAPIRoute{route}
	}
	return []openAPIRoute{inferOpenAPIRoute(method, path)}
}

func normalizeOpenAPIPath(path string) string {
	path = strings.TrimSpace(path)
	return strings.TrimRight(path, "/")
}

func inferOpenAPIRoute(method, path string) openAPIRoute {
	return openAPIRoute{
		Method:              method,
		Path:                normalizeOpenAPIPath(path),
		Summary:             method + " " + normalizeOpenAPIPath(path),
		Tag:                 tagForPath(path),
		Security:            !isPublicOpenAPIPath(path),
		ResponseSchema:      "Object",
		ResponseDescription: "Successful response",
		Parameters:          pathParameters(path),
	}
}

func openAPIOperationCatalog() map[string]openAPIRoute {
	routes := []openAPIRoute{
		{Method: "GET", Path: "/openapi", Summary: "View the interactive API reference", Tag: "System", Security: false, ResponseSchema: "String", ResponseContentType: "text/html"},
		{Method: "GET", Path: "/openapi.yaml", Summary: "Get the runtime OpenAPI specification", Tag: "System", Security: false, ResponseSchema: "String", ResponseContentType: "application/yaml"},
		{Method: "GET", Path: "/healthz", Summary: "Liveness probe", Tag: "System", Security: false, ResponseSchema: "HealthResponse"},
		{Method: "GET", Path: "/readyz", Summary: "Readiness probe with dependency checks", Tag: "System", Security: false, ResponseSchema: "ReadinessResponse"},
		{Method: "GET", Path: "/metrics", Summary: "Prometheus metrics", Tag: "System", Security: false, ResponseSchema: "String", ResponseContentType: "text/plain"},
		{Method: "GET", Path: "/api/v1/test-sentry", Summary: "Test Sentry integration", Tag: "System", Security: false, ResponseSchema: "Object", Parameters: []openAPIParameter{queryParam("type", "Sentry test type", "string")}},
		{Method: "GET", Path: "/api/v1/system/info", Summary: "Get system metadata", Tag: "System", ResponseSchema: "SystemInfoResponse"},
		{Method: "GET", Path: "/api/v1/system/changelog", Summary: "Get changelog markdown", Tag: "System", ResponseSchema: "String", ResponseContentType: "text/markdown"},
		{Method: "POST", Path: "/api/v1/auth/setup", Summary: "Create first admin account", Tag: "Auth", Security: false, RequestSchema: "SetupRequest", SuccessStatus: "201", ResponseSchema: "SetupResponse", ResponseDescription: "Initial admin account created"},
		{Method: "GET", Path: "/api/v1/pools", Summary: "List pools", Tag: "Pools", ResponseSchema: "Object", Parameters: []openAPIParameter{queryParam("include_stats", "Include utilization statistics", "boolean")}},
		{Method: "POST", Path: "/api/v1/pools", Summary: "Create pool", Tag: "Pools", RequestSchema: "CreatePool", SuccessStatus: "201", ResponseSchema: "Pool", ResponseDescription: "Pool created"},
		{Method: "GET", Path: "/api/v1/pools/hierarchy", Summary: "Get pool hierarchy", Tag: "Pools", ResponseSchema: "Object", Parameters: []openAPIParameter{queryParam("root_id", "Optional root pool ID", "integer")}},
		{Method: "GET", Path: "/api/v1/pools/{poolId}", Summary: "Get pool", Tag: "Pools", ResponseSchema: "Pool"},
		{Method: "PATCH", Path: "/api/v1/pools/{poolId}", Summary: "Update pool metadata", Tag: "Pools", RequestSchema: "UpdatePool", ResponseSchema: "Pool"},
		{Method: "DELETE", Path: "/api/v1/pools/{poolId}", Summary: "Delete pool", Tag: "Pools", ResponseDescription: "Pool deleted", Parameters: []openAPIParameter{queryParam("force", "Force recursive delete where supported", "boolean")}},
		{Method: "GET", Path: "/api/v1/pools/{poolId}/blocks", Summary: "Enumerate candidate blocks", Tag: "Blocks", ResponseSchema: "Object", Parameters: []openAPIParameter{queryParam("new_prefix_len", "Requested block prefix length", "integer"), queryParam("page", "Page number", "integer"), queryParam("page_size", "Page size", "integer")}},
		{Method: "GET", Path: "/api/v1/pools/{poolId}/stats", Summary: "Get pool utilization statistics", Tag: "Pools", ResponseSchema: "PoolStats"},
		{Method: "GET", Path: "/api/v1/accounts", Summary: "List accounts", Tag: "Accounts", ResponseSchema: "Object"},
		{Method: "POST", Path: "/api/v1/accounts", Summary: "Create account", Tag: "Accounts", RequestSchema: "CreateAccount", SuccessStatus: "201", ResponseSchema: "Account", ResponseDescription: "Account created"},
		{Method: "GET", Path: "/api/v1/accounts/{accountId}", Summary: "Get account", Tag: "Accounts", ResponseSchema: "Account"},
		{Method: "PATCH", Path: "/api/v1/accounts/{accountId}", Summary: "Update account", Tag: "Accounts", RequestSchema: "UpdateAccount", ResponseSchema: "Account"},
		{Method: "DELETE", Path: "/api/v1/accounts/{accountId}", Summary: "Delete account", Tag: "Accounts", ResponseDescription: "Account deleted"},
		{Method: "GET", Path: "/api/v1/blocks", Summary: "List assigned blocks", Tag: "Blocks", ResponseSchema: "Object", Parameters: []openAPIParameter{queryParam("q", "Search query", "string"), queryParam("pool_id", "Pool ID", "integer"), queryParam("account_id", "Account ID", "integer"), queryParam("page", "Page number", "integer"), queryParam("page_size", "Page size", "integer")}},
		{Method: "GET", Path: "/api/v1/export", Summary: "Export pools and accounts as CSV ZIP", Tag: "Export", ResponseSchema: "String", ResponseContentType: "application/zip"},
		{Method: "POST", Path: "/api/v1/import/accounts", Summary: "Import accounts from CSV", Tag: "Import", RequestSchema: "Object", ResponseSchema: "ImportResponse"},
		{Method: "POST", Path: "/api/v1/import/pools", Summary: "Import pools from CSV", Tag: "Import", RequestSchema: "Object", ResponseSchema: "ImportResponse"},
		{Method: "POST", Path: "/api/v1/schema/check", Summary: "Check schema conflicts", Tag: "Schema", RequestSchema: "SchemaPlanRequest", ResponseSchema: "Object"},
		{Method: "POST", Path: "/api/v1/schema/apply", Summary: "Apply schema plan", Tag: "Schema", RequestSchema: "SchemaPlanRequest", ResponseSchema: "Object"},
		{Method: "GET", Path: "/api/v1/search", Summary: "Search pools and accounts", Tag: "Search", ResponseSchema: "SearchResponse", Parameters: []openAPIParameter{queryParam("q", "Search query", "string"), queryParam("type", "Optional result type", "string")}},
		{Method: "GET", Path: "/api/v1/discovery/resources", Summary: "List discovered resources", Tag: "Discovery", ResponseSchema: "DiscoveryResourcesResponse", Parameters: discoveryResourceQueryParams()},
		{Method: "GET", Path: "/api/v1/discovery/resources/{resourceId}", Summary: "Get discovered resource", Tag: "Discovery", ResponseSchema: "DiscoveredResource"},
		{Method: "POST", Path: "/api/v1/discovery/resources/{resourceId}/link", Summary: "Link discovered resource to pool", Tag: "Discovery", RequestSchema: "LinkResourceRequest", ResponseSchema: "StatusResponse"},
		{Method: "DELETE", Path: "/api/v1/discovery/resources/{resourceId}/link", Summary: "Unlink discovered resource from pool", Tag: "Discovery", SuccessStatus: "200", ResponseSchema: "StatusResponse"},
		{Method: "POST", Path: "/api/v1/discovery/import", Summary: "Import discovered resources as pools", Tag: "Discovery", RequestSchema: "DiscoveryImportRequest", ResponseSchema: "DiscoveryImportResponse"},
		{Method: "GET", Path: "/api/v1/discovery/sync", Summary: "List discovery sync jobs", Tag: "Discovery", ResponseSchema: "SyncJobsResponse", Parameters: []openAPIParameter{queryParam("account_id", "Account ID", "integer"), queryParam("limit", "Maximum jobs to return", "integer")}},
		{Method: "POST", Path: "/api/v1/discovery/sync", Summary: "Trigger discovery sync", Tag: "Discovery", RequestSchema: "SyncRequest", ResponseSchema: "Object"},
		{Method: "GET", Path: "/api/v1/discovery/sync/{syncJobId}", Summary: "Get discovery sync job", Tag: "Discovery", ResponseSchema: "SyncJob"},
		{Method: "POST", Path: "/api/v1/discovery/ingest", Summary: "Ingest agent discovery resources", Tag: "Discovery", RequestSchema: "IngestRequest", ResponseSchema: "IngestResponse"},
		{Method: "POST", Path: "/api/v1/discovery/ingest/org", Summary: "Ingest AWS Organizations resources", Tag: "Discovery", RequestSchema: "BulkIngestRequest", ResponseSchema: "BulkIngestResponse"},
		{Method: "GET", Path: "/api/v1/discovery/agents", Summary: "List discovery agents", Tag: "Discovery", ResponseSchema: "DiscoveryAgentsResponse", Parameters: []openAPIParameter{queryParam("account_id", "Optional account ID", "integer")}},
		{Method: "GET", Path: "/api/v1/discovery/agents/{agentId}", Summary: "Get discovery agent", Tag: "Discovery", ResponseSchema: "DiscoveryAgent"},
		{Method: "DELETE", Path: "/api/v1/discovery/agents/{agentId}", Summary: "Delete discovery agent", Tag: "Discovery", SuccessStatus: "200", ResponseSchema: "StatusResponse"},
		{Method: "POST", Path: "/api/v1/discovery/agents/provision", Summary: "Provision discovery agent", Tag: "Discovery", RequestSchema: "AgentProvisionRequest", SuccessStatus: "201", ResponseSchema: "AgentProvisionResponse"},
		{Method: "POST", Path: "/api/v1/discovery/agents/register", Summary: "Register discovery agent", Tag: "Discovery", RequestSchema: "AgentRegisterRequest", SuccessStatus: "201", ResponseSchema: "AgentRegisterResponse"},
		{Method: "POST", Path: "/api/v1/discovery/agents/heartbeat", Summary: "Record discovery agent heartbeat", Tag: "Discovery", RequestSchema: "AgentHeartbeatRequest", ResponseSchema: "AgentHeartbeatResponse"},
		{Method: "POST", Path: "/api/v1/discovery/agents/{agentId}/approve", Summary: "Approve discovery agent", Tag: "Discovery", ResponseSchema: "AgentRegisterResponse"},
		{Method: "POST", Path: "/api/v1/discovery/agents/{agentId}/reject", Summary: "Reject discovery agent", Tag: "Discovery", ResponseSchema: "AgentRegisterResponse"},
		{Method: "GET", Path: "/api/v1/network/flat", Summary: "Get flat network view", Tag: "Network", ResponseSchema: "Object", Parameters: networkViewQueryParams()},
		{Method: "GET", Path: "/api/v1/network/hierarchy", Summary: "Get hierarchical network view", Tag: "Network", ResponseSchema: "Object", Parameters: networkViewQueryParams()},
		{Method: "GET", Path: "/api/v1/network/merged", Summary: "Get merged network view", Tag: "Network", ResponseSchema: "Object", Parameters: networkViewQueryParams()},
		{Method: "GET", Path: "/api/v1/network/conflicts", Summary: "List network conflicts", Tag: "Network", ResponseSchema: "NetworkConflictListResponse", Parameters: networkViewQueryParams()},
		{Method: "POST", Path: "/api/v1/network/conflicts/{conflictId}/resolve", Summary: "Record passive network conflict decision", Tag: "Network", RequestSchema: "ResolveNetworkConflictRequest", ResponseSchema: "NetworkConflict"},
		{Method: "POST", Path: "/api/v1/network/conflicts/{conflictId}/actions/link", Summary: "Link network conflict resource to pool", Tag: "Network", RequestSchema: "NetworkConflictLinkActionRequest", ResponseSchema: "NetworkConflictActionResponse"},
		{Method: "POST", Path: "/api/v1/network/conflicts/{conflictId}/actions/import", Summary: "Import network conflict resources as pools", Tag: "Network", RequestSchema: "NetworkConflictImportActionRequest", ResponseSchema: "NetworkConflictActionResponse"},
		{Method: "POST", Path: "/api/v1/network/conflicts/{conflictId}/actions/create_placeholder_parent", Summary: "Create placeholder parent network object", Tag: "Network", RequestSchema: "NetworkConflictPlaceholderParentActionRequest", ResponseSchema: "NetworkConflictActionResponse"},
		{Method: "GET", Path: "/api/v1/network/objects", Summary: "List managed network objects", Tag: "Network", ResponseSchema: "NetworkObjectListResponse", Parameters: networkObjectQueryParams()},
		{Method: "POST", Path: "/api/v1/network/objects", Summary: "Create managed network object", Tag: "Network", RequestSchema: "CreateNetworkObject", SuccessStatus: "201", ResponseSchema: "NetworkObject"},
		{Method: "GET", Path: "/api/v1/network/objects/{objectId}", Summary: "Get managed network object", Tag: "Network", ResponseSchema: "NetworkObject"},
		{Method: "PATCH", Path: "/api/v1/network/objects/{objectId}", Summary: "Update managed network object", Tag: "Network", RequestSchema: "UpdateNetworkObject", ResponseSchema: "NetworkObject"},
		{Method: "GET", Path: "/api/v1/network/relationships", Summary: "List network relationships", Tag: "Network", ResponseSchema: "NetworkRelationshipListResponse", Parameters: networkRelationshipQueryParams()},
		{Method: "POST", Path: "/api/v1/network/relationships", Summary: "Create or update network relationship", Tag: "Network", RequestSchema: "CreateNetworkRelationship", SuccessStatus: "201", ResponseSchema: "NetworkRelationship"},
		{Method: "POST", Path: "/api/v1/network/relationships/resolve", Summary: "Update network relationship resolution state by request body", Tag: "Network", RequestSchema: "ResolveNetworkRelationshipRequest", ResponseSchema: "NetworkRelationship"},
		{Method: "POST", Path: "/api/v1/network/relationships/{relationshipId}/resolve", Summary: "Update network relationship resolution state", Tag: "Network", RequestSchema: "ResolveNetworkRelationshipRequest", ResponseSchema: "NetworkRelationship"},
		{Method: "POST", Path: "/api/v1/analysis", Summary: "Run full network analysis", Tag: "Analysis", RequestSchema: "AnalysisRequest", ResponseSchema: "Object"},
		{Method: "POST", Path: "/api/v1/analysis/gaps", Summary: "Run gap analysis", Tag: "Analysis", RequestSchema: "GapAnalysisRequest", ResponseSchema: "Object"},
		{Method: "POST", Path: "/api/v1/analysis/fragmentation", Summary: "Run fragmentation analysis", Tag: "Analysis", RequestSchema: "AnalysisRequest", ResponseSchema: "Object"},
		{Method: "POST", Path: "/api/v1/analysis/compliance", Summary: "Run compliance checks", Tag: "Analysis", RequestSchema: "AnalysisRequest", ResponseSchema: "Object"},
		{Method: "POST", Path: "/api/v1/recommendations/generate", Summary: "Generate recommendations", Tag: "Recommendations", RequestSchema: "GenerateRecommendationsRequest", ResponseSchema: "GenerateRecommendationsResponse"},
		{Method: "GET", Path: "/api/v1/recommendations", Summary: "List recommendations", Tag: "Recommendations", ResponseSchema: "RecommendationsListResponse", Parameters: paginatedFilterParams("pool_id", "type", "status", "priority")},
		{Method: "GET", Path: "/api/v1/recommendations/{recommendationId}", Summary: "Get recommendation", Tag: "Recommendations", ResponseSchema: "Recommendation"},
		{Method: "POST", Path: "/api/v1/recommendations/{recommendationId}/apply", Summary: "Apply recommendation", Tag: "Recommendations", RequestSchema: "ApplyRecommendationRequest", ResponseSchema: "Recommendation"},
		{Method: "POST", Path: "/api/v1/recommendations/{recommendationId}/dismiss", Summary: "Dismiss recommendation", Tag: "Recommendations", RequestSchema: "DismissRecommendationRequest", ResponseSchema: "Recommendation"},
		{Method: "POST", Path: "/api/v1/drift/detect", Summary: "Run drift detection", Tag: "Drift", RequestSchema: "RunDriftDetectionRequest", ResponseSchema: "RunDriftDetectionResponse"},
		{Method: "GET", Path: "/api/v1/drift", Summary: "List drift items", Tag: "Drift", ResponseSchema: "DriftListResponse", Parameters: paginatedFilterParams("account_id", "type", "severity", "status")},
		{Method: "GET", Path: "/api/v1/drift/{driftItemId}", Summary: "Get drift item", Tag: "Drift", ResponseSchema: "DriftItem"},
		{Method: "POST", Path: "/api/v1/drift/{driftItemId}/resolve", Summary: "Resolve drift item", Tag: "Drift", ResponseSchema: "DriftItem"},
		{Method: "POST", Path: "/api/v1/drift/{driftItemId}/ignore", Summary: "Ignore drift item", Tag: "Drift", RequestSchema: "IgnoreDriftRequest", ResponseSchema: "DriftItem"},
		{Method: "GET", Path: "/api/v1/settings/security", Summary: "Get security settings", Tag: "Settings", ResponseSchema: "SecuritySettings"},
		{Method: "PATCH", Path: "/api/v1/settings/security", Summary: "Update security settings", Tag: "Settings", RequestSchema: "SecuritySettings", ResponseSchema: "SecuritySettings"},
		{Method: "GET", Path: "/api/v1/settings/network-schema-policy", Summary: "Get network schema policy", Tag: "Settings", ResponseSchema: "NetworkSchemaPolicy"},
		{Method: "PATCH", Path: "/api/v1/settings/network-schema-policy", Summary: "Update network schema policy", Tag: "Settings", RequestSchema: "NetworkSchemaPolicy", ResponseSchema: "NetworkSchemaPolicy"},
		{Method: "GET", Path: "/api/v1/auth/login", Summary: "Login", Tag: "Auth", Security: false, RequestSchema: "LoginRequest", ResponseSchema: "LoginResponse"},
		{Method: "POST", Path: "/api/v1/auth/login", Summary: "Login", Tag: "Auth", Security: false, RequestSchema: "LoginRequest", ResponseSchema: "LoginResponse"},
		{Method: "POST", Path: "/api/v1/auth/logout", Summary: "Logout", Tag: "Auth", ResponseDescription: "Session cleared"},
		{Method: "GET", Path: "/api/v1/auth/me", Summary: "Get current identity", Tag: "Auth", ResponseSchema: "MeResponse"},
		{Method: "GET", Path: "/api/v1/auth/users", Summary: "List users", Tag: "Auth", ResponseSchema: "UserListResponse"},
		{Method: "POST", Path: "/api/v1/auth/users", Summary: "Create user", Tag: "Auth", RequestSchema: "CreateUserRequest", SuccessStatus: "201", ResponseSchema: "User"},
		{Method: "GET", Path: "/api/v1/auth/users/{userId}", Summary: "Get user", Tag: "Auth", ResponseSchema: "User"},
		{Method: "PATCH", Path: "/api/v1/auth/users/{userId}", Summary: "Update user", Tag: "Auth", RequestSchema: "UpdateUserRequest", ResponseSchema: "User"},
		{Method: "DELETE", Path: "/api/v1/auth/users/{userId}", Summary: "Deactivate user", Tag: "Auth", ResponseDescription: "User deactivated"},
		{Method: "PATCH", Path: "/api/v1/auth/users/{userId}/password", Summary: "Change user password", Tag: "Auth", RequestSchema: "ChangePasswordRequest", ResponseDescription: "Password changed"},
		{Method: "POST", Path: "/api/v1/auth/users/{userId}/revoke-sessions", Summary: "Revoke user sessions", Tag: "Auth", ResponseSchema: "StatusResponse"},
		{Method: "POST", Path: "/api/v1/auth/users/{userId}/unlock", Summary: "Unlock user", Tag: "Auth", ResponseSchema: "User"},
		{Method: "GET", Path: "/api/v1/auth/keys", Summary: "List API keys", Tag: "Auth", ResponseSchema: "APIKeysResponse"},
		{Method: "POST", Path: "/api/v1/auth/keys", Summary: "Create API key", Tag: "Auth", RequestSchema: "APIKeyCreateRequest", SuccessStatus: "201", ResponseSchema: "APIKeyCreateResponse"},
		{Method: "DELETE", Path: "/api/v1/auth/keys/{keyId}", Summary: "Revoke API key", Tag: "Auth", ResponseDescription: "API key revoked"},
		{Method: "GET", Path: "/api/v1/auth/permissions", Summary: "List RBAC permissions", Tag: "Auth", ResponseSchema: "PermissionsResponse"},
		{Method: "GET", Path: "/api/v1/auth/roles", Summary: "List RBAC roles", Tag: "Auth", ResponseSchema: "RoleListResponse"},
		{Method: "POST", Path: "/api/v1/auth/roles", Summary: "Create RBAC role", Tag: "Auth", RequestSchema: "RoleRequest", SuccessStatus: "201", ResponseSchema: "Role"},
		{Method: "GET", Path: "/api/v1/auth/roles/{roleName}", Summary: "Get RBAC role", Tag: "Auth", ResponseSchema: "Role"},
		{Method: "PATCH", Path: "/api/v1/auth/roles/{roleName}", Summary: "Update RBAC role", Tag: "Auth", RequestSchema: "RoleRequest", ResponseSchema: "Role"},
		{Method: "DELETE", Path: "/api/v1/auth/roles/{roleName}", Summary: "Delete RBAC role", Tag: "Auth", ResponseDescription: "Role deleted"},
		{Method: "GET", Path: "/api/v1/audit", Summary: "Query audit log", Tag: "Audit", ResponseSchema: "AuditListResponse", Parameters: []openAPIParameter{queryParam("limit", "Maximum events", "integer"), queryParam("offset", "Offset", "integer"), queryParam("actor", "Actor filter", "string"), queryParam("action", "Action filter", "string"), queryParam("resource_type", "Resource type filter", "string")}},
		{Method: "GET", Path: "/api/v1/auth/oidc/login", Summary: "Start OIDC login", Tag: "OIDC", Security: false, ResponseDescription: "Redirect to OIDC provider", Parameters: []openAPIParameter{queryParam("provider_id", "OIDC provider ID", "string"), queryParam("prompt", "Optional OIDC prompt", "string")}},
		{Method: "GET", Path: "/api/v1/auth/oidc/callback", Summary: "Handle OIDC callback", Tag: "OIDC", Security: false, ResponseDescription: "Redirect to frontend or iframe HTML", Parameters: []openAPIParameter{queryParam("code", "Authorization code", "string"), queryParam("state", "OIDC state", "string")}},
		{Method: "POST", Path: "/api/v1/auth/oidc/refresh", Summary: "Get OIDC silent refresh URL", Tag: "OIDC", Security: false, ResponseSchema: "OIDCRefreshResponse"},
		{Method: "GET", Path: "/api/v1/auth/oidc/providers", Summary: "List enabled OIDC providers", Tag: "OIDC", Security: false, ResponseSchema: "PublicOIDCProvidersResponse"},
		{Method: "GET", Path: "/api/v1/settings/oidc/providers", Summary: "List OIDC providers", Tag: "OIDC", ResponseSchema: "OIDCProvidersResponse"},
		{Method: "POST", Path: "/api/v1/settings/oidc/providers", Summary: "Create OIDC provider", Tag: "OIDC", RequestSchema: "OIDCProvider", SuccessStatus: "201", ResponseSchema: "OIDCProvider"},
		{Method: "GET", Path: "/api/v1/settings/oidc/providers/{providerId}", Summary: "Get OIDC provider", Tag: "OIDC", ResponseSchema: "OIDCProvider"},
		{Method: "PATCH", Path: "/api/v1/settings/oidc/providers/{providerId}", Summary: "Update OIDC provider", Tag: "OIDC", RequestSchema: "Object", ResponseSchema: "OIDCProvider"},
		{Method: "DELETE", Path: "/api/v1/settings/oidc/providers/{providerId}", Summary: "Delete OIDC provider", Tag: "OIDC", ResponseDescription: "OIDC provider deleted"},
		{Method: "POST", Path: "/api/v1/settings/oidc/providers/{providerId}/test", Summary: "Test OIDC provider discovery", Tag: "OIDC", ResponseSchema: "OIDCProviderTestResponse"},
		{Method: "POST", Path: "/api/v1/ai/chat", Summary: "Stream AI planning chat", Tag: "AI", RequestSchema: "ChatRequest", ResponseSchema: "String", ResponseContentType: "text/event-stream"},
		{Method: "GET", Path: "/api/v1/ai/sessions", Summary: "List AI planning sessions", Tag: "AI", ResponseSchema: "ConversationListResponse"},
		{Method: "POST", Path: "/api/v1/ai/sessions", Summary: "Create AI planning session", Tag: "AI", RequestSchema: "CreateConversationRequest", SuccessStatus: "201", ResponseSchema: "Conversation"},
		{Method: "GET", Path: "/api/v1/ai/sessions/{sessionId}", Summary: "Get AI planning session", Tag: "AI", ResponseSchema: "ConversationWithMessages"},
		{Method: "DELETE", Path: "/api/v1/ai/sessions/{sessionId}", Summary: "Delete AI planning session", Tag: "AI", SuccessStatus: "200", ResponseSchema: "StatusResponse"},
		{Method: "POST", Path: "/api/v1/ai/sessions/{sessionId}/apply-plan", Summary: "Apply generated AI plan", Tag: "AI", RequestSchema: "ApplyPlanRequest", ResponseSchema: "ApplyPlanResponse"},
		{Method: "GET", Path: "/api/v1/updates", Summary: "Check for updates", Tag: "Updates", ResponseSchema: "UpdateCheckResponse", Parameters: []openAPIParameter{queryParam("force", "Force refresh release metadata", "boolean")}},
		{Method: "POST", Path: "/api/v1/updates/upgrade", Summary: "Request in-app upgrade", Tag: "Updates", SuccessStatus: "202", ResponseSchema: "UpgradeRequestResponse"},
		{Method: "GET", Path: "/api/v1/updates/status", Summary: "Get upgrade status", Tag: "Updates", ResponseSchema: "UpgradeStatusResponse"},
		{Method: "POST", Path: "/api/v1/updates/status/ack", Summary: "Acknowledge completed upgrade status", Tag: "Updates", ResponseSchema: "UpgradeStatusAckResponse"},
	}
	catalog := make(map[string]openAPIRoute, len(routes))
	for _, route := range routes {
		route.Path = normalizeOpenAPIPath(route.Path)
		route.Security = !isPublicOpenAPIPath(route.Path)
		route.Parameters = append(pathParameters(route.Path), route.Parameters...)
		if route.ResponseSchema == "" && successStatus(route.Method) != "204" {
			route.ResponseSchema = "Object"
		}
		catalog[route.Method+" "+route.Path] = route
	}
	return catalog
}

func queryParam(name, description, typ string) openAPIParameter {
	return openAPIParameter{Name: name, In: "query", Description: description, Type: typ}
}

func pathParameters(path string) []openAPIParameter {
	var params []openAPIParameter
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			name := strings.TrimSuffix(strings.TrimPrefix(part, "{"), "}")
			typ := "string"
			format := ""
			if strings.HasSuffix(strings.ToLower(name), "id") && !strings.Contains(strings.ToLower(name), "uuid") {
				format = ""
			}
			params = append(params, openAPIParameter{Name: name, In: "path", Required: true, Type: typ, Format: format})
		}
	}
	return params
}

func discoveryResourceQueryParams() []openAPIParameter {
	return []openAPIParameter{
		queryParam("account_id", "Account ID", "integer"),
		queryParam("provider", "Cloud provider", "string"),
		queryParam("region", "Cloud region", "string"),
		queryParam("resource_type", "Resource type", "string"),
		queryParam("status", "Discovery status", "string"),
		queryParam("q", "Search by name, resource ID, or CIDR", "string"),
		queryParam("linked", "Filter linked/unlinked resources", "boolean"),
		queryParam("page", "Page number", "integer"),
		queryParam("page_size", "Page size", "integer"),
	}
}

func networkViewQueryParams() []openAPIParameter {
	return []openAPIParameter{
		queryParam("account_id", "Account ID", "integer"),
		queryParam("provider", "Cloud provider", "string"),
		queryParam("region", "Cloud region", "string"),
		queryParam("object_type", "Network object type", "string"),
		queryParam("status", "Network object status", "string"),
		queryParam("conflict_type", "Network conflict type", "string"),
		queryParam("schema_policy", "Schema policy name", "string"),
		queryParam("duplicates", "Duplicate handling override", "string"),
		queryParam("q", "Search query", "string"),
	}
}

func networkObjectQueryParams() []openAPIParameter {
	return []openAPIParameter{
		queryParam("account_id", "Account ID", "integer"),
		queryParam("provider", "Cloud provider", "string"),
		queryParam("region", "Cloud region", "string"),
		queryParam("object_type", "Network object type", "string"),
		queryParam("state", "Network object state", "string"),
		queryParam("pool_id", "Associated pool ID", "integer"),
		queryParam("source_discovered_id", "Source discovered resource ID", "string"),
		queryParam("q", "Search query", "string"),
	}
}

func networkRelationshipQueryParams() []openAPIParameter {
	return []openAPIParameter{
		queryParam("account_id", "Account ID", "integer"),
		queryParam("type", "Relationship type", "string"),
		queryParam("id", "Relationship ID. Repeat to match multiple IDs.", "string"),
		queryParam("source_kind", "Source entity kind", "string"),
		queryParam("source_id", "Source entity ID", "string"),
		queryParam("target_kind", "Target entity kind", "string"),
		queryParam("target_id", "Target entity ID", "string"),
		queryParam("entity_kind", "Endpoint entity kind matched as source or target", "string"),
		queryParam("entity_id", "Endpoint entity ID matched as source or target", "string"),
		queryParam("resolution_state", "Resolution state", "string"),
	}
}

func paginatedFilterParams(names ...string) []openAPIParameter {
	params := []openAPIParameter{
		queryParam("page", "Page number", "integer"),
		queryParam("page_size", "Page size", "integer"),
	}
	for _, name := range names {
		typ := "string"
		if strings.HasSuffix(name, "_id") {
			typ = "integer"
		}
		params = append(params, queryParam(name, strings.ReplaceAll(name, "_", " ")+" filter", typ))
	}
	return params
}

func tagForPath(path string) string {
	switch {
	case strings.Contains(path, "/pools"):
		return "Pools"
	case strings.Contains(path, "/accounts"):
		return "Accounts"
	case strings.Contains(path, "/discovery"), strings.Contains(path, "/drift"):
		return "Discovery"
	case strings.Contains(path, "/auth/oidc"), strings.Contains(path, "/settings/oidc"):
		return "OIDC"
	case strings.Contains(path, "/auth"):
		return "Auth"
	case strings.Contains(path, "/settings"):
		return "Settings"
	case strings.Contains(path, "/analysis"):
		return "Analysis"
	case strings.Contains(path, "/recommendations"):
		return "Recommendations"
	case strings.Contains(path, "/ai"):
		return "AI"
	case strings.Contains(path, "/updates"):
		return "Updates"
	default:
		return "System"
	}
}

func isPublicOpenAPIPath(path string) bool {
	switch path {
	case "/openapi", "/openapi.yaml", "/healthz", "/readyz", "/metrics", "/api/v1/test-sentry", "/api/v1/auth/setup", "/api/v1/auth/login", "/api/v1/auth/oidc/login", "/api/v1/auth/oidc/callback", "/api/v1/auth/oidc/refresh", "/api/v1/auth/oidc/providers":
		return true
	default:
		return false
	}
}

func successStatus(method string) string {
	switch method {
	case http.MethodPost:
		return "200"
	case http.MethodDelete:
		return "204"
	default:
		return "200"
	}
}

func methodOrder(method string) int {
	switch method {
	case http.MethodGet:
		return 0
	case http.MethodPost:
		return 1
	case http.MethodPatch:
		return 2
	case http.MethodDelete:
		return 3
	default:
		return 9
	}
}

func yamlQuote(v string) string {
	if v == "" {
		return "\"\""
	}
	escaped := strings.ReplaceAll(v, "\\", "\\\\")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	return "\"" + escaped + "\""
}

func lowerCamel(v string) string {
	if v == "" {
		return ""
	}
	return strings.ToLower(v[:1]) + v[1:]
}
