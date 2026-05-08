package auth

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrRoleNotFound      = errors.New("role not found")
	ErrRoleExists        = errors.New("role already exists")
	ErrBuiltinRole       = errors.New("built-in roles cannot be modified")
	ErrRoleInUse         = errors.New("role is assigned to active users")
	ErrInvalidRole       = errors.New("invalid role")
	ErrInvalidPermission = errors.New("invalid permission")
)

type PermissionDefinition struct {
	ID          string `json:"id"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
}

type RoleDefinition struct {
	ID          string       `json:"id"`
	Name        Role         `json:"name"`
	Description string       `json:"description"`
	IsBuiltin   bool         `json:"is_builtin"`
	Permissions []Permission `json:"permissions"`
	CreatedAt   time.Time    `json:"created_at,omitempty"`
	UpdatedAt   time.Time    `json:"updated_at,omitempty"`
}

type RoleStore interface {
	ListPermissions(ctx context.Context) ([]PermissionDefinition, error)
	ListRoles(ctx context.Context) ([]*RoleDefinition, error)
	GetRole(ctx context.Context, name Role) (*RoleDefinition, error)
	CreateRole(ctx context.Context, role *RoleDefinition) error
	UpdateRole(ctx context.Context, name Role, description string, permissions []Permission) (*RoleDefinition, error)
	DeleteRole(ctx context.Context, name Role) error
	RoleAssignedToActiveUsers(ctx context.Context, name Role) (bool, error)
}

var roleNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,62}$`)

func NormalizeRoleName(name string) Role {
	return Role(strings.ToLower(strings.TrimSpace(name)))
}

func ValidateCustomRoleName(name Role) error {
	if IsBuiltinRole(name) {
		return ErrBuiltinRole
	}
	if name == RoleNone || !roleNamePattern.MatchString(string(name)) {
		return ErrInvalidRole
	}
	return nil
}

func IsBuiltinRole(role Role) bool {
	switch role {
	case RoleAdmin, RoleOperator, RoleViewer, RoleAuditor:
		return true
	default:
		return false
	}
}

func BuiltinRoleDefinition(role Role) *RoleDefinition {
	desc := map[Role]string{
		RoleAdmin:    "Full application administration",
		RoleOperator: "Manage pools, accounts, and discovery workflows",
		RoleViewer:   "Read-only access to pools, accounts, and discovery",
		RoleAuditor:  "Read-only audit log access",
	}
	perms := GetStaticPermissions(role)
	if perms == nil {
		return nil
	}
	return &RoleDefinition{
		ID:          string(role),
		Name:        role,
		Description: desc[role],
		IsBuiltin:   true,
		Permissions: perms,
	}
}

type MemoryRoleStore struct {
	mu    sync.RWMutex
	roles map[Role]*RoleDefinition
	users UserStore
}

func NewMemoryRoleStore(users ...UserStore) *MemoryRoleStore {
	s := &MemoryRoleStore{roles: make(map[Role]*RoleDefinition)}
	if len(users) > 0 {
		s.users = users[0]
	}
	for _, role := range ValidRoles() {
		if def := BuiltinRoleDefinition(role); def != nil {
			s.roles[role] = copyRoleDefinition(def)
		}
	}
	return s
}

func (s *MemoryRoleStore) ListPermissions(context.Context) ([]PermissionDefinition, error) {
	return PermissionCatalog(), nil
}

func (s *MemoryRoleStore) ListRoles(context.Context) ([]*RoleDefinition, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	roles := make([]*RoleDefinition, 0, len(s.roles))
	for _, role := range s.roles {
		roles = append(roles, copyRoleDefinition(role))
	}
	sort.Slice(roles, func(i, j int) bool {
		if roles[i].IsBuiltin != roles[j].IsBuiltin {
			return roles[i].IsBuiltin
		}
		return roles[i].Name < roles[j].Name
	})
	return roles, nil
}

func (s *MemoryRoleStore) GetRole(_ context.Context, name Role) (*RoleDefinition, error) {
	name = NormalizeRoleName(string(name))
	s.mu.RLock()
	defer s.mu.RUnlock()
	role := s.roles[name]
	if role == nil {
		return nil, ErrRoleNotFound
	}
	return copyRoleDefinition(role), nil
}

func (s *MemoryRoleStore) CreateRole(_ context.Context, role *RoleDefinition) error {
	if role == nil {
		return ErrInvalidRole
	}
	role.Name = NormalizeRoleName(string(role.Name))
	if err := ValidateCustomRoleName(role.Name); err != nil {
		return err
	}
	if err := ValidatePermissions(role.Permissions); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.roles[role.Name]; exists {
		return ErrRoleExists
	}
	now := time.Now().UTC()
	role.ID = string(role.Name)
	role.IsBuiltin = false
	role.CreatedAt = now
	role.UpdatedAt = now
	s.roles[role.Name] = copyRoleDefinition(role)
	return nil
}

func (s *MemoryRoleStore) UpdateRole(_ context.Context, name Role, description string, permissions []Permission) (*RoleDefinition, error) {
	name = NormalizeRoleName(string(name))
	if IsBuiltinRole(name) {
		return nil, ErrBuiltinRole
	}
	if err := ValidatePermissions(permissions); err != nil {
		return nil, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	role := s.roles[name]
	if role == nil {
		return nil, ErrRoleNotFound
	}
	role.Description = strings.TrimSpace(description)
	role.Permissions = copyPermissions(permissions)
	role.UpdatedAt = time.Now().UTC()
	return copyRoleDefinition(role), nil
}

func (s *MemoryRoleStore) DeleteRole(ctx context.Context, name Role) error {
	name = NormalizeRoleName(string(name))
	if IsBuiltinRole(name) {
		return ErrBuiltinRole
	}
	inUse, err := s.RoleAssignedToActiveUsers(ctx, name)
	if err != nil {
		return err
	}
	if inUse {
		return ErrRoleInUse
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.roles[name]; !exists {
		return ErrRoleNotFound
	}
	delete(s.roles, name)
	return nil
}

func (s *MemoryRoleStore) RoleAssignedToActiveUsers(ctx context.Context, name Role) (bool, error) {
	if s.users == nil {
		return false, nil
	}
	users, err := s.users.List(ctx)
	if err != nil {
		return false, err
	}
	for _, user := range users {
		if user.IsActive && user.Role == name {
			return true, nil
		}
	}
	return false, nil
}

func copyRoleDefinition(role *RoleDefinition) *RoleDefinition {
	if role == nil {
		return nil
	}
	return &RoleDefinition{
		ID:          role.ID,
		Name:        role.Name,
		Description: role.Description,
		IsBuiltin:   role.IsBuiltin,
		Permissions: copyPermissions(role.Permissions),
		CreatedAt:   role.CreatedAt,
		UpdatedAt:   role.UpdatedAt,
	}
}
