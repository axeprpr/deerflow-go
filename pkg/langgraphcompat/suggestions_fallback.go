package langgraphcompat

import (
	"strings"
	"unicode"
)

func localizedFallbackSuggestions(lastUser string, n int) []string {
	if n <= 0 {
		return []string{}
	}

	subject := compactSubject(lastUser)
	language := detectSuggestionLanguage(lastUser)

	var candidates []string
	switch language {
	case "zh":
		candidates = []string{
			"请基于以上内容给出一个可执行的分步计划。",
			"请总结关键结论，并标注不确定性。",
			"请给出 3 个下一步可选方案并比较利弊。",
		}
		if subject != "" {
			candidates[0] = "围绕“" + subject + "”给出一个可执行的分步计划。"
			candidates[1] = "继续深入“" + subject + "”：请总结关键结论并标注不确定性。"
		}
	case "ja":
		candidates = []string{
			"上の内容を実行可能なステップに分けてください。",
			"要点を整理して、不確実な点も示してください。",
			"次の選択肢を 3 つ挙げて、利点と注意点を比べてください。",
		}
		if subject != "" {
			candidates[0] = "「" + subject + "」について、実行可能なステップに分けてください。"
			candidates[1] = "「" + subject + "」をさらに深掘りして、要点と不確実な点を整理してください。"
		}
	case "ko":
		candidates = []string{
			"위 내용을 실행 가능한 단계별 계획으로 정리해 주세요.",
			"핵심 결론을 요약하고 불확실한 점도 표시해 주세요.",
			"다음 선택지 3가지를 제시하고 장단점을 비교해 주세요.",
		}
		if subject != "" {
			candidates[0] = "“" + subject + "”에 대해 실행 가능한 단계별 계획을 정리해 주세요."
			candidates[1] = "“" + subject + "”를 더 깊게 분석해 핵심 결론과 불확실한 점을 정리해 주세요."
		}
	default:
		candidates = []string{
			"Turn this into a concrete step-by-step plan.",
			"Summarize the key conclusions and call out uncertainties.",
			"Give me 3 possible next steps and compare the tradeoffs.",
		}
		if subject != "" {
			candidates[0] = "Turn \"" + subject + "\" into a concrete step-by-step plan."
			candidates[1] = "Go deeper on \"" + subject + "\" and summarize the key conclusions and uncertainties."
		}
	}

	if n < len(candidates) {
		return candidates[:n]
	}
	return candidates
}

func detectSuggestionLanguage(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return "en"
	}

	hasHan := false
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Hiragana, unicode.Katakana):
			return "ja"
		case unicode.In(r, unicode.Hangul):
			return "ko"
		case unicode.In(r, unicode.Han):
			hasHan = true
		}
	}
	if hasHan {
		return "zh"
	}
	return "en"
}
