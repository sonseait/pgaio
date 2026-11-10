package model

import "time"

// PostgreSQL Stats

type PgStat struct {
	Timestamp        time.Time         `json:"timestamp"`
	Databases        []DatabaseStats   `json:"databases"`
	Activity         ActivityStats     `json:"activity"`
	Connections      ConnectionStats   `json:"connections"`
	Replication      []ReplicationLag  `json:"replication"`
	System           SystemStats       `json:"system"`
	WAL              WALStats          `json:"wal"`
	BGWriter         BGWriterStats     `json:"bgwriter"`
	CollectionErrors map[string]string `json:"collectionErrors,omitempty"`
	PgBouncerStats   *PgBouncerStat    `json:"pgbouncer,omitempty"`
	PgBouncerPools   []PgBouncerPool   `json:"pgbouncerPools,omitempty"`
}

type DatabaseStats struct {
	Name             string  `json:"name"`
	Size             string  `json:"size"`
	SizeBytes        int64   `json:"sizeBytes"`
	NumBackends      int     `json:"numBackends"`
	TxCommit         int64   `json:"txCommit"`
	TxRollback       int64   `json:"txRollback"`
	BlksRead         int64   `json:"blksRead"`
	BlksHit          int64   `json:"blksHit"`
	CacheHitRatio    float64 `json:"cacheHitRatio"`
	BlkReadTime      float64 `json:"blkReadTime"`
	BlkWriteTime     float64 `json:"blkWriteTime"`
	TempFiles        int64   `json:"tempFiles"`
	TempBytes        int64   `json:"tempBytes"`
	Deadlocks        int64   `json:"deadlocks"`
	Conflicts        int64   `json:"conflicts"`
	ChecksumFailures int64   `json:"checksumFailures"`
	TupReturned      int64   `json:"tupReturned"`
	TupFetched       int64   `json:"tupFetched"`
	TupInserted      int64   `json:"tupInserted"`
	TupUpdated       int64   `json:"tupUpdated"`
	TupDeleted       int64   `json:"tupDeleted"`
}

type ActivityStats struct {
	TotalConnections   int             `json:"totalConnections"`
	ActiveQueries      int             `json:"activeQueries"`
	IdleConnections    int             `json:"idleConnections"`
	IdleInTransaction  int             `json:"idleInTransaction"`
	OldestIdleInXactMs int64           `json:"oldestIdleInXactMs"`
	LongRunningQueries int             `json:"longRunningQueries"`
	WaitingQueries     int             `json:"waitingQueries"`
	OldestQueryMs      int64           `json:"oldestQueryMs"`
	WaitEvents         []WaitEventStat `json:"waitEvents"`
	Queries            []ActiveQuery   `json:"queries"`
}

type WaitEventStat struct {
	Type  string `json:"type"`
	Event string `json:"event"`
	Count int    `json:"count"`
}

type ActiveQuery struct {
	PID         int       `json:"pid"`
	User        string    `json:"user"`
	Database    string    `json:"database"`
	State       string    `json:"state"`
	Query       string    `json:"query"`
	Duration    int64     `json:"duration"`
	WaitEvent   string    `json:"waitEvent"`
	BackendType string    `json:"backendType"`
	QueryStart  time.Time `json:"queryStart"`
}

type ConnectionStats struct {
	MaxConnections       int `json:"maxConnections"`
	UsedConnections      int `json:"usedConnections"`
	AvailableConnections int `json:"availableConnections"`
	AvailableTotal       int `json:"availableTotal"`
	ReservedConnections  int `json:"reservedConnections"`
}

type ReplicationLag struct {
	ClientAddr       string `json:"clientAddr"`
	State            string `json:"state"`
	SentLagBytes     int64  `json:"sentLagBytes"`
	WriteLagBytes    int64  `json:"writeLagBytes"`
	FlushLagBytes    int64  `json:"flushLagBytes"`
	ReplayLagBytes   int64  `json:"replayLagBytes"`
	ReplayLagSeconds int64  `json:"replayLagSeconds"`
}

type SystemStats struct {
	CPUUsage  float64 `json:"cpuUsage"`
	MemTotal  uint64  `json:"memTotal"`
	MemUsed   uint64  `json:"memUsed"`
	MemFree   uint64  `json:"memFree"`
	MemUsage  float64 `json:"memUsage"`
	DiskTotal uint64  `json:"diskTotal"`
	DiskUsed  uint64  `json:"diskUsed"`
	DiskFree  uint64  `json:"diskFree"`
	DiskUsage float64 `json:"diskUsage"`
	LoadAvg1  float64 `json:"loadAvg1"`
	LoadAvg5  float64 `json:"loadAvg5"`
	LoadAvg15 float64 `json:"loadAvg15"`
	Uptime    string  `json:"uptime"`
}

type WALStats struct {
	CurrentLSN      string  `json:"currentLsn"`
	BytesPerSec     float64 `json:"bytesPerSec"`
	SegmentsPerHour float64 `json:"segmentsPerHour"`
}

type BGWriterStats struct {
	CheckpointsTimed     int64   `json:"checkpointsTimed"`
	CheckpointsRequested int64   `json:"checkpointsRequested"`
	CheckpointWriteMs    float64 `json:"checkpointWriteMs"`
	CheckpointSyncMs     float64 `json:"checkpointSyncMs"`
	BuffersCheckpoint    int64   `json:"buffersCheckpoint"`
	BuffersClean         int64   `json:"buffersClean"`
	MaxwrittenClean      int64   `json:"maxwrittenClean"`
	BuffersBackend       int64   `json:"buffersBackend"`
}

// WAL-G Backup

type Backup struct {
	Name             string    `json:"backup_name"`
	StartTime        time.Time `json:"start_time"`
	FinishTime       time.Time `json:"finish_time"`
	StartLSN         string    `json:"start_lsn"`
	FinishLSN        string    `json:"finish_lsn"`
	Hostname         string    `json:"hostname"`
	DataDir          string    `json:"data_dir"`
	CompressedSize   int64     `json:"compressed_size"`
	UncompressedSize int64     `json:"uncompressed_size"`
	IsPermanent      bool      `json:"is_permanent"`
	UserData         string    `json:"user_data"`
	WalFileName      string    `json:"wal_file_name"`
}

type BackupListResponse struct {
	Backups     []Backup `json:"backups"`
	LastBackup  string   `json:"lastBackup"`
	TotalSize   string   `json:"totalSize"`
	BackupCount int      `json:"backupCount"`
}

type RestoreRequest struct {
	BackupName string `json:"backupName"`
	TargetTime string `json:"targetTime,omitempty"` // PITR timestamp
}

type BackupTriggerResponse struct {
	Message string `json:"message"`
	Status  string `json:"status"`
	JobID   string `json:"jobId,omitempty"`
}

// S3

type S3Object struct {
	Key          string    `json:"key"`
	Size         int64     `json:"size"`
	LastModified time.Time `json:"lastModified"`
	IsDir        bool      `json:"isDir"`
	SizeHuman    string    `json:"sizeHuman"`
}

type S3ListResponse struct {
	Objects    []S3Object `json:"objects"`
	Prefix     string     `json:"prefix"`
	Bucket     string     `json:"bucket"`
	TotalSize  string     `json:"totalSize"`
	TotalCount int        `json:"totalCount"`
}

// PgBouncer

type PgBouncerStat struct {
	Database        string `json:"database"`
	TotalXactCount  int64  `json:"totalXactCount"`
	TotalQueryCount int64  `json:"totalQueryCount"`
	TotalReceived   int64  `json:"totalReceived"`
	TotalSent       int64  `json:"totalSent"`
	TotalXactTime   int64  `json:"totalXactTime"`
	TotalQueryTime  int64  `json:"totalQueryTime"`
	TotalWaitTime   int64  `json:"totalWaitTime"`
	AvgXactCount    int64  `json:"avgXactCount"`
	AvgQueryCount   int64  `json:"avgQueryCount"`
	AvgXactTime     int64  `json:"avgXactTime"`
	AvgQueryTime    int64  `json:"avgQueryTime"`
	AvgWaitTime     int64  `json:"avgWaitTime"`
}

type PgBouncerPool struct {
	Database  string `json:"database"`
	User      string `json:"user"`
	ClActive  int    `json:"clActive"`
	ClWaiting int    `json:"clWaiting"`
	SvActive  int    `json:"svActive"`
	SvIdle    int    `json:"svIdle"`
	SvUsed    int    `json:"svUsed"`
	SvTested  int    `json:"svTested"`
	SvLogin   int    `json:"svLogin"`
	MaxWait   int    `json:"maxWait"`
	PoolMode  string `json:"poolMode"`
}

type PgBouncerClient struct {
	Type        string `json:"type"`
	User        string `json:"user"`
	Database    string `json:"database"`
	State       string `json:"state"`
	Addr        string `json:"addr"`
	Port        int    `json:"port"`
	LocalAddr   string `json:"localAddr"`
	LocalPort   int    `json:"localPort"`
	ConnectTime string `json:"connectTime"`
	RequestTime string `json:"requestTime"`
}

type PgBouncerFullStats struct {
	Stats   []PgBouncerStat   `json:"stats"`
	Pools   []PgBouncerPool   `json:"pools"`
	Clients []PgBouncerClient `json:"clients"`
	Config  map[string]string `json:"config"`
}

// API Response wrapper

type APIResponse struct {
	Success bool   `json:"success"`
	Data    any    `json:"data,omitempty"`
	Error   string `json:"error,omitempty"`
}
