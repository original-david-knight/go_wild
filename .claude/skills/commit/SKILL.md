---
name: Commit
description: >
  Build, test, and commit changes with a generated commit message.
  Verifies compilation and tests pass before committing.
---

# Commit Skill

1. Run `go build ./...` to verify compilation. If it fails, stop and report errors.
2. Run `go test ./...` to verify tests pass. If any fail, stop and report failures.
3. Run `git diff --stat` and `git diff` to review all changes.
4. Generate a concise commit message based on the diff. Focus on the "why" not the "what".
5. Stage and commit: `git add -A && git commit` with the generated message.
6. Ask the user if they want to push.
