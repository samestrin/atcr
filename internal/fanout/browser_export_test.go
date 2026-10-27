package fanout

import "maps"

// unregisterBrowserBackend removes name from the registry so a test leaves it
// as it found it.
func unregisterBrowserBackend(name string) {
	browserBackendsMu.Lock()
	defer browserBackendsMu.Unlock()
	delete(browserBackends, name)
}

// snapshotBrowserBackends copies the registry for restoreBrowserBackends.
func snapshotBrowserBackends() map[string]BrowserBackend {
	browserBackendsMu.RLock()
	defer browserBackendsMu.RUnlock()
	return maps.Clone(browserBackends)
}

// restoreBrowserBackends replaces the registry with saved (nil empties it).
func restoreBrowserBackends(saved map[string]BrowserBackend) {
	browserBackendsMu.Lock()
	defer browserBackendsMu.Unlock()
	browserBackends = map[string]BrowserBackend{}
	maps.Copy(browserBackends, saved)
}
