package store

type LoadResult struct {
	ProfileDir           string
	ProfileName          string
	TrackedCount         int
	UnloadedProfileName  string
	UnloadedTrackedCount int
	RemovedBackupCount   int
	ChangedPaths         []string
}

type UnloadResult struct {
	ProfileName        string
	RemovedCount       int
	RemovedBackupCount int
	ChangedPaths       []string
}

type TidyResult struct {
	RemovedCount int
	ChangedPaths []string
}
