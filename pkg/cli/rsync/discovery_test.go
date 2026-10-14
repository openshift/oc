package rsync

// mockFileDiscoverer implements the fileDiscoverer interface for testing.
type mockFileDiscoverer struct {
	files []string
	err   error
}

func (m *mockFileDiscoverer) DiscoverFiles(basePath string, lastN uint) ([]string, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.files, nil
}
