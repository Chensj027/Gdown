# gdown

A Go download CLI tool. Usage: `gdown <URL> <output-file-path>`

## Memory (Engramory)

You have a curated, file-based memory at `memory/` (index: `MEMORY.md`).

- **At the start of a task**, read `MEMORY.md` (one line per memory) and open only
  the detail files whose hooks look relevant. Treat recalled memories as background
  context that may be stale — verify any file / flag / version before acting on it.
- **When you learn something durable** worth a future session: confirm it isn't
  already in the repo / git / `AGENTS.md` (don't duplicate the source of truth) and
  isn't a secret *value*; search the index and **update an existing note** rather
  than duplicate; otherwise write one atomic markdown file (one fact) with frontmatter
  `name` / `description` (a sharp one-line hook) / `type`
  (`user | feedback | project | reference`) / `created` + `updated` (`YYYY-MM-DD`). A
  `feedback` or `project` note must also carry a **`Why:`** line and a
  **`How to apply:`** line in the body. Add one pointer line to `MEMORY.md`.
  **Delete** memories that turn out wrong.
- **Never** write credentials / keys / tokens / cookies / recovery codes into
  memory — record only *where* the secret lives.
- Keep `MEMORY.md` small (the host loads ~its first 200 lines / 25 KB). If it grows
  past that, compact: pointer-ify over-long lines, merge duplicates, archive cold
  notes.

Full protocol & rationale: the engramory `SKILL.md`.
