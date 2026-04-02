package langgraphcompat

import (
	"strings"
	"unicode"
)

func localizedContextFallbackSuggestions(hints suggestionContext, languageHint string, n int) []string {
	if n <= 0 {
		return []string{}
	}

	language := detectContextSuggestionLanguage(hints, languageHint)
	var candidates []string
	switch language {
	case "zh":
		candidates = zhContextFallbackCandidates(hints)
	case "ja":
		candidates = jaContextFallbackCandidates(hints)
	case "ko":
		candidates = koContextFallbackCandidates(hints)
	default:
		candidates = enContextFallbackCandidates(hints)
	}
	if len(candidates) >= n {
		return candidates[:n]
	}
	return completeSuggestionCandidates(candidates, genericContextSuggestions(language, n), n)
}

func genericContextSuggestions(language string, n int) []string {
	var candidates []string
	switch language {
	case "zh":
		candidates = []string{
			"请基于当前线程上下文给我一个清晰的下一步计划。",
			"请总结当前线程的关键结论和待确认事项。",
			"请给我 3 个最值得继续追问的问题。",
		}
	case "ja":
		candidates = []string{
			"現在のスレッド文脈をもとに、次のステップを整理してください。",
			"現時点の重要な結論と未確定事項をまとめてください。",
			"次に深掘りすべき質問を 3 つ挙げてください。",
		}
	case "ko":
		candidates = []string{
			"현재 스레드 맥락을 바탕으로 다음 단계를 정리해 주세요.",
			"지금까지의 핵심 결론과 확인이 필요한 사항을 요약해 주세요.",
			"다음에 더 물어볼 만한 질문 3가지를 제안해 주세요.",
		}
	default:
		candidates = []string{
			"Based on the current thread context, what should I do next?",
			"Summarize the key conclusions and open questions in this thread.",
			"Give me 3 good follow-up questions to keep this moving.",
		}
	}
	if n < len(candidates) {
		return candidates[:n]
	}
	return candidates
}

func completeSuggestionCandidates(primary, fallback []string, n int) []string {
	if n <= 0 {
		return []string{}
	}

	out := make([]string, 0, n)
	seen := make(map[string]struct{}, n)
	appendUnique := func(items []string) {
		for _, item := range items {
			item = normalizeSuggestionText(item)
			if item == "" {
				continue
			}
			key := strings.ToLower(item)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, item)
			if len(out) == n {
				return
			}
		}
	}

	appendUnique(primary)
	if len(out) < n {
		appendUnique(fallback)
	}
	return out
}

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

func detectContextSuggestionLanguage(hints suggestionContext, languageHint string) string {
	sampleParts := make([]string, 0, 6)
	languageHint = strings.TrimSpace(languageHint)
	if languageHint != "" {
		sampleParts = append(sampleParts, languageHint)
	}
	for _, part := range []string{hints.Title, hints.AgentName, hints.AgentHint} {
		part = strings.TrimSpace(part)
		if part != "" {
			sampleParts = append(sampleParts, part)
		}
	}
	for _, items := range [][]string{hints.Uploads, hints.Artifacts} {
		for _, item := range items {
			item = strings.TrimSpace(item)
			if item == "" {
				continue
			}
			sampleParts = append(sampleParts, item)
			if len(sampleParts) >= 6 {
				break
			}
		}
		if len(sampleParts) >= 6 {
			break
		}
	}
	return detectSuggestionLanguage(strings.Join(sampleParts, " "))
}

func zhContextFallbackCandidates(hints suggestionContext) []string {
	candidates := make([]string, 0, 5)
	if len(hints.Uploads) > 0 {
		candidates = append(candidates,
			"先概括这些上传文件的关键内容",
			"这些文件里最值得我优先关注什么",
		)
	}
	if len(hints.Artifacts) > 0 {
		candidates = append(candidates,
			"基于当前产物，下一步建议我做什么",
			"帮我检查这些产物还有哪些需要完善",
		)
	}
	if strings.TrimSpace(hints.AgentName) != "" {
		candidates = append(candidates, "继续用这个 agent 帮我细化下一步方案")
	}
	if len(candidates) == 0 {
		return nil
	}
	return candidates
}

func jaContextFallbackCandidates(hints suggestionContext) []string {
	candidates := make([]string, 0, 5)
	if len(hints.Uploads) > 0 {
		candidates = append(candidates,
			"アップロードしたファイルの要点を先に整理してください。",
			"これらのファイルで優先して確認すべき点は何ですか。",
		)
	}
	if len(hints.Artifacts) > 0 {
		candidates = append(candidates,
			"この成果物を踏まえて、次に何を進めるべきですか。",
			"この成果物でまだ改善すべき点を確認してください。",
		)
	}
	if strings.TrimSpace(hints.AgentName) != "" {
		candidates = append(candidates, "この agent を使って次のステップをさらに具体化してください。")
	}
	if len(candidates) == 0 {
		return nil
	}
	return candidates
}

func koContextFallbackCandidates(hints suggestionContext) []string {
	candidates := make([]string, 0, 5)
	if len(hints.Uploads) > 0 {
		candidates = append(candidates,
			"업로드한 파일들의 핵심 내용을 먼저 정리해 주세요.",
			"이 파일들에서 무엇을 가장 먼저 봐야 하나요?",
		)
	}
	if len(hints.Artifacts) > 0 {
		candidates = append(candidates,
			"현재 산출물을 기준으로 다음 단계는 무엇이 좋을까요?",
			"이 산출물에서 더 보완할 점이 있는지 점검해 주세요.",
		)
	}
	if strings.TrimSpace(hints.AgentName) != "" {
		candidates = append(candidates, "이 agent로 다음 단계를 더 구체화해 주세요.")
	}
	if len(candidates) == 0 {
		return nil
	}
	return candidates
}

func enContextFallbackCandidates(hints suggestionContext) []string {
	candidates := make([]string, 0, 5)
	if len(hints.Uploads) > 0 {
		candidates = append(candidates,
			"Summarize the key points from these uploaded files first.",
			"What should I pay attention to first in these files?",
		)
	}
	if len(hints.Artifacts) > 0 {
		candidates = append(candidates,
			"Based on the current artifacts, what should I do next?",
			"Review these artifacts and point out what still needs improvement.",
		)
	}
	if strings.TrimSpace(hints.AgentName) != "" {
		candidates = append(candidates, "Use this agent to refine the next steps for me.")
	}
	if len(candidates) == 0 {
		return nil
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
