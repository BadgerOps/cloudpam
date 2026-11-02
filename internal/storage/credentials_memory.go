package storage

import (
	"context"
	"time"

	"cloudpam/internal/domain"
)

func (m *MemoryStore) GetAccountCredential(_ context.Context, accountID int64) (domain.AccountCredentialRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	rec, ok := m.credentials[accountID]
	if !ok {
		return domain.AccountCredentialRecord{}, ErrNotFound
	}
	return cloneAccountCredential(rec), nil
}

func (m *MemoryStore) PutAccountCredential(_ context.Context, in domain.SealedAccountCredential) (domain.AccountCredentialRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	a, ok := m.accounts[in.AccountID]
	if !ok || a.DeletedAt != nil {
		return domain.AccountCredentialRecord{}, ErrNotFound
	}
	now := time.Now().UTC()
	rec := domain.AccountCredentialRecord{
		AccountCredential: domain.AccountCredential{
			AccountID: in.AccountID,
			Type:      in.Type,
			Summary:   in.Summary,
			CreatedAt: now,
			UpdatedAt: now,
		},
		PayloadEncrypted: in.PayloadEncrypted,
	}
	if prev, exists := m.credentials[in.AccountID]; exists {
		rec.CreatedAt = prev.CreatedAt
	}
	m.credentials[in.AccountID] = rec
	return cloneAccountCredential(rec), nil
}

func (m *MemoryStore) DeleteAccountCredential(_ context.Context, accountID int64) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.credentials[accountID]
	delete(m.credentials, accountID)
	return ok, nil
}

func (m *MemoryStore) RecordAccountCredentialTest(_ context.Context, accountID int64, sealedPayload string, test domain.CredentialTest) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	rec, ok := m.credentials[accountID]
	if !ok || rec.PayloadEncrypted != sealedPayload {
		return ErrConflict
	}
	rec.LastTest = &test
	m.credentials[accountID] = rec
	return nil
}

func cloneAccountCredential(rec domain.AccountCredentialRecord) domain.AccountCredentialRecord {
	if rec.LastTest != nil {
		lt := *rec.LastTest
		rec.LastTest = &lt
	}
	return rec
}
