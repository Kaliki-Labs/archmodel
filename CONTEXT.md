# Context

## Glossary

**Architecture model**: a machine-readable description of a project's structure: its modules, the dependencies between them, and the API operations, schemas and tables each one owns. Extracted from code, never hand-written. It is the single source that both the Explorer and the Guardrails read, so what a human sees and what an agent is held to cannot drift apart.

**Explorer**: the human-facing view of the Architecture model: navigate, drill down, understand. Never enforces anything.

**Guardrails**: the agent-facing side of the Architecture model, with two parts: *checks* (rules that fail with a fix hint) and *queries* (questions an agent asks the model instead of grepping).

**Module**: anything that shows up as a node in the Architecture model. Its kind is one of:
- **Domain**: a business capability treated as a vertical slice: its OpenAPI spec, backend package, tables, and (later) its generated client calls and frontend feature, all under one node.
- **Infrastructure module**: a supporting module that isn't a business capability (e.g. rate limiting, mail, config). Drillable exactly like a Domain; it owns no API operations.
- **Composition root**: the module(s) that wire everything together (e.g. `cmd/*`, `server`). Allowed to depend on anything; nothing may depend on it.
- **Foundation**: the innermost modules everything may use (e.g. config, generated DB access).
- **Test-only module**: support code only test files import. Excluded from the production graph.

**Level**: a Module kind's position in the dependency policy, from outermost to innermost: Composition root → Domain → Infrastructure → Foundation. Dependencies point inward only.

**Module card**: the summary shown for a Module at any Zoom level: purpose, public surface, depends-on, depended-on-by, what it owns (operations, tables, events), and its metrics.

**Zoom level**: how far the Explorer has drilled in: system → Modules → a Module's internals → individual types/functions.

**Architecture diff**: the change a branch makes to the Architecture model relative to its base: Modules, dependencies, operations and tables added or removed, new rule violations, and metric changes.

**Rationale**: the short "why" attached to every rule and metric. The same text is shown on a red edge in the Explorer, beside a metric on a Module card, and in a check's failure output, so it teaches the human and steers the agent at once.
