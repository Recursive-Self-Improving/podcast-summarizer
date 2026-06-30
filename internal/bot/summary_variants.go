package bot

import (
	"fmt"

	"github.com/go-telegram/bot/models"

	"github.com/Recursive-Self-Improving/podcast-summarizer/internal/summarize"
)

const summaryVariantCallbackPrefix = "summary_variant:"

func summaryVariantCallbackData(requestID int64, variant summarize.SummaryVariant) string {
	return fmt.Sprintf("%s%d:%s", summaryVariantCallbackPrefix, requestID, variant.Code)
}

func summaryVariantKeyboard(requestID int64) models.InlineKeyboardMarkup {
	variants := summarize.SummaryVariants()
	row := make([]models.InlineKeyboardButton, 0, len(variants))
	for _, variant := range variants {
		row = append(row, models.InlineKeyboardButton{Text: variant.Label, CallbackData: summaryVariantCallbackData(requestID, variant)})
	}
	return models.InlineKeyboardMarkup{InlineKeyboard: [][]models.InlineKeyboardButton{row}}
}
