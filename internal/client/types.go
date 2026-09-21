package client

// RunRoleSetup is everything a caller needs to build one workspace's run role.
type RunRoleSetup struct {
	PrincipalARN  string   `json:"principal_arn"`
	PrincipalARNs []string `json:"principal_arns"`
	ExternalID    string   `json:"external_id"`
	RoleName      string   `json:"role_name"`
}

// Workspace is a stored workspace as the API renders it.
type Workspace struct {
	WorkspaceID      string       `json:"workspace_id"`
	Name             string       `json:"name"`
	Engine           string       `json:"engine"`
	EngineVersion    string       `json:"engine_version"`
	RunRoleARN       *string      `json:"run_role_arn"`
	WorkingDirectory string       `json:"working_directory"`
	Description      string       `json:"description"`
	CreatedAt        string       `json:"created_at"`
	UpdatedAt        *string      `json:"updated_at"`
	RunRoleSetup     RunRoleSetup `json:"run_role_setup"`
	RunRoleCheckedAt *string      `json:"run_role_checked_at"`
	RunRoleAccountID *string      `json:"run_role_account_id"`
}

// WorkspaceCreate is the body of a workspace create.
type WorkspaceCreate struct {
	Name             string  `json:"name"`
	Engine           string  `json:"engine,omitempty"`
	EngineVersion    string  `json:"engine_version"`
	RunRoleARN       *string `json:"run_role_arn,omitempty"`
	WorkingDirectory string  `json:"working_directory,omitempty"`
	Description      string  `json:"description,omitempty"`
}

// WorkspaceUpdate is a partial workspace edit. Every field is omitted when nil,
// which is what makes the PATCH partial. The name and the id are not editable.
type WorkspaceUpdate struct {
	Engine           *string `json:"engine,omitempty"`
	EngineVersion    *string `json:"engine_version,omitempty"`
	RunRoleARN       *string `json:"run_role_arn,omitempty"`
	WorkingDirectory *string `json:"working_directory,omitempty"`
	Description      *string `json:"description,omitempty"`
}

// WorkspaceList is the envelope every workspace listing returns.
type WorkspaceList struct {
	Items []Workspace `json:"items"`
}

// Variable is a stored variable as the API renders it. Value is nil for a
// sensitive variable on every route.
type Variable struct {
	WorkspaceID string  `json:"workspace_id"`
	Key         string  `json:"key"`
	Value       *string `json:"value"`
	Category    string  `json:"category"`
	Sensitive   bool    `json:"sensitive"`
	Description string  `json:"description"`
	CreatedAt   string  `json:"created_at"`
	UpdatedAt   *string `json:"updated_at"`
}

// VariableWrite is the body of a variable set.
type VariableWrite struct {
	Value       string `json:"value"`
	Category    string `json:"category,omitempty"`
	Sensitive   bool   `json:"sensitive"`
	Description string `json:"description,omitempty"`
}

// VariableList is the envelope every variable listing returns.
type VariableList struct {
	Items []Variable `json:"items"`
}

// RunRoleCheck is the outcome of one AssumeRole against a workspace's run role.
type RunRoleCheck struct {
	Connected bool    `json:"connected"`
	AccountID *string `json:"account_id"`
	Error     *string `json:"error"`
}

// Engine values a workspace may carry.
const (
	EngineTerraform = "terraform"
	EngineTofu      = "tofu"
)

// Variable category values. The API spells the process environment category
// "env", not "environment".
const (
	CategoryTerraform = "terraform"
	CategoryEnv       = "env"
)
