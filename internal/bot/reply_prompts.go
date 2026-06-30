package bot

import "sync"

type ReplyAction string

const (
	ReplyActionSummarize ReplyAction = "summarize"
	ReplyActionStatus    ReplyAction = "status"
)

type PendingReply struct {
	Action          ReplyAction
	ChatID          int64
	UserID          int64
	PromptMessageID int64
}

type ReplyPromptStore interface {
	Put(prompt PendingReply)
	Get(chatID, promptMessageID int64) (PendingReply, bool)
	Delete(chatID, promptMessageID int64)
}

type MemoryReplyPromptStore struct {
	mu       sync.RWMutex
	byPrompt map[replyPromptKey]PendingReply
	latest   map[replyPromptOwnerKey]replyPromptKey
}

type replyPromptKey struct {
	chatID          int64
	promptMessageID int64
}

type replyPromptOwnerKey struct {
	chatID int64
	userID int64
	action ReplyAction
}

func NewMemoryReplyPromptStore() *MemoryReplyPromptStore {
	return &MemoryReplyPromptStore{
		byPrompt: map[replyPromptKey]PendingReply{},
		latest:   map[replyPromptOwnerKey]replyPromptKey{},
	}
}

func (s *MemoryReplyPromptStore) Put(prompt PendingReply) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := replyPromptKey{chatID: prompt.ChatID, promptMessageID: prompt.PromptMessageID}
	owner := replyPromptOwnerKey{chatID: prompt.ChatID, userID: prompt.UserID, action: prompt.Action}
	if previous, ok := s.latest[owner]; ok {
		delete(s.byPrompt, previous)
	}
	s.byPrompt[key] = prompt
	s.latest[owner] = key
}

func (s *MemoryReplyPromptStore) Get(chatID, promptMessageID int64) (PendingReply, bool) {
	if s == nil {
		return PendingReply{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	prompt, ok := s.byPrompt[replyPromptKey{chatID: chatID, promptMessageID: promptMessageID}]
	return prompt, ok
}

func (s *MemoryReplyPromptStore) Delete(chatID, promptMessageID int64) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	key := replyPromptKey{chatID: chatID, promptMessageID: promptMessageID}
	prompt, ok := s.byPrompt[key]
	if !ok {
		return
	}
	delete(s.byPrompt, key)
	owner := replyPromptOwnerKey{chatID: prompt.ChatID, userID: prompt.UserID, action: prompt.Action}
	if s.latest[owner] == key {
		delete(s.latest, owner)
	}
}
