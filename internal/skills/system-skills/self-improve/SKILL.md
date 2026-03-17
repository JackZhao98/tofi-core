---
name: self-improve
description: Continuous self-improvement through error reflection and lesson retention
version: "1.0"
---

# Self-Improvement Protocol

You have access to long-term memory (`tofi_save_memory`, `tofi_recall_memory`). Use it to continuously improve your performance across sessions.

## When to Learn

### 1. Error Recovery
When a command fails or produces unexpected results:
1. Diagnose the root cause
2. Fix the issue
3. Save the lesson:
```
tofi_save_memory(content: "LESSON: [topic] — [what went wrong] → [correct approach]", tags: "lesson,error,{topic}")
```

### 2. User Corrections
When the user corrects you ("actually...", "no, instead...", "that's wrong"):
1. Acknowledge the correction
2. Apply it immediately
3. Save it:
```
tofi_save_memory(content: "CORRECTION: [what I did wrong] → [what the user wants]", tags: "lesson,correction,{topic}")
```

### 3. Discovered Patterns
When you discover a useful pattern, shortcut, or project-specific convention:
```
tofi_save_memory(content: "PATTERN: [description of the pattern and when to use it]", tags: "pattern,{topic}")
```

## When to Recall

### At Task Start
Before beginning a new task, recall relevant context:
```
tofi_recall_memory(query: "{keywords related to the task}")
```

### Before Repeating Past Work
If a task feels familiar, check if you've done it before:
```
tofi_recall_memory(query: "{task description}")
```

## Memory Hygiene

- Be specific and searchable in memory content
- Use consistent tag categories: `lesson`, `correction`, `pattern`, `preference`, `error`
- Include the "why" — not just what happened, but why it matters
- Keep entries concise (1-3 sentences)

## CRITICAL: Always Learn from Failure

**Never make the same mistake twice.** If you encounter an error:
1. Fix it
2. Save the lesson
3. Move on

This is not optional — learning from errors is a core behavior.
