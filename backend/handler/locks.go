package handler

import (
	"fmt"
	"net/http"

	"pgaio/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

type LocksHandler struct {
	pool *pgxpool.Pool
}

func NewLocksHandler(pool *pgxpool.Pool) *LocksHandler {
	return &LocksHandler{pool: pool}
}

// GetLocks returns current lock conflicts.
func (h *LocksHandler) GetLocks(w http.ResponseWriter, r *http.Request) {
	rows, err := h.pool.Query(r.Context(), `
		SELECT blocked_locks.pid AS blocked_pid,
		       blocked_activity.usename AS blocked_user,
		       LEFT(blocked_activity.query, 200) AS blocked_query,
		       blocking_locks.pid AS blocking_pid,
		       blocking_activity.usename AS blocking_user,
		       LEFT(blocking_activity.query, 200) AS blocking_query,
		       blocked_activity.wait_event_type,
		       blocked_activity.state AS blocked_state,
		       now() - blocked_activity.query_start AS blocked_duration
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
		WHERE NOT blocked_locks.granted
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, model.APIResponse{Error: err.Error()})
		return
	}
	defer rows.Close()

	type LockInfo struct {
		BlockedPID      int     `json:"blockedPid"`
		BlockedUser     string  `json:"blockedUser"`
		BlockedQuery    string  `json:"blockedQuery"`
		BlockingPID     int     `json:"blockingPid"`
		BlockingUser    string  `json:"blockingUser"`
		BlockingQuery   string  `json:"blockingQuery"`
		WaitEventType   *string `json:"waitEventType"`
		BlockedState    string  `json:"blockedState"`
		BlockedDuration string  `json:"blockedDuration"`
	}

	var locks []LockInfo
	for rows.Next() {
		var l LockInfo
		var dur any
		if err := rows.Scan(&l.BlockedPID, &l.BlockedUser, &l.BlockedQuery,
			&l.BlockingPID, &l.BlockingUser, &l.BlockingQuery,
			&l.WaitEventType, &l.BlockedState, &dur); err != nil {
			continue
		}
		if dur != nil {
			l.BlockedDuration = fmt.Sprintf("%v", dur)
		}
		locks = append(locks, l)
	}

	writeJSON(w, http.StatusOK, model.APIResponse{Success: true, Data: locks})
}
