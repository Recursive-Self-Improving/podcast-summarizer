package auth

import (
	"context"
	"fmt"
)

type ChatType string

const (
	ChatTypePrivate    ChatType = "private"
	ChatTypeGroup      ChatType = "group"
	ChatTypeSupergroup ChatType = "supergroup"
)

type WhitelistStore interface {
	IsGroupWhitelisted(ctx context.Context, chatID int64) (bool, error)
	IsDMUserWhitelisted(ctx context.Context, userID int64) (bool, error)
}

type Service struct {
	ownerID int64
	store   WhitelistStore
}

func NewService(ownerID int64, store WhitelistStore) *Service {
	return &Service{ownerID: ownerID, store: store}
}

func (s *Service) IsOwner(userID int64) bool {
	return userID == s.ownerID
}

func (s *Service) CanUse(ctx context.Context, chatID, userID int64, chatType ChatType) (bool, error) {
	if s.IsOwner(userID) {
		return true, nil
	}

	switch chatType {
	case ChatTypePrivate:
		allowed, err := s.store.IsDMUserWhitelisted(ctx, userID)
		if err != nil {
			return false, fmt.Errorf("check DM whitelist: %w", err)
		}
		return allowed, nil
	case ChatTypeGroup, ChatTypeSupergroup:
		allowed, err := s.store.IsGroupWhitelisted(ctx, chatID)
		if err != nil {
			return false, fmt.Errorf("check group whitelist: %w", err)
		}
		return allowed, nil
	default:
		return false, nil
	}
}

func (s *Service) CanManageWhitelist(userID int64) bool {
	return s.IsOwner(userID)
}

func (s *Service) CanManageSubscriptions(userID int64) bool {
	return s.IsOwner(userID)
}
