package langgraphcompat

import (
	"sort"
	"strconv"
	"strings"
)

const thinkingStylePrompt = "<thinking_style>\n" +
	"- Think concisely and strategically before taking action.\n" +
	"- Break down the task into what is clear, what is ambiguous, and what is missing.\n" +
	"- Never write your full final answer inside hidden reasoning; use reasoning only to plan.\n" +
	"- After planning, always provide the user-facing answer or continue with the next visible action.\n" +
	"</thinking_style>"

const clarificationWorkflowPrompt = "<clarification_system>\n" +
	"WORKFLOW PRIORITY: CLARIFY -> PLAN -> ACT\n" +
	"1. Analyze the request first.\n" +
	"2. If required information is missing or ambiguous, call `ask_clarification` immediately.\n" +
	"3. Only continue execution after the clarification is resolved.\n\n" +
	"Ask for clarification before acting when:\n" +
	"- required details are missing\n" +
	"- multiple interpretations are valid\n" +
	"- there are meaningful approach choices the user should make\n" +
	"- the operation is destructive or risky\n" +
	"- you have a recommendation that needs user approval\n\n" +
	"Do not start working and then clarify mid-execution.\n" +
	"</clarification_system>"

const workingDirectoryPrompt = "<working_directory existed=\"true\">\n" +
	"- User uploads: `/mnt/user-data/uploads` - Files uploaded by the user\n" +
	"- User workspace: `/mnt/user-data/workspace` - Working directory for temporary files\n" +
	"- Output files: `/mnt/user-data/outputs` - Final deliverables must be saved here\n\n" +
	"**File Management:**\n" +
	"- Uploaded files are automatically listed in the `<uploaded_files>` section before each request\n" +
	"- Use available file tools to inspect uploaded files using their listed paths\n" +
	"- For PDF, PPT, Excel, and Word files, converted Markdown versions (`*.md`) may be available alongside originals\n" +
	"- All temporary work happens in `/mnt/user-data/workspace`\n" +
	"- Final deliverables must be copied to `/mnt/user-data/outputs` and presented using `present_files` tool\n" +
	"</working_directory>"

const acpAgentPrompt = "**ACP Agent Tasks (`invoke_acp_agent`):**\n" +
	"- ACP agents run in their own independent workspace, not in `/mnt/user-data/`\n" +
	"- When writing prompts for ACP agents, describe the task only and do not reference `/mnt/user-data` paths\n" +
	"- ACP agent results are accessible at `/mnt/acp-workspace/` (read-only) and can be inspected with file tools or `bash cp`\n" +
	"- To deliver ACP output to the user: copy from `/mnt/acp-workspace/<file>` to `/mnt/user-data/outputs/<file>`, then use `present_files`"

const responseStylePrompt = "<response_style>\n" +
	"- Clear and Concise: Avoid over-formatting unless requested\n" +
	"- Natural Tone: Use paragraphs and prose, not bullet points by default\n" +
	"- Action-Oriented: Focus on delivering results, not explaining processes\n" +
	"</response_style>"

const criticalRemindersPrompt = "<critical_reminders>\n" +
	"- Clarification First: Clarify unclear, missing, or ambiguous requirements before committing to a path.\n" +
	"- Skill First: Load the relevant skill before starting complex work when a skill matches the task.\n" +
	"- Progressive Loading: Load referenced resources incrementally and only when needed.\n" +
	"- Output Files: Final deliverables must be saved in `/mnt/user-data/outputs`.\n" +
	"- Clarity: Be direct and helpful, avoid unnecessary meta-commentary.\n" +
	"- Language Consistency: Reply in the same language as the user unless they ask to switch.\n" +
	"- Always Respond: Thinking is internal; always provide a visible response to the user.\n" +
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
	parts := make([]string, 0, 8)
	parts = append(parts, thinkingStylePrompt, clarificationWorkflowPrompt)
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
	parts = append(parts, criticalRemindersPrompt)
	return strings.Join(parts, "\n\n")
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

	skills := make([]gatewaySkill, 0)
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
	b.WriteString("You have access to skills that provide optimized workflows for specific tasks. Each skill contains instructions, best practices, and references to extra resources.\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("1. When a user request matches a skill, read that skill's `SKILL.md` with `read_file` before starting the main work.\n")
	b.WriteString("2. Follow the skill's workflow and only load extra files it references when needed.\n")
	b.WriteString("3. Prefer the paths listed below when reading skill files.\n\n")
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
