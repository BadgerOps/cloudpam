//go:build sqlite

package auth

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type SQLiteRoleStore struct {
	db    *sql.DB
	users UserStore
}

func NewSQLiteRoleStore(dsn string, users UserStore) (*SQLiteRoleStore, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set pragmas: %w", err)
	}
	return &SQLiteRoleStore{db: db, users: users}, nil
}

func (s *SQLiteRoleStore) Close() error { return s.db.Close() }

func (s *SQLiteRoleStore) ListPermissions(ctx context.Context) ([]PermissionDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, COALESCE(description, ''), category FROM permissions ORDER BY category, id`)
	if err != nil {
		return nil, fmt.Errorf("query permissions: %w", err)
	}
	defer rows.Close()
	var defs []PermissionDefinition
	for rows.Next() {
		var def PermissionDefinition
		if err := rows.Scan(&def.ID, &def.Name, &def.Description, &def.Category); err != nil {
			return nil, fmt.Errorf("scan permission: %w", err)
		}
		if perm, ok := PermissionFromID(def.ID); ok {
			def.Resource = perm.Resource
			def.Action = perm.Action
		}
		defs = append(defs, def)
	}
	return defs, rows.Err()
}

func (s *SQLiteRoleStore) ListRoles(ctx context.Context) ([]*RoleDefinition, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, COALESCE(description, ''), is_builtin, created_at, updated_at
		FROM roles
		WHERE organization_id IS NULL
		ORDER BY is_builtin DESC, name`)
	if err != nil {
		return nil, fmt.Errorf("query roles: %w", err)
	}
	defer rows.Close()

	var roles []*RoleDefinition
	for rows.Next() {
		role, err := s.scanRoleMetadata(rows)
		if err != nil {
			return nil, err
		}
		perms, err := s.rolePermissionsByID(ctx, role.ID)
		if err != nil {
			return nil, err
		}
		role.Permissions = perms
		roles = append(roles, role)
	}
	return roles, rows.Err()
}

func (s *SQLiteRoleStore) GetRole(ctx context.Context, name Role) (*RoleDefinition, error) {
	name = NormalizeRoleName(string(name))
	row := s.db.QueryRowContext(ctx, `
		SELECT id, name, COALESCE(description, ''), is_builtin, created_at, updated_at
		FROM roles WHERE name = ? AND organization_id IS NULL`, string(name))
	role, err := s.scanRoleMetadata(row)
	if err != nil {
		return nil, err
	}
	role.Permissions, err = s.rolePermissionsByID(ctx, role.ID)
	if err != nil {
		return nil, err
	}
	return role, nil
}

func (s *SQLiteRoleStore) CreateRole(ctx context.Context, role *RoleDefinition) error {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.ExecContext(ctx, `
		INSERT INTO roles (organization_id, name, description, is_builtin, created_at, updated_at)
		VALUES (NULL, ?, ?, 0, ?, ?)`, string(role.Name), strings.TrimSpace(role.Description), now, now)
	if err != nil {
		if isUniqueViolation(err) {
			return ErrRoleExists
		}
		return fmt.Errorf("insert role: %w", err)
	}
	roleID, _ := res.LastInsertId()
	if err := insertSQLiteRolePermissions(ctx, tx, strconv.FormatInt(roleID, 10), role.Permissions); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *SQLiteRoleStore) UpdateRole(ctx context.Context, name Role, description string, permissions []Permission) (*RoleDefinition, error) {
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
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE roles SET description = ?, updated_at = ? WHERE id = ? AND is_builtin = 0`,
		strings.TrimSpace(description), time.Now().UTC().Format(time.RFC3339Nano), role.ID); err != nil {
		return nil, fmt.Errorf("update role: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM role_permissions WHERE role_id = ?`, role.ID); err != nil {
		return nil, fmt.Errorf("delete role permissions: %w", err)
	}
	if err := insertSQLiteRolePermissions(ctx, tx, role.ID, permissions); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetRole(ctx, name)
}

func (s *SQLiteRoleStore) DeleteRole(ctx context.Context, name Role) error {
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
	res, err := s.db.ExecContext(ctx, `DELETE FROM roles WHERE name = ? AND organization_id IS NULL AND is_builtin = 0`, string(name))
	if err != nil {
		return fmt.Errorf("delete role: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrRoleNotFound
	}
	return nil
}

func (s *SQLiteRoleStore) RoleAssignedToActiveUsers(ctx context.Context, name Role) (bool, error) {
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
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE role = ? AND is_active = 1`, string(name)).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

type roleScanner interface {
	Scan(dest ...any) error
}

func (s *SQLiteRoleStore) scanRoleMetadata(row roleScanner) (*RoleDefinition, error) {
	var id int64
	var role RoleDefinition
	var name string
	var isBuiltin int
	var createdAt, updatedAt string
	if err := row.Scan(&id, &name, &role.Description, &isBuiltin, &createdAt, &updatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, ErrRoleNotFound
		}
		return nil, fmt.Errorf("scan role: %w", err)
	}
	role.ID = strconv.FormatInt(id, 10)
	role.Name = Role(name)
	role.IsBuiltin = isBuiltin != 0
	role.CreatedAt, _ = time.Parse(time.RFC3339Nano, createdAt)
	role.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updatedAt)
	return &role, nil
}

func (s *SQLiteRoleStore) rolePermissionsByID(ctx context.Context, roleID string) ([]Permission, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT permission_id FROM role_permissions WHERE role_id = ? ORDER BY permission_id`, roleID)
	if err != nil {
		return nil, fmt.Errorf("query role permissions: %w", err)
	}
	defer rows.Close()
	var perms []Permission
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan role permission: %w", err)
		}
		if perm, ok := PermissionFromID(id); ok {
			perms = append(perms, perm)
		}
	}
	sort.Slice(perms, func(i, j int) bool { return perms[i].String() < perms[j].String() })
	return perms, rows.Err()
}

func insertSQLiteRolePermissions(ctx context.Context, tx *sql.Tx, roleID string, permissions []Permission) error {
	for _, perm := range permissions {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO role_permissions (role_id, permission_id) VALUES (?, ?)`, roleID, perm.String()); err != nil {
			return fmt.Errorf("insert role permission: %w", err)
		}
	}
	return nil
}
