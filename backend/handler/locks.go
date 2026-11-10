package handler

import (
	"fmt"
	"net/http"
	"sort"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type LocksHandler struct {
	pool *pgxpool.Pool
}

type LockInfo struct {
	BlockedPID      int     `json:"blockedPid"`
	BlockedUser     string  `json:"blockedUser"`
	BlockedDatabase string  `json:"blockedDatabase"`
	BlockedQuery    string  `json:"blockedQuery"`
	BlockingPID     int     `json:"blockingPid"`
	BlockingUser    string  `json:"blockingUser"`
	BlockingDatabase string `json:"blockingDatabase"`
	BlockingQuery   string  `json:"blockingQuery"`
	WaitEventType   *string `json:"waitEventType"`
	WaitEvent       string  `json:"waitEvent"`
	BlockedState    string  `json:"blockedState"`
	BlockedDuration string  `json:"blockedDuration"`
	LockType        string  `json:"lockType"`
	RelationName    string  `json:"relationName"`
}

func NewLocksHandler(pool *pgxpool.Pool) *LocksHandler {
	return &LocksHandler{pool: pool}
}

// GetLocks returns current lock conflicts.
func (h *LocksHandler) GetLocks(w http.ResponseWriter, r *http.Request) {
	locks, err := h.queryLocks(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: locks})
}

func (h *LocksHandler) GetLockTree(w http.ResponseWriter, r *http.Request) {
	locks, err := h.queryLocks(r)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: buildLockTree(locks)})
}

func (h *LocksHandler) queryLocks(r *http.Request) ([]LockInfo, error) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT blocked_locks.pid AS blocked_pid,
		       blocked_activity.usename AS blocked_user,
		       COALESCE(blocked_activity.datname, '') AS blocked_database,
		       LEFT(blocked_activity.query, 200) AS blocked_query,
		       blocking_locks.pid AS blocking_pid,
		       blocking_activity.usename AS blocking_user,
		       COALESCE(blocking_activity.datname, '') AS blocking_database,
		       LEFT(blocking_activity.query, 200) AS blocking_query,
		       blocked_activity.wait_event_type,
		       COALESCE(blocked_activity.wait_event, '') AS wait_event,
		       blocked_activity.state AS blocked_state,
		       now() - blocked_activity.query_start AS blocked_duration,
		       blocked_locks.locktype,
		       COALESCE(c.relname, '')
		FROM pg_catalog.pg_locks blocked_locks
		JOIN pg_catalog.pg_stat_activity blocked_activity ON blocked_activity.pid = blocked_locks.pid
		JOIN pg_catalog.pg_locks blocking_locks
		  ON blocking_locks.locktype = blocked_locks.locktype
		  AND blocking_locks.database IS NOT DISTINCT FROM blocked_locks.database
		  AND blocking_locks.relation IS NOT DISTINCT FROM blocked_locks.relation
		  AND blocking_locks.page IS NOT DISTINCT FROM blocked_locks.page
		  AND blocking_locks.tuple IS NOT DISTINCT FROM blocked_locks.tuple
		  AND blocking_locks.virtualxid IS NOT DISTINCT FROM blocked_locks.virtualxid
		  AND blocking_locks.transactionid IS NOT DISTINCT FROM blocked_locks.transactionid
		  AND blocking_locks.classid IS NOT DISTINCT FROM blocked_locks.classid
		  AND blocking_locks.objid IS NOT DISTINCT FROM blocked_locks.objid
		  AND blocking_locks.objsubid IS NOT DISTINCT FROM blocked_locks.objsubid
		  AND blocking_locks.pid != blocked_locks.pid
		JOIN pg_catalog.pg_stat_activity blocking_activity ON blocking_activity.pid = blocking_locks.pid
		LEFT JOIN pg_catalog.pg_class c ON c.oid = blocked_locks.relation
		WHERE NOT blocked_locks.granted
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var locks []LockInfo
	for rows.Next() {
		var l LockInfo
		var dur any
		if err := rows.Scan(
			&l.BlockedPID, &l.BlockedUser, &l.BlockedDatabase, &l.BlockedQuery,
			&l.BlockingPID, &l.BlockingUser, &l.BlockingDatabase, &l.BlockingQuery,
			&l.WaitEventType, &l.WaitEvent, &l.BlockedState, &dur, &l.LockType, &l.RelationName,
		); err != nil {
			continue
		}
		if dur != nil {
			l.BlockedDuration = fmt.Sprintf("%v", dur)
		}
		locks = append(locks, l)
	}
	return locks, nil
}

type lockTreeNode struct {
	PID            int            `json:"pid"`
	User           string         `json:"user"`
	Database       string         `json:"database"`
	State          string         `json:"state"`
	Query          string         `json:"query"`
	WaitEventType  string         `json:"waitEventType,omitempty"`
	WaitEvent      string         `json:"waitEvent,omitempty"`
	BlockedDuration string        `json:"blockedDuration,omitempty"`
	Children       []*lockTreeNode `json:"children,omitempty"`
	Edges          []map[string]string `json:"edges,omitempty"`
}

func buildLockTree(locks []LockInfo) map[string]any {
	type nodeData struct {
		pid             int
		user            string
		database        string
		state           string
		query           string
		waitEventType   string
		waitEvent       string
		blockedDuration string
	}

	nodes := map[int]nodeData{}
	children := map[int]map[int]LockInfo{}
	blocked := map[int]bool{}
	blockers := map[int]bool{}

	for _, lock := range locks {
		blockers[lock.BlockingPID] = true
		blocked[lock.BlockedPID] = true

		nodes[lock.BlockingPID] = nodeData{
			pid:      lock.BlockingPID,
			user:     lock.BlockingUser,
			database: lock.BlockingDatabase,
			query:    lock.BlockingQuery,
		}
		nodes[lock.BlockedPID] = nodeData{
			pid:             lock.BlockedPID,
			user:            lock.BlockedUser,
			database:        lock.BlockedDatabase,
			state:           lock.BlockedState,
			query:           lock.BlockedQuery,
			waitEventType:   derefString(lock.WaitEventType),
			waitEvent:       lock.WaitEvent,
			blockedDuration: lock.BlockedDuration,
		}
		if children[lock.BlockingPID] == nil {
			children[lock.BlockingPID] = map[int]LockInfo{}
		}
		children[lock.BlockingPID][lock.BlockedPID] = lock
	}

	var rootPIDs []int
	for pid := range blockers {
		if !blocked[pid] {
			rootPIDs = append(rootPIDs, pid)
		}
	}
	if len(rootPIDs) == 0 {
		for pid := range blockers {
			rootPIDs = append(rootPIDs, pid)
		}
	}
	sort.Ints(rootPIDs)

	var build func(pid int, seen map[int]bool) *lockTreeNode
	build = func(pid int, seen map[int]bool) *lockTreeNode {
		data := nodes[pid]
		node := &lockTreeNode{
			PID:             data.pid,
			User:            data.user,
			Database:        data.database,
			State:           data.state,
			Query:           data.query,
			WaitEventType:   data.waitEventType,
			WaitEvent:       data.waitEvent,
			BlockedDuration: data.blockedDuration,
		}
		if seen[pid] {
			return node
		}
		nextSeen := make(map[int]bool, len(seen)+1)
		for k, v := range seen {
			nextSeen[k] = v
		}
		nextSeen[pid] = true

		var childPIDs []int
		for childPID := range children[pid] {
			childPIDs = append(childPIDs, childPID)
		}
		sort.Ints(childPIDs)
		for _, childPID := range childPIDs {
			lock := children[pid][childPID]
			node.Children = append(node.Children, build(childPID, nextSeen))
			node.Edges = append(node.Edges, map[string]string{
				"lockType":     lock.LockType,
				"relationName": lock.RelationName,
				"duration":     lock.BlockedDuration,
			})
		}
		return node
	}

	var roots []*lockTreeNode
	for _, pid := range rootPIDs {
		roots = append(roots, build(pid, map[int]bool{}))
	}

	return map[string]any{
		"roots": roots,
		"flat":  locks,
	}
}

func derefString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
