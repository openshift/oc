package rsync

// fileDiscoverer discovers files at the given path,
// limiting the list to lastN most recently modified files.
type fileDiscoverer interface {
	DiscoverFiles(basePath string, lastN uint) ([]string, error)
}
