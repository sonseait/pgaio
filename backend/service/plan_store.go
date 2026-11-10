package service

import (
	"encoding/json"
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type SavedPlan struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Query     string          `json:"query"`
	Database  string          `json:"database,omitempty"`
	Profile   string          `json:"profile,omitempty"`
	Mode      string          `json:"mode"`
	CreatedAt time.Time       `json:"createdAt"`
	Plan      json.RawMessage `json:"plan"`
}

type PlanStore struct {
	mu     sync.RWMutex
	plans  map[string]*SavedPlan
	order  []string
	limit  int
	seq    atomic.Uint64
}

func NewPlanStore() *PlanStore {
	return &PlanStore{
		plans: make(map[string]*SavedPlan),
		limit: 100,
	}
}

func (s *PlanStore) Save(name, query, database, profile, mode string, plan json.RawMessage) *SavedPlan {
	now := time.Now()
	entry := &SavedPlan{
		ID:        fmt.Sprintf("plan-%d-%06d", now.Unix(), s.seq.Add(1)),
		Name:      name,
		Query:     query,
		Database:  database,
		Profile:   profile,
		Mode:      mode,
		CreatedAt: now,
		Plan:      append(json.RawMessage(nil), plan...),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.plans[entry.ID] = entry
	s.order = append([]string{entry.ID}, s.order...)
	for len(s.order) > s.limit {
		last := s.order[len(s.order)-1]
		delete(s.plans, last)
		s.order = s.order[:len(s.order)-1]
	}
	return cloneSavedPlan(entry)
}

func (s *PlanStore) List(limit int) []SavedPlan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if limit <= 0 || limit > s.limit {
		limit = 50
	}
	out := make([]SavedPlan, 0, limit)
	for _, id := range s.order {
		plan, ok := s.plans[id]
		if !ok {
			continue
		}
		out = append(out, *cloneSavedPlan(plan))
		if len(out) >= limit {
			break
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

func (s *PlanStore) Get(id string) *SavedPlan {
	s.mu.RLock()
	defer s.mu.RUnlock()
	plan, ok := s.plans[id]
	if !ok {
		return nil
	}
	return cloneSavedPlan(plan)
}

func cloneSavedPlan(in *SavedPlan) *SavedPlan {
	if in == nil {
		return nil
	}
	cp := *in
	cp.Plan = append(json.RawMessage(nil), in.Plan...)
	return &cp
}
