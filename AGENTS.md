# Working in this repository

## Track everything in Trackstar

This project is managed in Trackstar itself: the project with slug `trackstar`,
reached through the `trackstar` MCP server (`list_stories`, `create_story`,
`update_story`, `move_story`, `add_comment`, …). The tracker is the single
record of what is planned and what was done. Keep it current as you go, not
in one pass at the end.

**Anything planned becomes stories, not documents.** Do not add plan,
roadmap or TODO files to the repository, and do not write "future work"
sections into the README. The README describes what exists today.

- Before planning, search the tracker (`list_stories` with `query`) so you
  extend what is there instead of duplicating it.
- One story per deliverable slice, written the way the existing ones are: a
  title that states the outcome, "As a …, I want …, so that …" for features,
  then an `Acceptance:` list that can be checked. Put design decisions and
  open questions for the user in the first story of the plan.
- Give every story of a plan the same label (`organizations`, `postgres`,
  `mcp-oauth`), and set `blocked_by` where one story needs another.
- Plans go to the icebox. A new story lands at the top of its section, so
  move the stories into the order they should be done in.
- Features get an estimate in points (1, 2, 3, 5). Bugs and chores do not.

**Anything done has a story.** If you are about to change the repository and
no story covers it, create one first (bug, chore or feature) in `current`.
This includes small fixes and work the user asks for directly.

- Starting work: set the story to `started` and make yourself (the token's
  user) the owner.
- While working: when a decision is made, the scope changes, or you get
  blocked, add a comment then. Problems you notice that are outside the
  story get their own story (bugs in the backlog, ideas in the icebox)
  instead of being fixed silently or forgotten.
- Code and tests done: set the story to `finished` and add a comment saying
  what was built, where, which tests were run and their result, and anything
  that deviates from the acceptance criteria or was left out.
- In a plan with several stories, finish and update each story before
  starting the next.
- `delivered` and `accepted` belong to the user: they mean deployed and
  signed off. Do not set them. Work that needs the deployed instance or the
  user's own accounts stays unstarted, with a comment saying what is ready.

If the `trackstar` MCP tools are not available in a session, say so at the
start instead of working untracked.
