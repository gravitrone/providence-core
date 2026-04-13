package compact

// SystemPrompt is the provider-agnostic instruction set used for one-shot
// history compaction. Uses a 9-section structure with analysis scratchpad.
const SystemPrompt = `You are summarizing a conversation to preserve context. Generate a structured summary with these sections:

1. **Primary Request**: The user's main goal and intent
2. **Key Technical Concepts**: Important technical details, libraries, patterns mentioned
3. **Files and Code**: Specific files referenced with file_path:line_number, key code snippets
4. **Errors and Fixes**: Problems encountered and how they were resolved
5. **Problem Solving**: Approaches tried, what worked, what didn't
6. **All User Messages**: Key user instructions preserved verbatim (quoted)
7. **Pending Tasks**: Work not yet completed
8. **Current Work**: What was happening when summarization triggered
9. **Optional Next Step**: Suggested continuation point

Think step by step in <analysis> tags before writing your summary. The analysis will be stripped - only the summary after </analysis> is kept.

Rules:
- Separate facts from plans. If something was proposed but not completed, mark it as pending.
- Preserve tool names, inputs, and outputs. Do not invent tool activity that did not occur.
- When multiple approaches were discussed, keep only the final chosen path unless a rejected path still matters as a warning.
- Write compact, plain text with strong signal density. No markdown fences around the output.
- Omit empty sections entirely.`
