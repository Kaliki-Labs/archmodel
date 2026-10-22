<!-- archmodel-diff -->
### Architecture diff
1 new violation · modules -1 · dependencies +1 -2 · operations +1 ~1 · tables +1 ~1

**New violations**
- **no_domain_to_domain** `items -> auth`: Each Domain is a self-contained vertical slice. When one Domain imports another, they can no longer be understood, tested or changed independently, and a feature in one starts to hide inside the other. *Fix:* Declare a small interface in the consuming Domain describing only what it needs, and have the Composition root wire the other Domain into it.

**Modules**
- removed `push` (Infrastructure)

**Dependencies**
- added `items` -> `auth`
- removed `push` -> `config`
- removed `server` -> `push`

**Operations**
- added `POST /items/{id}/archive` (archiveItem, owner `items`)
- changed `POST /items` (createItem): security: bearerAuth -> public

**Tables**
- added `widgets` (3 columns)
- `items` columns: +`archived_at`

![changed subgraph](ARCHMODEL_IMAGE_URL)

<details><summary>Changed subgraph as Mermaid (text version)</summary>

```mermaid
flowchart TB
  n0["server"]
  n1["auth"]
  n2["items<br/>operations +1 ~1<br/>tables ~1"]
  n3["push (removed)"]
  n4["config"]
  n5["database"]
  n2 -->|added| n1
  n2 --> n5
  n3 -.->|removed| n4
  n0 -.->|removed| n3
  classDef composition_root fill:#7c4dbb,stroke:#7c4dbb,color:#fff
  classDef domain fill:#1f7ae0,stroke:#1f7ae0,color:#fff
  classDef foundation fill:#c07a10,stroke:#c07a10,color:#fff
  classDef infrastructure fill:#1a9a78,stroke:#1a9a78,color:#fff
  classDef added stroke:#1a8f3c,stroke-width:4px
  classDef removed stroke:#c0392b,stroke-width:3px,stroke-dasharray:6 3,opacity:0.7
  classDef changed stroke:#d97706,stroke-width:4px
  class n0 composition_root
  class n1,n2 domain
  class n4,n5 foundation
  class n3 infrastructure
  class n3 removed
  class n2 changed
  linkStyle 0 stroke:#d92d20,stroke-width:3px
  linkStyle 2 stroke:#c0392b
  linkStyle 3 stroke:#c0392b
```

</details>
