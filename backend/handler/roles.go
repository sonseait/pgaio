package handler

import (
	"net/http"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type RolesHandler struct {
	pool *pgxpool.Pool
}

func NewRolesHandler(pool *pgxpool.Pool) *RolesHandler {
	return &RolesHandler{pool: pool}
}

func (h *RolesHandler) GetOverview(w http.ResponseWriter, r *http.Request) {
	type RoleInfo struct {
		Name         string   `json:"name"`
		Superuser    bool     `json:"superuser"`
		Inherit      bool     `json:"inherit"`
		CreateRole   bool     `json:"createRole"`
		CreateDB     bool     `json:"createDb"`
		CanLogin     bool     `json:"canLogin"`
		Replication  bool     `json:"replication"`
		BypassRLS    bool     `json:"bypassRls"`
		ConnLimit    int      `json:"connLimit"`
		MemberOf     []string `json:"memberOf"`
	}

	type Membership struct {
		Member      string `json:"member"`
		Role        string `json:"role"`
		AdminOption bool   `json:"adminOption"`
	}

	type Grant struct {
		Grantee     string `json:"grantee"`
		ObjectType  string `json:"objectType"`
		Database    string `json:"database,omitempty"`
		Schema      string `json:"schema,omitempty"`
		ObjectName  string `json:"objectName,omitempty"`
		Privilege   string `json:"privilege"`
		IsGrantable bool   `json:"isGrantable"`
	}

	roles := []RoleInfo{}
	roleRows, err := h.pool.Query(r.Context(), `
		SELECT
			r.rolname,
			r.rolsuper,
			r.rolinherit,
			r.rolcreaterole,
			r.rolcreatedb,
			r.rolcanlogin,
			r.rolreplication,
			r.rolbypassrls,
			r.rolconnlimit,
			COALESCE(array_remove(array_agg(parent.rolname ORDER BY parent.rolname), NULL), '{}') AS member_of
		FROM pg_roles r
		LEFT JOIN pg_auth_members am ON am.member = r.oid
		LEFT JOIN pg_roles parent ON parent.oid = am.roleid
		GROUP BY r.oid, r.rolname, r.rolsuper, r.rolinherit, r.rolcreaterole, r.rolcreatedb,
		         r.rolcanlogin, r.rolreplication, r.rolbypassrls, r.rolconnlimit
		ORDER BY r.rolname
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	for roleRows.Next() {
		var role RoleInfo
		if err := roleRows.Scan(
			&role.Name,
			&role.Superuser,
			&role.Inherit,
			&role.CreateRole,
			&role.CreateDB,
			&role.CanLogin,
			&role.Replication,
			&role.BypassRLS,
			&role.ConnLimit,
			&role.MemberOf,
		); err == nil {
			roles = append(roles, role)
		}
	}
	roleRows.Close()

	memberships := []Membership{}
	memberRows, err := h.pool.Query(r.Context(), `
		SELECT member.rolname, parent.rolname, am.admin_option
		FROM pg_auth_members am
		JOIN pg_roles parent ON parent.oid = am.roleid
		JOIN pg_roles member ON member.oid = am.member
		ORDER BY member.rolname, parent.rolname
	`)
	if err == nil {
		for memberRows.Next() {
			var membership Membership
			if err := memberRows.Scan(&membership.Member, &membership.Role, &membership.AdminOption); err == nil {
				memberships = append(memberships, membership)
			}
		}
		memberRows.Close()
	}

	grants := []Grant{}
	appendGrantRows := func(rowsErr error, rows interface {
		Next() bool
		Scan(dest ...any) error
		Close()
	}) {
		if rowsErr != nil {
			return
		}
		defer rows.Close()
		for rows.Next() {
			var grant Grant
			if err := rows.Scan(&grant.Grantee, &grant.ObjectType, &grant.Database, &grant.Schema, &grant.ObjectName, &grant.Privilege, &grant.IsGrantable); err == nil {
				grants = append(grants, grant)
			}
		}
	}

	dbGrantRows, err := h.pool.Query(r.Context(), `
		SELECT
			COALESCE(grantee.rolname, 'PUBLIC'),
			'database' AS object_type,
			d.datname,
			'' AS schema_name,
			'' AS object_name,
			acl.privilege_type,
			acl.is_grantable
		FROM pg_database d
		CROSS JOIN LATERAL aclexplode(COALESCE(d.datacl, acldefault('d', d.datdba))) acl
		LEFT JOIN pg_roles grantee ON grantee.oid = acl.grantee
		WHERE d.datistemplate = false
		ORDER BY grantee.rolname, d.datname, acl.privilege_type
	`)
	appendGrantRows(err, dbGrantRows)

	schemaGrantRows, err := h.pool.Query(r.Context(), `
		SELECT
			COALESCE(grantee.rolname, 'PUBLIC'),
			'schema' AS object_type,
			current_database(),
			n.nspname,
			'' AS object_name,
			acl.privilege_type,
			acl.is_grantable
		FROM pg_namespace n
		CROSS JOIN LATERAL aclexplode(COALESCE(n.nspacl, acldefault('n', n.nspowner))) acl
		LEFT JOIN pg_roles grantee ON grantee.oid = acl.grantee
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		ORDER BY 1, 4, 6
	`)
	appendGrantRows(err, schemaGrantRows)

	tableGrantRows, err := h.pool.Query(r.Context(), `
		SELECT
			grantee,
			'table' AS object_type,
			table_catalog,
			table_schema,
			table_name,
			privilege_type,
			is_grantable = 'YES'
		FROM information_schema.role_table_grants
		WHERE table_schema NOT IN ('pg_catalog', 'information_schema')
		ORDER BY grantee, table_schema, table_name, privilege_type
	`)
	appendGrantRows(err, tableGrantRows)

	defaultPrivileges := []Grant{}
	defaultRows, err := h.pool.Query(r.Context(), `
		SELECT
			owner.rolname,
			CASE d.defaclobjtype
				WHEN 'r' THEN 'default_table'
				WHEN 'S' THEN 'default_sequence'
				WHEN 'f' THEN 'default_function'
				WHEN 'T' THEN 'default_type'
				WHEN 'n' THEN 'default_schema'
				ELSE 'default_other'
			END AS object_type,
			current_database(),
			COALESCE(n.nspname, '(all schemas)'),
			COALESCE(grantee.rolname, 'PUBLIC'),
			acl.privilege_type,
			acl.is_grantable
		FROM pg_default_acl d
		JOIN pg_roles owner ON owner.oid = d.defaclrole
		LEFT JOIN pg_namespace n ON n.oid = d.defaclnamespace
		CROSS JOIN LATERAL aclexplode(d.defaclacl) acl
		LEFT JOIN pg_roles grantee ON grantee.oid = acl.grantee
		ORDER BY owner.rolname, 2, 4, 5, 6
	`)
	if err == nil {
		defer defaultRows.Close()
		for defaultRows.Next() {
			var grant Grant
			if err := defaultRows.Scan(&grant.Grantee, &grant.ObjectType, &grant.Database, &grant.Schema, &grant.ObjectName, &grant.Privilege, &grant.IsGrantable); err == nil {
				defaultPrivileges = append(defaultPrivileges, grant)
			}
		}
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: map[string]any{
		"roles":             roles,
		"memberships":       memberships,
		"grants":            grants,
		"defaultPrivileges": defaultPrivileges,
	}})
}
