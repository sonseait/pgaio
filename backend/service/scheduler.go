package service

import (
	"context"
	"log"
	"sync"
	"time"
)

// Scheduler manages independent full and incremental WAL-G backup schedules.
type Scheduler struct {
	walg               *WalG
	config             *ConfigStore
	mu                 sync.Mutex
	cancel             context.CancelFunc
	lastFullRun        time.Time
	lastIncrementalRun time.Time
	nextFullRun        time.Time
	nextIncrementalRun time.Time
}

func NewScheduler(walg *WalG, config *ConfigStore) *Scheduler {
	s := &Scheduler{walg: walg, config: config}
	s.Start()
	return s
}

// Start begins the full and incremental backup loops.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}

	cfg := s.config.GetBackup()
	incrementalHours := cfg.IncrementalIntervalHours
	if incrementalHours <= 0 {
		incrementalHours = cfg.IntervalHours // Migrate existing persisted settings.
	}
	if !cfg.Enabled || incrementalHours <= 0 {
		log.Println("Backup scheduler disabled or missing full/incremental intervals")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	incrementalInterval := time.Duration(incrementalHours) * time.Hour
	s.nextIncrementalRun = time.Now().Add(incrementalInterval)
	s.nextFullRun = nextWeeklyRun(time.Now(), cfg.FullWeekday, cfg.FullHour)
	log.Printf("Backup scheduler started (incremental every %dh; full every %s at %02d:00)", incrementalHours, time.Weekday(cfg.FullWeekday), cfg.FullHour)

	go s.run(ctx, incrementalInterval, s.nextFullRun)
}

func (s *Scheduler) run(ctx context.Context, incrementalInterval time.Duration, nextFull time.Time) {
	incrementalTicker := time.NewTicker(incrementalInterval)
	fullTimer := time.NewTimer(time.Until(nextFull))
	defer incrementalTicker.Stop()
	defer fullTimer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-fullTimer.C:
			s.runFullBackup()
			cfg := s.config.GetBackup()
			fullTimer.Reset(time.Until(nextWeeklyRun(time.Now(), cfg.FullWeekday, cfg.FullHour)))
		case <-incrementalTicker.C:
			s.runIncrementalBackup()
		}
	}
}

func (s *Scheduler) runFullBackup() {
	log.Println("Scheduled full backup starting...")
	resp, err := s.walg.TriggerFullBackup(context.Background())
	s.mu.Lock()
	s.lastFullRun = time.Now()
	cfg := s.config.GetBackup()
	s.nextFullRun = nextWeeklyRun(time.Now(), cfg.FullWeekday, cfg.FullHour)
	s.mu.Unlock()
	if err != nil {
		log.Printf("Scheduled full backup failed to start: %v", err)
	} else {
		log.Printf("Scheduled full backup accepted (job: %s)", resp.JobID)
	}
}

func (s *Scheduler) runIncrementalBackup() {
	log.Println("Scheduled incremental backup starting...")
	resp, err := s.walg.TriggerIncrementalBackup(context.Background())
	cfg := s.config.GetBackup()
	hours := cfg.IncrementalIntervalHours
	if hours <= 0 {
		hours = cfg.IntervalHours
	}
	s.mu.Lock()
	s.lastIncrementalRun = time.Now()
	s.nextIncrementalRun = time.Now().Add(time.Duration(hours) * time.Hour)
	s.mu.Unlock()
	if err != nil {
		log.Printf("Scheduled incremental backup failed to start: %v", err)
	} else {
		log.Printf("Scheduled incremental backup accepted (job: %s)", resp.JobID)
	}
}

// Restart re-reads config and restarts the scheduler.
func (s *Scheduler) Restart() { s.Start() }

// Status returns scheduler status info.
func (s *Scheduler) Status() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	cfg := s.config.GetBackup()
	incrementalHours := cfg.IncrementalIntervalHours
	if incrementalHours <= 0 {
		incrementalHours = cfg.IntervalHours
	}
	status := map[string]any{
		"enabled":                  cfg.Enabled,
		"incrementalIntervalHours": incrementalHours,
		"fullWeekday":              cfg.FullWeekday,
		"fullHour":                 cfg.FullHour,
		"retainCount":              cfg.RetainCount,
	}
	if !s.lastFullRun.IsZero() {
		status["lastFullRun"] = s.lastFullRun
	}
	if !s.lastIncrementalRun.IsZero() {
		status["lastIncrementalRun"] = s.lastIncrementalRun
	}
	if cfg.Enabled {
		status["nextFullRun"] = s.nextFullRun
		status["nextIncrementalRun"] = s.nextIncrementalRun
	}
	return status
}

// nextWeeklyRun returns the next requested weekday/hour in GMT+7. A fixed
// offset avoids relying on tzdata being installed in the runtime image.
func nextWeeklyRun(now time.Time, weekday, hour int) time.Time {
	now = now.In(time.FixedZone("GMT+7", 7*60*60))
	if weekday < int(time.Sunday) || weekday > int(time.Saturday) {
		weekday = int(time.Sunday)
	}
	if hour < 0 || hour > 23 {
		hour = 4
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, now.Location())
	days := (weekday - int(now.Weekday()) + 7) % 7
	next = next.AddDate(0, 0, days)
	if !next.After(now) {
		next = next.AddDate(0, 0, 7)
	}
	return next
}
