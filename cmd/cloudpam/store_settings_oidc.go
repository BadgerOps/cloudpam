package main

import (
	"cloudpam/internal/observability"
	"cloudpam/internal/storage"
)

func selectSettingsStore(logger observability.Logger, mainStore storage.Store) storage.SettingsStore {
	if ss, ok := mainStore.(storage.SettingsStore); ok {
		return ss
	}
	logger.Warn("main store does not implement SettingsStore; using in-memory fallback")
	return storage.NewMemorySettingsStore()
}

func selectOIDCProviderStore(logger observability.Logger, mainStore storage.Store) storage.OIDCProviderStore {
	if os, ok := mainStore.(storage.OIDCProviderStore); ok {
		return os
	}
	logger.Warn("main store does not implement OIDCProviderStore; using in-memory fallback")
	return storage.NewMemoryOIDCProviderStore()
}
