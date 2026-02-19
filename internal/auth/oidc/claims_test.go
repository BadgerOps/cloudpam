package oidc

import (
	"testing"

	"cloudpam/internal/auth"
)

func TestMapRole_GroupMatch(t *testing.T) {
	claims := Claims{
		Subject: "user1",
		Email:   "user@example.com",
		Groups:  []string{"cloudpam-admins"},
	}
	mapping := map[string]string{"cloudpam-admins": "admin"}

	role := MapRole(claims, mapping, auth.RoleViewer)
	if role != auth.RoleAdmin {
		t.Errorf("expected admin, got %q", role)
	}
}

func TestMapRole_NoMatch(t *testing.T) {
	claims := Claims{
		Subject: "user1",
		Groups:  []string{"other"},
	}
	mapping := map[string]string{"admins": "admin"}

	role := MapRole(claims, mapping, auth.RoleViewer)
	if role != auth.RoleViewer {
		t.Errorf("expected viewer (default), got %q", role)
	}
}

func TestMapRole_HighestWins(t *testing.T) {
	claims := Claims{
		Subject: "user1",
		Groups:  []string{"viewers", "admins"},
	}
	mapping := map[string]string{
		"viewers": "viewer",
		"admins":  "admin",
	}

	role := MapRole(claims, mapping, auth.RoleViewer)
	if role != auth.RoleAdmin {
		t.Errorf("expected admin (highest), got %q", role)
	}
}

func TestMapRole_EmptyGroups(t *testing.T) {
	claims := Claims{
		Subject: "user1",
		Groups:  []string{},
	}
	mapping := map[string]string{"admins": "admin"}

	role := MapRole(claims, mapping, auth.RoleViewer)
	if role != auth.RoleViewer {
		t.Errorf("expected viewer (default), got %q", role)
	}
}

func TestMapRole_EmptyMapping(t *testing.T) {
	claims := Claims{
		Subject: "user1",
		Groups:  []string{"admins"},
	}
	mapping := map[string]string{}

	role := MapRole(claims, mapping, auth.RoleViewer)
	if role != auth.RoleViewer {
		t.Errorf("expected viewer (default), got %q", role)
	}
}

func TestMapRole_NilMapping(t *testing.T) {
	claims := Claims{
		Subject: "user1",
		Groups:  []string{"x"},
	}

	role := MapRole(claims, nil, auth.RoleViewer)
	if role != auth.RoleViewer {
		t.Errorf("expected viewer (default), got %q", role)
	}
}
