package bot

import "testing"

func TestSummaryVariantKeyboardUsesOneRowLanguageButtons(t *testing.T) {
	keyboard := summaryVariantKeyboard(42)
	if len(keyboard.InlineKeyboard) != 1 {
		t.Fatalf("rows = %#v", keyboard.InlineKeyboard)
	}
	row := keyboard.InlineKeyboard[0]
	if len(row) != 2 {
		t.Fatalf("row = %#v", row)
	}
	if row[0].Text != "简中" || row[0].CallbackData != "summary_variant:42:zh-hans" {
		t.Fatalf("first button = %#v", row[0])
	}
	if row[1].Text != "繁中" || row[1].CallbackData != "summary_variant:42:zh-hant" {
		t.Fatalf("second button = %#v", row[1])
	}
}
