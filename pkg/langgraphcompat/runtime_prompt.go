package langgraphcompat

import "strings"

const workingDirectoryPrompt = "<working_directory existed=\"true\">\n" +
	"- User uploads: `/mnt/user-data/uploads` - Files uploaded by the user\n" +
	"- User workspace: `/mnt/user-data/workspace` - Working directory for temporary files\n" +
	"- Output files: `/mnt/user-data/outputs` - Final deliverables must be saved here\n\n" +
	"**File Management:**\n" +
	"- Uploaded files are automatically listed in the `<uploaded_files>` section before each request\n" +
	"- Use available file tools to inspect uploaded files using their listed paths\n" +
	"- For PDF, PPT, Excel, and Word files, converted Markdown versions (`*.md`) may be available alongside originals\n" +
	"- All temporary work happens in `/mnt/user-data/workspace`\n" +
	"- Final deliverables must be copied to `/mnt/user-data/outputs` and presented using `present_file` tool\n" +
	"</working_directory>"

const acpAgentPrompt = "**ACP Agent Tasks (`invoke_acp_agent`):**\n" +
	"- ACP agents run in their own independent workspace, not in `/mnt/user-data/`\n" +
	"- When writing prompts for ACP agents, describe the task only and do not reference `/mnt/user-data` paths\n" +
	"- ACP agent results are accessible at `/mnt/acp-workspace/` (read-only) and can be inspected with file tools or `bash cp`\n" +
	"- To deliver ACP output to the user: copy from `/mnt/acp-workspace/<file>` to `/mnt/user-data/outputs/<file>`, then use `present_file`"

func (s *Server) environmentPrompt() string {
	parts := []string{workingDirectoryPrompt}
	if s != nil && s.tools != nil && s.tools.Get("invoke_acp_agent") != nil {
		parts = append(parts, acpAgentPrompt)
	}
	return strings.Join(parts, "\n\n")
}
