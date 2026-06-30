package bot

import "testing"

func TestMemoryReplyPromptStoreStoresAndDeletesPrompt(t *testing.T) {
	store := NewMemoryReplyPromptStore()
	prompt := PendingReply{Action: ReplyActionSummarize, ChatID: 10, UserID: 20, PromptMessageID: 30}

	store.Put(prompt)
	got, ok := store.Get(10, 30)
	if !ok || got != prompt {
		t.Fatalf("prompt = %#v ok=%v", got, ok)
	}
	store.Delete(10, 30)
	if _, ok := store.Get(10, 30); ok {
		t.Fatal("expected prompt to be deleted")
	}
}

func TestMemoryReplyPromptStoreReplacesLatestPromptForSameOwnerAndAction(t *testing.T) {
	store := NewMemoryReplyPromptStore()
	store.Put(PendingReply{Action: ReplyActionSummarize, ChatID: 10, UserID: 20, PromptMessageID: 30})
	latest := PendingReply{Action: ReplyActionSummarize, ChatID: 10, UserID: 20, PromptMessageID: 31}
	store.Put(latest)

	if _, ok := store.Get(10, 30); ok {
		t.Fatal("expected older prompt to be replaced")
	}
	got, ok := store.Get(10, 31)
	if !ok || got != latest {
		t.Fatalf("prompt = %#v ok=%v", got, ok)
	}
}

func TestMemoryReplyPromptStoreKeepsDifferentActionsSeparate(t *testing.T) {
	store := NewMemoryReplyPromptStore()
	summarizePrompt := PendingReply{Action: ReplyActionSummarize, ChatID: 10, UserID: 20, PromptMessageID: 30}
	statusPrompt := PendingReply{Action: ReplyActionStatus, ChatID: 10, UserID: 20, PromptMessageID: 31}

	store.Put(summarizePrompt)
	store.Put(statusPrompt)
	if got, ok := store.Get(10, 30); !ok || got != summarizePrompt {
		t.Fatalf("summarize prompt = %#v ok=%v", got, ok)
	}
	if got, ok := store.Get(10, 31); !ok || got != statusPrompt {
		t.Fatalf("status prompt = %#v ok=%v", got, ok)
	}
}
