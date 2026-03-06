package service

import (
	"context"
	"log"
	"sync"
	"time"
)

// Scheduler manages periodic WAL-G backups.
type Scheduler struct {
	walg    *WalG
	config  *ConfigStore
	mu      sync.Mutex
	cancel  context.CancelFunc
	lastRun time.Time
	nextRun time.Time
}

func NewScheduler(walg *WalG, config *ConfigStore) *Scheduler {
	s := &Scheduler{walg: walg, config: config}
	s.Start()
	return s
}

// Start begins the backup scheduler loop.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Cancel previous if running
	if s.cancel != nil {
		s.cancel()
	}

	cfg := s.config.GetBackup()
	if !cfg.Enabled || cfg.IntervalHours <= 0 {
		log.Println("⏰ Backup scheduler disabled")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	interval := time.Duration(cfg.IntervalHours) * time.Hour
	s.nextRun = time.Now().Add(interval)

	log.Printf("⏰ Backup scheduler started (every %dh)", cfg.IntervalHours)

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.runBackup()
				// Recalculate in case config changed
				newCfg := s.config.GetBackup()
				if !newCfg.Enabled {
					log.Println("⏰ Backup scheduler stopped (disabled)")
					return
				}
				if newCfg.IntervalHours != cfg.IntervalHours {
					// Restart with new interval
					ticker.Reset(time.Duration(newCfg.IntervalHours) * time.Hour)
					cfg = newCfg
				}
				s.mu.Lock()
				s.nextRun = time.Now().Add(time.Duration(cfg.IntervalHours) * time.Hour)
				s.mu.Unlock()
			}
		}
	}()
}

func (s *Scheduler) runBackup() {
	log.Println("⏰ Scheduled backup starting...")
	_, err := s.walg.TriggerBackup(context.Background())
	s.mu.Lock()
	s.lastRun = time.Now()
	s.mu.Unlock()
	if err != nil {
		log.Printf("⏰ Scheduled backup failed: %v", err)
	} else {
		log.Println("⏰ Scheduled backup completed")
	}
}

// Restart re-reads config and restarts the scheduler.
func (s *Scheduler) Restart() {
	s.Start()
}

// Status returns scheduler status info.
func (s *Scheduler) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.config.GetBackup()
	status := map[string]any{
		"enabled":       cfg.Enabled,
		"intervalHours": cfg.IntervalHours,
		"retainCount":   cfg.RetainCount,
	}
	if !s.lastRun.IsZero() {
		status["lastRun"] = s.lastRun
	}
	if !s.nextRun.IsZero() && cfg.Enabled {
		status["nextRun"] = s.nextRun
	}
	return status
}
