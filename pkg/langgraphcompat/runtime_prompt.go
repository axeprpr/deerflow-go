package langgraphcompat

import (
	"sort"
	"strconv"
	"strings"
)

const thinkingStylePrompt = "<thinking_style>\n" +
	"- Think concisely and strategically before taking action.\n" +
	"- Break down the task into what is clear, what is ambiguous, and what is missing.\n" +
	"- PRIORITY CHECK: If anything is unclear, missing, or has multiple interpretations, ask for clarification FIRST before doing work.\n" +
	"- Never write your full final answer inside hidden reasoning; use reasoning only to plan.\n" +
	"- After planning, always provide the user-facing answer or continue with the next visible action.\n" +
	"</thinking_style>"

const clarificationSystemPrompt = "<clarification_system>\n" +
	"**WORKFLOW PRIORITY: CLARIFY -> PLAN -> ACT**\n" +
	"1. **FIRST**: Analyze the request in your thinking - identify what is unclear, missing, or ambiguous\n" +
	"2. **SECOND**: If clarification is needed, call `ask_clarification` immediately - do NOT start working\n" +
	"3. **THIRD**: Only after clarifications are resolved, proceed with planning and execution\n\n" +
	"**CRITICAL RULE: Clarification ALWAYS comes BEFORE action. Never start working and clarify mid-execution.**\n\n" +
	"**MANDATORY Clarification Scenarios - You MUST call ask_clarification BEFORE starting work when:**\n\n" +
	"1. **Missing Information** (`missing_info`): Required details are not provided\n" +
	"2. **Ambiguous Requirements** (`ambiguous_requirement`): Multiple valid interpretations exist\n" +
	"3. **Approach Choices** (`approach_choice`): Several valid approaches exist and user preference matters\n" +
	"4. **Risky Operations** (`risk_confirmation`): Destructive or high-impact changes need confirmation\n" +
	"5. **Suggestions** (`suggestion`): You have a recommendation but need approval before proceeding\n\n" +
	"**STRICT ENFORCEMENT:**\n" +
	"- DO NOT start working and then ask for clarification mid-execution\n" +
	"- DO NOT skip clarification for speed\n" +
	"- DO NOT proceed with guesses when key information is missing\n" +
	"- After calling `ask_clarification`, execution will be interrupted automatically\n" +
	"</clarification_system>"

const workingDirectoryPrompt = "<working_directory existed=\"true\">\n" +
	"- User uploads: `/mnt/user-data/uploads` - Files uploaded by the user (automatically listed in context)\n" +
	"- User workspace: `/mnt/user-data/workspace` - Working directory for temporary files\n" +
	"- Output files: `/mnt/user-data/outputs` - Final deliverables must be saved here\n\n" +
	"**File Management:**\n" +
	"- Uploaded files are automatically listed in the `<uploaded_files>` section before each request\n" +
	"- Use `read_file` tool to read uploaded files using their paths from the list\n" +
	"- For PDF, PPT, Excel, and Word files, converted Markdown versions (`*.md`) are available alongside originals\n" +
	"- All temporary work happens in `/mnt/user-data/workspace`\n" +
	"- Final deliverables must be copied to `/mnt/user-data/outputs` and presented using `present_file` tool\n" +
	"</working_directory>"

const acpAgentPrompt = "**ACP Agent Tasks (`invoke_acp_agent`):**\n" +
	"- ACP agents run in their own independent workspace, not in `/mnt/user-data/`\n" +
	"- When writing prompts for ACP agents, describe the task only and do not reference `/mnt/user-data` paths\n" +
	"- ACP agent results are accessible at `/mnt/acp-workspace/` (read-only) and can be inspected with file tools or `bash cp`\n" +
	"- To deliver ACP output to the user: copy from `/mnt/acp-workspace/<file>` to `/mnt/user-data/outputs/<file>`, then use `present_file`"

const responseStylePrompt = "<response_style>\n" +
	"- Clear and Concise: Avoid over-formatting unless requested\n" +
	"- Natural Tone: Use paragraphs and prose, not bullet points by default\n" +
	"- Action-Oriented: Focus on delivering results, not explaining processes\n" +
	"</response_style>"

const citationsPrompt = "<citations>\n" +
	"**CRITICAL: Always include citations when using web search results**\n\n" +
	"- **When to Use**: MANDATORY after web_search, web_fetch, or any external information source\n" +
	"- **Format**: Use Markdown link format `[citation:TITLE](URL)` immediately after the claim\n" +
	"- **Placement**: Inline citations should appear right after the sentence or claim they support\n" +
	"- **Sources Section**: Also collect all citations in a \"Sources\" section at the end of reports\n\n" +
	"**WORKFLOW for Research Tasks:**\n" +
	"1. Use web_search to find sources -> Extract {title, url, snippet} from results\n" +
	"2. Write content with inline citations: `claim [citation:Title](url)`\n" +
	"3. Collect all citations in a \"Sources\" section at the end\n" +
	"4. NEVER write claims without citations when sources are available\n\n" +
	"**CRITICAL RULES:**\n" +
	"- DO NOT write research content without citations\n" +
	"- DO NOT forget to extract URLs from search results\n" +
	"- ALWAYS add `[citation:Title](URL)` after claims from external sources\n" +
	"- ALWAYS include a \"Sources\" section listing all references\n" +
	"</citations>"

const criticalRemindersPrompt = "<critical_reminders>\n" +
	"- **Clarification First**: ALWAYS clarify unclear/missing/ambiguous requirements BEFORE starting work - never assume or guess\n" +
	"- Skill First: Always load the relevant skill before starting **complex** tasks.\n" +
	"- Progressive Loading: Load resources incrementally as referenced in skills\n" +
	"- Output Files: Final deliverables must be in `/mnt/user-data/outputs`\n" +
	"- Clarity: Be direct and helpful, avoid unnecessary meta-commentary\n" +
	"- Including Images and Mermaid: Images and Mermaid diagrams are always welcomed in Markdown, and you are encouraged to use `![Image Description](image_path)` or fenced Mermaid blocks when useful\n" +
	"- Multi-task: Better utilize parallel tool calling to call multiple tools at one time for better performance\n" +
	"- Language Consistency: Keep using the same language as user's\n" +
	"- Always Respond: Your thinking is internal. You MUST always provide a visible response to the user after thinking.\n" +
	"</critical_reminders>"

func subagentPrompt(maxConcurrent int) string {
	if maxConcurrent <= 0 {
		maxConcurrent = defaultGatewaySubagentMaxConcurrent
	}
	limit := strconv.Itoa(maxConcurrent)
	return "<subagent_system>\n" +
		"SUBAGENT MODE ACTIVE. When the task is complex and decomposable, act as an orchestrator: decompose, delegate, and synthesize.\n\n" +
		"Rules:\n" +
		"1. Use `task` only when there are 2 or more meaningful sub-tasks that benefit from parallel execution.\n" +
		"2. You may launch at most " + limit + " `task` calls in one response.\n" +
		"3. If the work has more than " + limit + " sub-tasks, batch them across multiple turns.\n" +
		"4. If the task is simple, tightly sequential, or needs clarification first, do it directly instead of using subagents.\n" +
		"5. After delegated work finishes, synthesize all results into one coherent answer.\n" +
		"</subagent_system>"
}

func (s *Server) environmentPrompt(runtimeContext map[string]any, skillNames ...string) string {
	parts := make([]string, 0, 9)
	parts = append(parts, thinkingStylePrompt)
	parts = append(parts, clarificationSystemPrompt)
	if boolFromAny(runtimeContext["subagent_enabled"]) {
		parts = append(parts, subagentPrompt(intValueFromAny(runtimeContext["max_concurrent_subagents"], defaultGatewaySubagentMaxConcurrent)))
	}
	if skills := s.skillsPrompt(skillNames...); skills != "" {
		parts = append(parts, skills)
	}
	parts = append(parts, workingDirectoryPrompt)
	if s != nil && s.tools != nil && s.tools.Get("invoke_acp_agent") != nil {
		parts = append(parts, acpAgentPrompt)
	}
	parts = append(parts, responseStylePrompt)
	parts = append(parts, citationsPrompt)
	parts = append(parts, criticalRemindersPrompt)
	return strings.Join(parts, "\n\n")
}

func (s *Server) runtimeSkillPaths(skillNames ...string) map[string]any {
	if s == nil {
		return nil
	}

	allowed := make(map[string]struct{}, len(skillNames))
	for _, name := range skillNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}

	out := map[string]any{}
	for _, skill := range s.currentGatewaySkills() {
		if !skill.Enabled {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[skill.Name]; !ok {
				continue
			}
		}
		if _, ok := s.loadGatewaySkillBody(skill.Name, skill.Category); !ok {
			continue
		}
		category := resolveSkillCategory(skill.Category, skillCategoryPublic)
		out[skill.Name] = "/mnt/skills/" + category + "/" + skill.Name + "/SKILL.md"
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func intValueFromAny(raw any, fallback int) int {
	if value := intPointerFromAny(raw); value != nil {
		return *value
	}
	return fallback
}

func (s *Server) skillsPrompt(skillNames ...string) string {
	if s == nil {
		return ""
	}

	allowed := make(map[string]struct{}, len(skillNames))
	for _, name := range skillNames {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		allowed[name] = struct{}{}
	}

	skillsByKey := make(map[string]gatewaySkill)
	for _, skill := range s.currentGatewaySkills() {
		if !skill.Enabled {
			continue
		}
		if len(allowed) > 0 {
			if _, ok := allowed[skill.Name]; !ok {
				continue
			}
		}
		if _, ok := s.loadGatewaySkillBody(skill.Name, skill.Category); !ok {
			continue
		}
		skillsByKey[skillStorageKey(skill.Category, skill.Name)] = skill
	}
	for name := range allowed {
		if name == "" {
			continue
		}
		if _, ok := findGatewaySkill(skillsByKey, name, ""); ok {
			continue
		}
		body, ok := s.loadGatewaySkillBody(name, "")
		if !ok || strings.TrimSpace(body) == "" {
			continue
		}
		skillsByKey[skillStorageKey(skillCategoryPublic, name)] = gatewaySkill{
			Name:        name,
			Description: "Internal skill loaded by explicit runtime request.",
			Category:    skillCategoryPublic,
			Enabled:     true,
		}
	}

	skills := make([]gatewaySkill, 0, len(skillsByKey))
	for _, skill := range skillsByKey {
		skills = append(skills, skill)
	}
	if len(skills) == 0 {
		return ""
	}

	sort.Slice(skills, func(i, j int) bool {
		if skills[i].Category == skills[j].Category {
			return skills[i].Name < skills[j].Name
		}
		return skills[i].Category < skills[j].Category
	})

	var b strings.Builder
	b.WriteString("<skill_system>\n")
	b.WriteString("You have access to skills that provide optimized workflows for specific tasks. Each skill contains best practices, frameworks, and references to additional resources.\n\n")
	b.WriteString("**Progressive Loading Pattern:**\n")
	b.WriteString("1. When a user query matches a skill's use case, immediately call `read_file` on the skill's main file using the path attribute provided in the skill tag below.\n")
	b.WriteString("2. Read and understand the skill's workflow and instructions before starting the main work.\n")
	b.WriteString("3. The skill file may reference external resources under the same folder.\n")
	b.WriteString("4. Load referenced resources only when needed during execution.\n")
	b.WriteString("5. Follow the skill's instructions precisely.\n")
	b.WriteString("\n")
	b.WriteString("**Skills are located at:** /mnt/skills\n\n")
	b.WriteString("<available_skills>\n")
	for _, skill := range skills {
		category := resolveSkillCategory(skill.Category, skillCategoryPublic)
		b.WriteString("    <skill>\n")
		b.WriteString("        <name>" + skill.Name + "</name>\n")
		b.WriteString("        <description>" + skill.Description + "</description>\n")
		b.WriteString("        <location>/mnt/skills/" + category + "/" + skill.Name + "/SKILL.md</location>\n")
		b.WriteString("    </skill>\n")
	}
	b.WriteString("</available_skills>\n")
	b.WriteString("</skill_system>")
	return b.String()
}
