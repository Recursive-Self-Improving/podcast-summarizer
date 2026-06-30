package summarize

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

type SummaryLanguage string

const (
	SummaryLanguageSimplified  SummaryLanguage = "simplified"
	SummaryLanguageTraditional SummaryLanguage = "traditional"
)

type SummaryVariant struct {
	Code     string
	Label    string
	Language SummaryLanguage
}

var (
	VariantSimplified  = SummaryVariant{Code: "zh-hans", Label: "简中", Language: SummaryLanguageSimplified}
	VariantTraditional = SummaryVariant{Code: "zh-hant", Label: "繁中", Language: SummaryLanguageTraditional}
)

var summaryVariants = []SummaryVariant{
	VariantSimplified,
	VariantTraditional,
}

const DefaultPrompt = `请用以下固定 Markdown 二级标题输出摘要，标题文字必须保持完全一致，并以简体中文输出：

## 核心摘要

## 容易被忽略但有价值的信息

## 直观地可以 bullish / bearish on 什么

## 隐含地可以 bullish / bearish on 什么

## 可能利好/利空的股票

每个标题下填写对应内容：
1. 核心摘要：写 5-20 个关键要点，但不必死板地平铺列出。根据具体内容灵活选择结构：可以是时间线、主题分类、问答提炼、观点交锋对比、或者按嘉宾/话题分块。要求覆盖关键人物、核心话题、主要结论。务必在摘要开头点明本期嘉宾是谁。
2. 容易被忽略但有价值的信息：提炼主持人或嘉宾可能只是带过一句、但投资人应该重视的细节、数据、趋势。
3. 直观地可以 bullish / bearish on 什么：列出嘉宾明确表达了看好/看空态度的方向，或者数据/事实直接支持的产业、公司、赛道。
4. 隐含地可以 bullish / bearish on 什么：列出没有被明确说出但可以推导出的投资启示，或者主持人的追问与嘉宾的回答之间暗含的逻辑。
5. 可能利好/利空的股票：基于本期讨论的产业趋势、技术方向、政策变化或嘉宾观点，列出可能受益或受损的具体股票/公司（美股、A股、港股均可，不限市场）。每个标的后附 1-2 句话说明逻辑。如果本期内容未涉及可直接关联的投资标的，注明“本期未提及具体标的”即可。`

const traditionalPrompt = `請用以下固定 Markdown 二級標題輸出摘要，標題文字必須保持完全一致，並以繁體中文輸出：

## 核心摘要

## 容易被忽略但有價值的資訊

## 直觀地可以 bullish / bearish on 什麼

## 隱含地可以 bullish / bearish on 什麼

## 可能利好/利空的股票

每個標題下填寫對應內容：
1. 核心摘要：寫 5-20 個關鍵要點，但不必死板地平鋪列出。根據具體內容靈活選擇結構：可以是時間線、主題分類、問答提煉、觀點交鋒對比、或者按嘉賓/話題分塊。要求覆蓋關鍵人物、核心話題、主要結論。務必在摘要開頭點明本期嘉賓是誰。
2. 容易被忽略但有價值的資訊：提煉主持人或嘉賓可能只是帶過一句、但投資人應該重視的細節、數據、趨勢。
3. 直觀地可以 bullish / bearish on 什麼：列出嘉賓明確表達了看好/看空態度的方向，或者數據/事實直接支持的產業、公司、賽道。
4. 隱含地可以 bullish / bearish on 什麼：列出沒有被明確說出但可以推導出的投資啟示，或者主持人的追問與嘉賓的回答之間暗含的邏輯。
5. 可能利好/利空的股票：基於本期討論的產業趨勢、技術方向、政策變化或嘉賓觀點，列出可能受益或受損的具體股票/公司（美股、A股、港股均可，不限市場）。每個標的後附 1-2 句話說明邏輯。如果本期內容未涉及可直接關聯的投資標的，註明「本期未提及具體標的」即可。`

func DefaultSummaryVariant() SummaryVariant {
	return VariantSimplified
}

func SummaryVariants() []SummaryVariant {
	return append([]SummaryVariant(nil), summaryVariants...)
}

func SummaryVariantByCode(code string) (SummaryVariant, bool) {
	switch code {
	case "js":
		code = VariantSimplified.Code
	case "fs":
		code = VariantTraditional.Code
	}
	for _, variant := range summaryVariants {
		if variant.Code == code {
			return variant, true
		}
	}
	return SummaryVariant{}, false
}

func SummaryVariantByPromptHash(promptHash string) (SummaryVariant, bool) {
	for _, variant := range summaryVariants {
		if PromptHash(variant.Prompt()) == promptHash {
			return variant, true
		}
	}
	return SummaryVariant{}, false
}

func (v SummaryVariant) Prompt() string {
	switch v.Code {
	case VariantTraditional.Code:
		return traditionalPrompt
	default:
		return DefaultPrompt
	}
}

func ResolvePrompt(customPrompt string) string {
	if strings.TrimSpace(customPrompt) == "" {
		return DefaultPrompt
	}
	return customPrompt
}

func PromptHash(prompt string) string {
	sum := sha256.Sum256([]byte(prompt))
	return hex.EncodeToString(sum[:])
}
