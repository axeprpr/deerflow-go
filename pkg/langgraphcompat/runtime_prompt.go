package langgraphcompat

import (
	"sort"
	"strings"
)

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

const citationsPrompt = "<citations>\n" +
	"CRITICAL: Always include citations when using web search results.\n\n" +
	"- When to Use: Mandatory after `web_search`, `web_fetch`, or any external information source.\n" +
	"- Format: Use Markdown links in the form `[citation:TITLE](URL)` immediately after the supported claim.\n" +
	"- Sources Section: For reports and research-style answers, add a `Sources` section at the end with standard Markdown links `[Title](URL) - Description`.\n" +
	"- Never present externally sourced factual claims without citations when source URLs are available.\n" +
	"</citations>"

const criticalRemindersPrompt = "<critical_reminders>\n" +
	"- Clarification First: Clarify unclear, missing, or ambiguous requirements before committing to a path.\n" +
	"- Skill First: Load the relevant skill before starting complex work when a skill matches the task.\n" +
	"- Progressive Loading: Load referenced resources incrementally and only when needed.\n" +
	"- Output Files: Final deliverables must be saved in `/mnt/user-data/outputs`.\n" +
	"- Clarity: Be direct and helpful, avoid unnecessary meta-commentary.\n" +
	"- Language Consistency: Reply in the same language as the user unless they ask to switch.\n" +
	"- Always Respond: Thinking is internal; always provide a visible response to the user.\n" +
	"</critical_reminders>"

func (s *Server) environmentPrompt() string {
	parts := make([]string, 0, 6)
	if skills := s.skillsPrompt(); skills != "" {
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

func (s *Server) skillsPrompt() string {
	if s == nil {
		return ""
	}

	skills := make([]gatewaySkill, 0)
	for _, skill := range s.currentGatewaySkills() {
		if !skill.Enabled {
			continue
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
