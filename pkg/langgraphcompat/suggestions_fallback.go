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
	intent := detectSuggestionIntent(lastUser, language)

	var candidates []string
	switch language {
	case "zh":
		candidates = zhFallbackCandidates(subject, intent)
	case "ja":
		candidates = jaFallbackCandidates(subject, intent)
	case "ko":
		candidates = koFallbackCandidates(subject, intent)
	default:
		candidates = enFallbackCandidates(subject, intent)
	}

	if n < len(candidates) {
		return candidates[:n]
	}
	return candidates
}

func zhFallbackCandidates(subject, intent string) []string {
	switch intent {
	case "summarize":
		if subject != "" {
			return []string{
				"把“" + subject + "”整理成一份精炼摘要，并标注关键结论。",
				"继续围绕“" + subject + "”：请补充风险、假设和不确定性。",
				"基于“" + subject + "”，给我一个适合继续追问的问题清单。",
			}
		}
	case "compare":
		if subject != "" {
			return []string{
				"把“" + subject + "”涉及的选项做成对比表，并说明取舍。",
				"继续围绕“" + subject + "”：请给出推荐方案和理由。",
				"如果要落地“" + subject + "”，最需要先验证什么？",
			}
		}
	case "write":
		if subject != "" {
			return []string{
				"基于“" + subject + "”产出一个可直接使用的初稿。",
				"继续围绕“" + subject + "”：请改成更专业但更简洁的版本。",
				"如果面向不同受众，“" + subject + "”应该如何分别表达？",
			}
		}
	case "analyze":
		if subject != "" {
			return []string{
				"继续深入“" + subject + "”：请拆解关键因素和因果关系。",
				"围绕“" + subject + "”列出最重要的数据点或证据。",
				"如果继续分析“" + subject + "”，下一步最值得验证什么？",
			}
		}
	}
	if subject != "" {
		return []string{
			"围绕“" + subject + "”给出一个可执行的分步计划。",
			"继续深入“" + subject + "”：请总结关键结论并标注不确定性。",
			"请围绕“" + subject + "”给出 3 个下一步可选方案并比较利弊。",
		}
	}
	return []string{
		"请基于以上内容给出一个可执行的分步计划。",
		"请总结关键结论，并标注不确定性。",
		"请给出 3 个下一步可选方案并比较利弊。",
	}
}

func jaFallbackCandidates(subject, intent string) []string {
	switch intent {
	case "summarize":
		if subject != "" {
			return []string{
				"「" + subject + "」の要点を短く整理し、重要な結論をまとめてください。",
				"「" + subject + "」について、不確実な点や前提も補足してください。",
				"「" + subject + "」をさらに深掘りするための質問案を挙げてください。",
			}
		}
	case "compare":
		if subject != "" {
			return []string{
				"「" + subject + "」の選択肢を比較表にして、違いを整理してください。",
				"「" + subject + "」について、おすすめ案とその理由を教えてください。",
				"「" + subject + "」を進める前に確認すべき点は何ですか。",
			}
		}
	case "write":
		if subject != "" {
			return []string{
				"「" + subject + "」をもとに、そのまま使えるドラフトを作ってください。",
				"「" + subject + "」をより簡潔で伝わりやすい表現に直してください。",
				"「" + subject + "」を相手別にどう書き分けるべきか提案してください。",
			}
		}
	case "analyze":
		if subject != "" {
			return []string{
				"「" + subject + "」をさらに分析して、重要な要因を分解してください。",
				"「" + subject + "」に関する根拠やデータの観点を整理してください。",
				"「" + subject + "」を次に検証するなら、何から着手すべきですか。",
			}
		}
	}
	if subject != "" {
		return []string{
			"「" + subject + "」について、実行可能なステップに分けてください。",
			"「" + subject + "」をさらに深掘りして、要点と不確実な点を整理してください。",
			"「" + subject + "」の次の選択肢を 3 つ挙げて、利点と注意点を比べてください。",
		}
	}
	return []string{
		"上の内容を実行可能なステップに分けてください。",
		"要点を整理して、不確実な点も示してください。",
		"次の選択肢を 3 つ挙げて、利点と注意点を比べてください。",
	}
}

func koFallbackCandidates(subject, intent string) []string {
	switch intent {
	case "summarize":
		if subject != "" {
			return []string{
				"“" + subject + "”의 핵심 내용을 짧게 요약해 주세요.",
				"“" + subject + "”와 관련된 불확실한 점과 전제를 함께 정리해 주세요.",
				"“" + subject + "”를 더 깊게 보기 위한 다음 질문을 제안해 주세요.",
			}
		}
	case "compare":
		if subject != "" {
			return []string{
				"“" + subject + "”의 선택지를 비교표로 정리해 주세요.",
				"“" + subject + "”에 대해 추천안을 고르고 이유를 설명해 주세요.",
				"“" + subject + "”를 진행하기 전에 먼저 확인할 점은 무엇인가요?",
			}
		}
	case "write":
		if subject != "" {
			return []string{
				"“" + subject + "”를 바탕으로 바로 사용할 수 있는 초안을 작성해 주세요.",
				"“" + subject + "”를 더 간결하고 자연스럽게 다듬어 주세요.",
				"“" + subject + "”를 대상별로 어떻게 표현하면 좋을지 제안해 주세요.",
			}
		}
	case "analyze":
		if subject != "" {
			return []string{
				"“" + subject + "”를 더 분석해 핵심 요인을 분해해 주세요.",
				"“" + subject + "”에 필요한 근거와 데이터를 정리해 주세요.",
				"“" + subject + "”를 다음 단계로 검증하려면 무엇부터 봐야 하나요?",
			}
		}
	}
	if subject != "" {
		return []string{
			"“" + subject + "”에 대해 실행 가능한 단계별 계획을 정리해 주세요.",
			"“" + subject + "”를 더 깊게 분석해 핵심 결론과 불확실한 점을 정리해 주세요.",
			"“" + subject + "”의 다음 선택지 3가지를 제시하고 장단점을 비교해 주세요.",
		}
	}
	return []string{
		"위 내용을 실행 가능한 단계별 계획으로 정리해 주세요.",
		"핵심 결론을 요약하고 불확실한 점도 표시해 주세요.",
		"다음 선택지 3가지를 제시하고 장단점을 비교해 주세요.",
	}
}

func enFallbackCandidates(subject, intent string) []string {
	switch intent {
	case "summarize":
		if subject != "" {
			return []string{
				"Summarize \"" + subject + "\" into a concise takeaway list.",
				"Go deeper on \"" + subject + "\" and call out assumptions or uncertainties.",
				"What are the best follow-up questions to keep exploring \"" + subject + "\"?",
			}
		}
	case "compare":
		if subject != "" {
			return []string{
				"Compare the main options in \"" + subject + "\" in a simple table.",
				"Based on \"" + subject + "\", which option do you recommend and why?",
				"What should I validate first before acting on \"" + subject + "\"?",
			}
		}
	case "write":
		if subject != "" {
			return []string{
				"Turn \"" + subject + "\" into a polished draft I can use directly.",
				"Rewrite \"" + subject + "\" to be clearer and more concise.",
				"How should \"" + subject + "\" change for different audiences?",
			}
		}
	case "analyze":
		if subject != "" {
			return []string{
				"Analyze \"" + subject + "\" more deeply and break down the main drivers.",
				"What evidence or data matters most for \"" + subject + "\"?",
				"If I keep exploring \"" + subject + "\", what should I verify next?",
			}
		}
	}
	if subject != "" {
		return []string{
			"Turn \"" + subject + "\" into a concrete step-by-step plan.",
			"Go deeper on \"" + subject + "\" and summarize the key conclusions and uncertainties.",
			"Give me 3 possible next steps for \"" + subject + "\" and compare the tradeoffs.",
		}
	}
	return []string{
		"Turn this into a concrete step-by-step plan.",
		"Summarize the key conclusions and call out uncertainties.",
		"Give me 3 possible next steps and compare the tradeoffs.",
	}
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

func detectSuggestionIntent(text, language string) string {
	normalized := strings.ToLower(strings.TrimSpace(text))
	if normalized == "" {
		return "general"
	}

	switch language {
	case "zh":
		if containsAny(normalized, "总结", "概括", "摘要", "梳理", "提炼") {
			return "summarize"
		}
		if containsAny(normalized, "对比", "比较", "区别", "优缺点", "利弊") {
			return "compare"
		}
		if containsAny(normalized, "写", "撰写", "起草", "文案", "邮件", "回复") {
			return "write"
		}
		if containsAny(normalized, "分析", "拆解", "研究", "判断", "评估") {
			return "analyze"
		}
	case "ja":
		if containsAny(normalized, "要約", "まとめ", "整理", "要点") {
			return "summarize"
		}
		if containsAny(normalized, "比較", "違い", "利点", "欠点") {
			return "compare"
		}
		if containsAny(normalized, "書いて", "作成", "下書き", "文面", "メール") {
			return "write"
		}
		if containsAny(normalized, "分析", "検討", "評価", "分解") {
			return "analyze"
		}
	case "ko":
		if containsAny(normalized, "요약", "정리", "핵심", "개요") {
			return "summarize"
		}
		if containsAny(normalized, "비교", "차이", "장단점", "옵션") {
			return "compare"
		}
		if containsAny(normalized, "작성", "초안", "문안", "메일", "답장") {
			return "write"
		}
		if containsAny(normalized, "분석", "검토", "평가", "해석") {
			return "analyze"
		}
	default:
		if containsAny(normalized, "summarize", "summary", "recap", "outline") {
			return "summarize"
		}
		if containsAny(normalized, "compare", "comparison", "tradeoff", "pros and cons") {
			return "compare"
		}
		if containsAny(normalized, "write", "draft", "rewrite", "email", "reply") {
			return "write"
		}
		if containsAny(normalized, "analyze", "analysis", "evaluate", "assess", "break down") {
			return "analyze"
		}
	}
	return "general"
}

func containsAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if needle != "" && strings.Contains(text, needle) {
			return true
		}
	}
	return false
}
