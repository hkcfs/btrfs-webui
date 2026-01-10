package config

// Log Levels
const (
	LogLevelNone    = "NONE"
	LogLevelDefault = "DEFAULT"
	LogLevelVerbose = "VERBOSE"
)

type ScheduleConfig struct {
	Enabled bool   `json:"enabled"`
	Type    string `json:"type"`  // every_x, cron
	Value   string `json:"value"` // 15, */15...
	Unit    string `json:"unit"`  // minutes, hours, days
}

type RetentionConfig struct {
	Enabled bool   `json:"enabled"`
	Mode    string `json:"mode"` // count, time
	Value   int    `json:"value"`
	Unit    string `json:"unit"` // days, weeks
}

type ReplicationConfig struct {
	Enabled    bool   `json:"enabled"`
	TargetDest string `json:"target_dest"` // Path to mount point of external drive
}

type BackupJob struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Source      string            `json:"source"`
	Dest        string            `json:"dest"`
	PreCommand  string            `json:"pre_command"`  // Hook
	PostCommand string            `json:"post_command"` // Hook
	Schedule    ScheduleConfig    `json:"schedule"`
	Retention   RetentionConfig   `json:"retention"`
	Replication ReplicationConfig `json:"replication"`
}

type GlobalConfig struct {
	TargetDrive  string         `json:"target_drive"`
	LogLevel     string         `json:"log_level"` // NONE, DEFAULT, VERBOSE
	Jobs         []BackupJob    `json:"jobs"`
	ScrubSched   ScheduleConfig `json:"scrub_sched"`
	BalanceSched ScheduleConfig `json:"balance_sched"`

	// Legacy fields for migration (Must match old config.json structure)
	SnapshotSource string           `json:"snapshot_source,omitempty"`
	SnapshotDest   string           `json:"snapshot_dest,omitempty"`
	SnapshotSched  *ScheduleConfig  `json:"snapshot_sched,omitempty"`
	Retention      *RetentionConfig `json:"retention,omitempty"`
}
