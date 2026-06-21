package compaction

const defaultPrompt = `You are creating a context handoff summary for another agent that will resume this conversation.

First, analyze the conversation history to identify:
- The primary goal or task the user is trying to accomplish
- Key decisions, constraints, preferences, and rules established
- Completed work and successful outcomes
- Current state and work in progress
- Pending tasks and next steps required

Do not include system prompt information (identity, guardrails, or application configuration) in the summary. These are added separately by the receiving agent and do not need to be preserved.

Then, output ONLY a structured summary using the following markdown sections. Do not include any analysis, reasoning, introductory text, or commentary outside these sections. The summary must be concise but complete — another agent will resume work from this summary.

## Primary Goal
[What the user is trying to accomplish]

## Key Decisions & Constraints
[Important rules, preferences, architectural choices, or constraints established]

## Completed Work
[What has already been successfully done]

## Current State / Work in Progress
[What is actively being worked on, including any in-progress decisions or partial results]

## Pending Tasks & Next Steps
[What still needs to be done to complete the goal]`
