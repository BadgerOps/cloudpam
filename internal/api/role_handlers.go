package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"cloudpam/internal/audit"
	"cloudpam/internal/auth"
)

type RoleServer struct {
	*Server
	roleStore auth.RoleStore
}

func NewRoleServer(s *Server, roleStore auth.RoleStore) *RoleServer {
	return &RoleServer{Server: s, roleStore: roleStore}
}

func (rs *RoleServer) RegisterProtectedRoleRoutes(authMW Middleware, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	readMW := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionRead, logger)
	writeMW := RequirePermissionMiddleware(auth.ResourceSettings, auth.ActionWrite, logger)

	rs.mux.Handle("/api/v1/auth/permissions", authMW(readMW(http.HandlerFunc(rs.listPermissions))))
	rs.mux.Handle("/api/v1/auth/roles", authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			readMW(http.HandlerFunc(rs.listRoles)).ServeHTTP(w, r)
		case http.MethodPost:
			writeMW(http.HandlerFunc(rs.createRole)).ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPost}, ", "))
			rs.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})))
	rs.mux.Handle("/api/v1/auth/roles/", authMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := auth.NormalizeRoleName(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/auth/roles/"), "/"))
		if name == auth.RoleNone {
			rs.writeErr(r.Context(), w, http.StatusNotFound, "not found", "")
			return
		}
		switch r.Method {
		case http.MethodGet:
			readMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { rs.getRole(w, r, name) })).ServeHTTP(w, r)
		case http.MethodPatch:
			writeMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { rs.updateRole(w, r, name) })).ServeHTTP(w, r)
		case http.MethodDelete:
			writeMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { rs.deleteRole(w, r, name) })).ServeHTTP(w, r)
		default:
			w.Header().Set("Allow", strings.Join([]string{http.MethodGet, http.MethodPatch, http.MethodDelete}, ", "))
			rs.writeErr(r.Context(), w, http.StatusMethodNotAllowed, "method not allowed", "")
		}
	})))
}

func (rs *RoleServer) listPermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := rs.roleStore.ListPermissions(r.Context())
	if err != nil {
		rs.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to list permissions", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Permissions []auth.PermissionDefinition `json:"permissions"`
	}{Permissions: perms})
}

func (rs *RoleServer) listRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := rs.roleStore.ListRoles(r.Context())
	if err != nil {
		rs.writeErr(r.Context(), w, http.StatusInternalServerError, "failed to list roles", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, struct {
		Roles []*auth.RoleDefinition `json:"roles"`
	}{Roles: roles})
}

func (rs *RoleServer) getRole(w http.ResponseWriter, r *http.Request, name auth.Role) {
	role, err := rs.roleStore.GetRole(r.Context(), name)
	if err != nil {
		rs.writeRoleErr(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, role)
}

func (rs *RoleServer) createRole(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	input, ok := rs.decodeRoleInput(w, r, true)
	if !ok {
		return
	}
	role := &auth.RoleDefinition{
		Name:        auth.NormalizeRoleName(input.Name),
		Description: input.Description,
		Permissions: input.Permissions,
	}
	if err := rs.roleStore.CreateRole(ctx, role); err != nil {
		rs.writeRoleErr(w, r, err)
		return
	}
	created, err := rs.roleStore.GetRole(ctx, role.Name)
	if err != nil {
		rs.writeErr(ctx, w, http.StatusInternalServerError, "failed to read created role", err.Error())
		return
	}
	rs.logAudit(ctx, audit.ActionCreate, "role", string(created.Name), string(created.Name), http.StatusCreated)
	writeJSON(w, http.StatusCreated, created)
}

func (rs *RoleServer) updateRole(w http.ResponseWriter, r *http.Request, name auth.Role) {
	ctx := r.Context()
	input, ok := rs.decodeRoleInput(w, r, false)
	if !ok {
		return
	}
	role, err := rs.roleStore.UpdateRole(ctx, name, input.Description, input.Permissions)
	if err != nil {
		rs.writeRoleErr(w, r, err)
		return
	}
	rs.logAudit(ctx, audit.ActionUpdate, "role", string(role.Name), string(role.Name), http.StatusOK)
	writeJSON(w, http.StatusOK, role)
}

func (rs *RoleServer) deleteRole(w http.ResponseWriter, r *http.Request, name auth.Role) {
	ctx := r.Context()
	if err := rs.roleStore.DeleteRole(ctx, name); err != nil {
		rs.writeRoleErr(w, r, err)
		return
	}
	rs.logAudit(ctx, audit.ActionDelete, "role", string(name), string(name), http.StatusNoContent)
	w.WriteHeader(http.StatusNoContent)
}

type roleInput struct {
	Name        string
	Description string
	Permissions []auth.Permission
}

func (rs *RoleServer) decodeRoleInput(w http.ResponseWriter, r *http.Request, requireName bool) (roleInput, bool) {
	var raw struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Permissions []string `json:"permissions"`
	}
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		rs.writeErr(r.Context(), w, http.StatusBadRequest, "invalid json", "")
		return roleInput{}, false
	}
	raw.Name = strings.TrimSpace(raw.Name)
	if requireName && raw.Name == "" {
		rs.writeErr(r.Context(), w, http.StatusBadRequest, "name is required", "")
		return roleInput{}, false
	}
	perms := make([]auth.Permission, 0, len(raw.Permissions))
	seen := make(map[string]bool, len(raw.Permissions))
	for _, id := range raw.Permissions {
		perm, ok := auth.PermissionFromID(id)
		if !ok {
			rs.writeErr(r.Context(), w, http.StatusBadRequest, "invalid permission", id)
			return roleInput{}, false
		}
		if seen[perm.String()] {
			continue
		}
		seen[perm.String()] = true
		perms = append(perms, perm)
	}
	return roleInput{Name: raw.Name, Description: strings.TrimSpace(raw.Description), Permissions: perms}, true
}

func (rs *RoleServer) writeRoleErr(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	switch {
	case errors.Is(err, auth.ErrRoleNotFound):
		rs.writeErr(ctx, w, http.StatusNotFound, err.Error(), "")
	case errors.Is(err, auth.ErrRoleExists):
		rs.writeErr(ctx, w, http.StatusConflict, err.Error(), "")
	case errors.Is(err, auth.ErrBuiltinRole), errors.Is(err, auth.ErrRoleInUse):
		rs.writeErr(ctx, w, http.StatusConflict, err.Error(), "")
	case errors.Is(err, auth.ErrInvalidRole), errors.Is(err, auth.ErrInvalidPermission):
		rs.writeErr(ctx, w, http.StatusBadRequest, err.Error(), "")
	default:
		rs.writeErr(ctx, w, http.StatusInternalServerError, "internal error", err.Error())
	}
}
