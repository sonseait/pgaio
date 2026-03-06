package model

import "time"

// PostgreSQL Stats

type PgStat struct {
	Timestamp       time.Time        `json:"timestamp"`
	Database        DatabaseStats    `json:"database"`
	Activity        ActivityStats    `json:"activity"`
	Connections     ConnectionStats  `json:"connections"`
	Replication     []ReplicationLag `json:"replication"`
	System          SystemStats      `json:"system"`
	PgBouncerStats  *PgBouncerStat   `json:"pgbouncer,omitempty"`
	PgBouncerPools  []PgBouncerPool  `json:"pgbouncerPools,omitempty"`
}

type DatabaseStats struct {
	Name          string  `json:"name"`
	Size          string  `json:"size"`
	TxCommit      int64   `json:"txCommit"`
	TxRollback    int64   `json:"txRollback"`
	BlksRead      int64   `json:"blksRead"`
	BlksHit       int64   `json:"blksHit"`
	CacheHitRatio float64 `json:"cacheHitRatio"`
	TempFiles     int64   `json:"tempFiles"`
	TempBytes     int64   `json:"tempBytes"`
	Deadlocks     int64   `json:"deadlocks"`
	Conflicts     int64   `json:"conflicts"`
	TupReturned   int64   `json:"tupReturned"`
	TupFetched    int64   `json:"tupFetched"`
	TupInserted   int64   `json:"tupInserted"`
	TupUpdated    int64   `json:"tupUpdated"`
	TupDeleted    int64   `json:"tupDeleted"`
}

type ActivityStats struct {
	TotalConnections int              `json:"totalConnections"`
	ActiveQueries    int              `json:"activeQueries"`
	IdleConnections  int              `json:"idleConnections"`
	WaitingQueries   int              `json:"waitingQueries"`
	Queries          []ActiveQuery    `json:"queries"`
}

type ActiveQuery struct {
	PID         int       `json:"pid"`
	User        string    `json:"user"`
	Database    string    `json:"database"`
	State       string    `json:"state"`
	Query       string    `json:"query"`
	Duration    string    `json:"duration"`
	WaitEvent   string    `json:"waitEvent"`
	BackendType string    `json:"backendType"`
	QueryStart  time.Time `json:"queryStart"`
}

type ConnectionStats struct {
	MaxConnections int `json:"maxConnections"`
	UsedConnections int `json:"usedConnections"`
	AvailableConnections int `json:"availableConnections"`
	ReservedConnections int `json:"reservedConnections"`
}

type ReplicationLag struct {
	ClientAddr string `json:"clientAddr"`
	State      string `json:"state"`
	SentLag    string `json:"sentLag"`
	WriteLag   string `json:"writeLag"`
	FlushLag   string `json:"flushLag"`
	ReplayLag  string `json:"replayLag"`
}

type SystemStats struct {
	CPUUsage    float64 `json:"cpuUsage"`
	MemTotal    uint64  `json:"memTotal"`
	MemUsed     uint64  `json:"memUsed"`
	MemFree     uint64  `json:"memFree"`
	MemUsage    float64 `json:"memUsage"`
	DiskTotal   uint64  `json:"diskTotal"`
	DiskUsed    uint64  `json:"diskUsed"`
	DiskFree    uint64  `json:"diskFree"`
	DiskUsage   float64 `json:"diskUsage"`
	LoadAvg1    float64 `json:"loadAvg1"`
	LoadAvg5    float64 `json:"loadAvg5"`
	LoadAvg15   float64 `json:"loadAvg15"`
	Uptime      string  `json:"uptime"`
}

// WAL-G Backup

type Backup struct {
	Name            string    `json:"backup_name"`
	StartTime       time.Time `json:"start_time"`
	FinishTime      time.Time `json:"finish_time"`
	StartLSN        string    `json:"start_lsn"`
	FinishLSN       string    `json:"finish_lsn"`
	Hostname        string    `json:"hostname"`
	DataDir         string    `json:"data_dir"`
	CompressedSize  int64     `json:"compressed_size"`
	UncompressedSize int64    `json:"uncompressed_size"`
	IsPermanent     bool      `json:"is_permanent"`
	UserData        string    `json:"user_data"`
	WalFileName     string    `json:"wal_file_name"`
}

type BackupListResponse struct {
	Backups       []Backup `json:"backups"`
	LastBackup    string   `json:"lastBackup"`
	TotalSize     string   `json:"totalSize"`
	BackupCount   int      `json:"backupCount"`
}

type RestoreRequest struct {
	BackupName string `json:"backupName"`
	TargetTime string `json:"targetTime,omitempty"` // PITR timestamp
}

type BackupTriggerResponse struct {
	Message string `json:"message"`
	Status  string `json:"status"`
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
	Database     string `json:"database"`
	TotalXactCount int64 `json:"totalXactCount"`
	TotalQueryCount int64 `json:"totalQueryCount"`
	TotalReceived  int64 `json:"totalReceived"`
	TotalSent      int64 `json:"totalSent"`
	TotalXactTime  int64 `json:"totalXactTime"`
	TotalQueryTime int64 `json:"totalQueryTime"`
	TotalWaitTime  int64 `json:"totalWaitTime"`
	AvgXactCount   int64 `json:"avgXactCount"`
	AvgQueryCount  int64 `json:"avgQueryCount"`
	AvgXactTime    int64 `json:"avgXactTime"`
	AvgQueryTime   int64 `json:"avgQueryTime"`
	AvgWaitTime    int64 `json:"avgWaitTime"`
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
	Type     string `json:"type"`
	User     string `json:"user"`
	Database string `json:"database"`
	State    string `json:"state"`
	Addr     string `json:"addr"`
	Port     int    `json:"port"`
	LocalAddr string `json:"localAddr"`
	LocalPort int    `json:"localPort"`
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
	Success bool        `json:"success"`
	Data    interface{} `json:"data,omitempty"`
	Error   string      `json:"error,omitempty"`
}
