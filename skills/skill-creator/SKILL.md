---
name: skill-creator
description: Create new skills for this agent. Use when the user wants to create a skill, add a new capability, define a reusable workflow, or says "create a skill" or "make a skill".
---

# Skill Creator

Create new skills by writing a SKILL.md file in the skills directory.

## Skill Format

Each skill is a directory containing a SKILL.md file with YAML frontmatter:

```
skills/
  my-skill/
    SKILL.md          # Required — instructions + frontmatter
    references/       # Optional — detailed docs loaded on demand
    scripts/          # Optional — executable helpers
```

### SKILL.md Structure

```markdown
---
name: my-skill
description: What this skill does and when to use it. Be specific about trigger phrases.
---

Instructions for the agent when this skill is activated.
```

## Creating a Skill

1. **Understand the intent**: What should the skill enable? When should it trigger?
2. **Create the directory**: `skills/<skill-name>/`
3. **Write SKILL.md**: Frontmatter (name + description) + markdown instructions
4. **Keep it focused**: Under 500 lines. Move detailed references to separate files.

## Writing Guidelines

- Use imperative form in instructions
- Explain **why**, not just what — the LLM is smart, give it reasoning
- Front-load the description with the key use case (max ~250 chars)
- Include examples when helpful
- For large reference material, put in `references/` and point to it from SKILL.md

## Progressive Disclosure

1. **Metadata** (name + description) — always in system prompt (~100 words)
2. **SKILL.md body** — loaded when skill is activated
3. **Bundled resources** — loaded on demand from references/scripts directories

## Example

```markdown
---
name: code-review
description: Review code changes for bugs, style issues, and improvements. Use when asked to review code, check a PR, or audit changes.
---

When reviewing code:

1. Read the diff or changed files
2. Check for bugs, edge cases, and error handling
3. Evaluate code style and consistency
4. Suggest specific improvements with examples
5. Summarize findings in a structured report
```
