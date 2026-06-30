package auth

import (
	"context"
	"errors"
	"testing"
)

func TestServiceAllowsOwnerEverywhere(t *testing.T) {
	service := NewService(1, fakeStore{})

	for _, chatType := range []ChatType{ChatTypePrivate, ChatTypeGroup, ChatTypeSupergroup, "channel"} {
		allowed, err := service.CanUse(context.Background(), -100, 1, chatType)
		if err != nil {
			t.Fatalf("CanUse returned error: %v", err)
		}
		if !allowed {
			t.Fatalf("owner not allowed in %s", chatType)
		}
	}
	if !service.CanManageWhitelist(1) {
		t.Fatal("owner should manage whitelist")
	}
	if !service.CanManageSubscriptions(1) {
		t.Fatal("owner should manage subscriptions")
	}
}

func TestServiceRejectsUnauthorizedDM(t *testing.T) {
	service := NewService(1, fakeStore{})

	allowed, err := service.CanUse(context.Background(), 2, 2, ChatTypePrivate)
	if err != nil {
		t.Fatalf("CanUse returned error: %v", err)
	}
	if allowed {
		t.Fatal("unauthorized DM user allowed")
	}
}

func TestServiceAllowsWhitelistedDM(t *testing.T) {
	service := NewService(1, fakeStore{dmUsers: map[int64]bool{2: true}})

	allowed, err := service.CanUse(context.Background(), 2, 2, ChatTypePrivate)
	if err != nil {
		t.Fatalf("CanUse returned error: %v", err)
	}
	if !allowed {
		t.Fatal("whitelisted DM user rejected")
	}
}

func TestServiceRejectsUnauthorizedGroup(t *testing.T) {
	service := NewService(1, fakeStore{})

	allowed, err := service.CanUse(context.Background(), -100, 2, ChatTypeGroup)
	if err != nil {
		t.Fatalf("CanUse returned error: %v", err)
	}
	if allowed {
		t.Fatal("unauthorized group member allowed")
	}
}

func TestServiceAllowsWhitelistedGroup(t *testing.T) {
	service := NewService(1, fakeStore{groups: map[int64]bool{-100: true}})

	allowed, err := service.CanUse(context.Background(), -100, 2, ChatTypeSupergroup)
	if err != nil {
		t.Fatalf("CanUse returned error: %v", err)
	}
	if !allowed {
		t.Fatal("whitelisted group member rejected")
	}
}

func TestServiceRestrictsManagementToOwner(t *testing.T) {
	service := NewService(1, fakeStore{groups: map[int64]bool{-100: true}, dmUsers: map[int64]bool{2: true}})

	if service.CanManageWhitelist(2) {
		t.Fatal("non-owner should not manage whitelist")
	}
	if service.CanManageSubscriptions(2) {
		t.Fatal("non-owner should not manage subscriptions")
	}
}

func TestServicePropagatesStoreErrors(t *testing.T) {
	boom := errors.New("boom")
	service := NewService(1, fakeStore{err: boom})

	if _, err := service.CanUse(context.Background(), -100, 2, ChatTypeGroup); !errors.Is(err, boom) {
		t.Fatalf("expected group error, got %v", err)
	}
	if _, err := service.CanUse(context.Background(), 2, 2, ChatTypePrivate); !errors.Is(err, boom) {
		t.Fatalf("expected DM error, got %v", err)
	}
}

type fakeStore struct {
	groups  map[int64]bool
	dmUsers map[int64]bool
	err     error
}

func (f fakeStore) IsGroupWhitelisted(_ context.Context, chatID int64) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.groups[chatID], nil
}

func (f fakeStore) IsDMUserWhitelisted(_ context.Context, userID int64) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.dmUsers[userID], nil
}
