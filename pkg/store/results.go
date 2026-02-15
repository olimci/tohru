package store

type LoadResult struct {
	SourceDir            string
	SourceName           string
	TrackedCount         int
	UnloadedSourceName   string
	UnloadedTrackedCount int
	RemovedBackupCount   int
	ChangedPaths         []string
}

type UnloadResult struct {
	SourceName         string
	RemovedCount       int
	RemovedBackupCount int
	ChangedPaths       []string
}

type TidyResult struct {
	RemovedCount int
	ChangedPaths []string
}
