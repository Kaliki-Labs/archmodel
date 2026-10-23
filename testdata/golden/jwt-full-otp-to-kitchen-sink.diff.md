<!-- archmodel-diff -->
### Architecture diff
modules +5 · dependencies +12 · operations -1 ~1 · tables +1 ~1

**Modules**
- added `cmd/workers` (Composition root)
- added `events` (Infrastructure)
- added `payments` (Infrastructure)
- added `push` (Infrastructure)
- added `storage` (Infrastructure)

**Dependencies**
- added `cmd/workers` -> `config`, `database`, `events`, `observability`
- added `events` -> `config`
- added `payments` -> `config`
- added `push` -> `config`
- added `server` -> `events`, `payments`, `push`, `storage`
- added `storage` -> `config`

**Operations**
- removed `POST /auth/verify/resend` (resendVerification, owner `auth`)
- changed `POST /auth/verify` (verifyEmail): rate limit changed

**Tables**
- added `outbox` (6 columns)
- `auth_tokens` columns: -`attempts`

![changed subgraph](large.png)

<details><summary>Changed subgraph as Mermaid (text version)</summary>

```mermaid
flowchart TB
  n0["cmd/workers (added)"]
  n1["server"]
  n2["auth<br/>operations -1 ~1<br/>tables ~1"]
  n3["events (added)<br/>tables +1"]
  n4["mail"]
  n5["observability"]
  n6["payments (added)"]
  n7["push (added)"]
  n8["ratelimit"]
  n9["storage (added)"]
  n10["config"]
  n11["database"]
  n2 --> n4
  n2 --> n8
  n0 -->|added| n10
  n0 -->|added| n11
  n0 -->|added| n3
  n0 -->|added| n5
  n3 -->|added| n10
  n6 -->|added| n10
  n7 -->|added| n10
  n1 -->|added| n3
  n1 -->|added| n6
  n1 -->|added| n7
  n1 -->|added| n9
  n9 -->|added| n10
  classDef composition_root fill:#7c4dbb,stroke:#7c4dbb,color:#fff
  classDef domain fill:#1f7ae0,stroke:#1f7ae0,color:#fff
  classDef foundation fill:#c07a10,stroke:#c07a10,color:#fff
  classDef infrastructure fill:#1a9a78,stroke:#1a9a78,color:#fff
  classDef added stroke:#1a8f3c,stroke-width:4px
  classDef removed stroke:#c0392b,stroke-width:3px,stroke-dasharray:6 3,opacity:0.7
  classDef changed stroke:#d97706,stroke-width:4px
  class n0,n1 composition_root
  class n2 domain
  class n10,n11 foundation
  class n3,n4,n5,n6,n7,n8,n9 infrastructure
  class n0,n3,n6,n7,n9 added
  class n2 changed
  linkStyle 2 stroke:#1a8f3c,stroke-width:2px
  linkStyle 3 stroke:#1a8f3c,stroke-width:2px
  linkStyle 4 stroke:#1a8f3c,stroke-width:2px
  linkStyle 5 stroke:#1a8f3c,stroke-width:2px
  linkStyle 6 stroke:#1a8f3c,stroke-width:2px
  linkStyle 7 stroke:#1a8f3c,stroke-width:2px
  linkStyle 8 stroke:#1a8f3c,stroke-width:2px
  linkStyle 9 stroke:#1a8f3c,stroke-width:2px
  linkStyle 10 stroke:#1a8f3c,stroke-width:2px
  linkStyle 11 stroke:#1a8f3c,stroke-width:2px
  linkStyle 12 stroke:#1a8f3c,stroke-width:2px
  linkStyle 13 stroke:#1a8f3c,stroke-width:2px
```

</details>
