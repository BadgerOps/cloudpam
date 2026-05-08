//go:build postgres

package auth

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRoleStore struct {
	pool    *pgxpool.Pool
	ownPool bool
	users   UserStore
}

func NewPostgresRoleStore(connStr string, users UserStore) (*PostgresRoleStore, error) {
	pool, err := pgxpool.New(context.Background(), connStr)
	if err != nil {
		return nil, err
	}
	return &PostgresRoleStore{pool: pool, ownPool: true, users: users}, nil
}

func (s *PostgresRoleStore) Close() error {
	if s.ownPool {
		s.pool.Close()
	}
	return nil
}

func (s *PostgresRoleStore) ListPermissions(ctx context.Context) ([]PermissionDefinition, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, name, COALESCE(description, ''), category FROM permissions ORDER BY category, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var defs []PermissionDefinition
	for rows.Next() {
		var def PermissionDefinition
		if err := rows.Scan(&def.ID, &def.Name, &def.Description, &def.Category); err != nil {
			return nil, err
		}
		if perm, ok := PermissionFromID(def.ID); ok {
			def.Resource = perm.Resource
			def.Action = perm.Action
		}
		defs = append(defs, def)
	}
	return defs, rows.Err()
}

func (s *PostgresRoleStore) ListRoles(ctx context.Context) ([]*RoleDefinition, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, name, COALESCE(description, ''), is_builtin, created_at, updated_at
		FROM roles
		WHERE organization_id IS NULL
		ORDER BY is_builtin DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var roles []*RoleDefinition
	for rows.Next() {
		role, err := scanPostgresRoleMetadata(rows)
		if err != nil {
			return nil, err
		}
		role.Permissions, err = s.rolePermissionsByID(ctx, role.ID)
		if err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (s *PostgresRoleStore) GetRole(ctx context.Context, name Role) (*RoleDefinition, error) {
	name = NormalizeRoleName(string(name))
	role, err := scanPostgresRoleMetadata(s.pool.QueryRow(ctx, `
		SELECT id, name, COALESCE(description, ''), is_builtin, created_at, updated_at
		FROM roles WHERE organization_id IS NULL AND name = $1`, string(name)))
	if err != nil {
		return nil, err
	}
	role.Permissions, err = s.rolePermissionsByID(ctx, role.ID)
	if err != nil {
		return nil, err
	}
	return role, nil
}

func (s *PostgresRoleStore) CreateRole(ctx context.Context, role *RoleDefinition) error {
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
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		INSERT INTO roles (organization_id, name, description, is_builtin)
		VALUES (NULL, $1, $2, FALSE)
		RETURNING id`, string(role.Name), strings.TrimSpace(role.Description)).Scan(&id)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrRoleExists
		}
		return err
	}
	if err := insertPostgresRolePermissions(ctx, tx, id.String(), role.Permissions); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *PostgresRoleStore) UpdateRole(ctx context.Context, name Role, description string, permissions []Permission) (*RoleDefinition, error) {
	name = NormalizeRoleName(string(name))
	if IsBuiltinRole(name) {
		return nil, ErrBuiltinRole
	}
	if err := ValidatePermissions(permissions); err != nil {
		return nil, err
	}
	role, err := s.GetRole(ctx, name)
	if err != nil {
		return nil, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE roles SET description = $2 WHERE id = $1 AND is_builtin = FALSE`, role.ID, strings.TrimSpace(description))
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrRoleNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM role_permissions WHERE role_id = $1`, role.ID); err != nil {
		return nil, err
	}
	if err := insertPostgresRolePermissions(ctx, tx, role.ID, permissions); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return s.GetRole(ctx, name)
}

func (s *PostgresRoleStore) DeleteRole(ctx context.Context, name Role) error {
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
	tag, err := s.pool.Exec(ctx, `DELETE FROM roles WHERE organization_id IS NULL AND name = $1 AND is_builtin = FALSE`, string(name))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrRoleNotFound
	}
	return nil
}

func (s *PostgresRoleStore) RoleAssignedToActiveUsers(ctx context.Context, name Role) (bool, error) {
	if s.users != nil {
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
	var count int
	if err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM users WHERE role = $1 AND is_active = TRUE`, string(name)).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

type postgresRoleScanner interface {
	Scan(dest ...any) error
}

func scanPostgresRoleMetadata(row postgresRoleScanner) (*RoleDefinition, error) {
	var id uuid.UUID
	var name string
	var createdAt, updatedAt time.Time
	role := RoleDefinition{}
	if err := row.Scan(&id, &name, &role.Description, &role.IsBuiltin, &createdAt, &updatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrRoleNotFound
		}
		return nil, err
	}
	role.ID = id.String()
	role.Name = Role(name)
	role.CreatedAt = createdAt
	role.UpdatedAt = updatedAt
	return &role, nil
}

func (s *PostgresRoleStore) rolePermissionsByID(ctx context.Context, roleID string) ([]Permission, error) {
	rows, err := s.pool.Query(ctx, `SELECT permission_id FROM role_permissions WHERE role_id = $1 ORDER BY permission_id`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var perms []Permission
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if perm, ok := PermissionFromID(id); ok {
			perms = append(perms, perm)
		}
	}
	sort.Slice(perms, func(i, j int) bool { return perms[i].String() < perms[j].String() })
	return perms, rows.Err()
}

func insertPostgresRolePermissions(ctx context.Context, tx pgx.Tx, roleID string, permissions []Permission) error {
	for _, perm := range permissions {
		if _, err := tx.Exec(ctx, `INSERT INTO role_permissions (role_id, permission_id) VALUES ($1::uuid, $2) ON CONFLICT DO NOTHING`, roleID, perm.String()); err != nil {
			return err
		}
	}
	return nil
}
