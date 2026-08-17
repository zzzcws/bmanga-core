package prototype

// newServerWithoutCatalogForTest preserves the focused, hand-built schemas used
// by older unit tests. Production construction always uses NewServer and
// initializes the complete catalog.
func newServerWithoutCatalogForTest(dbPath string) (*Server, error) {
	return newServer(dbPath, false)
}
