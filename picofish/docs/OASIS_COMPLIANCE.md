# OASIS Compliance

This document maps PicoFish behaviors to the [OASIS multi-agent social simulation specification](https://github.com/camel-ai/oasis) (camel-ai/oasis).

---

## Agent types

| OASIS spec | PicoFish | Notes |
|---|---|---|
| `RecSysAgent` (feed algorithm) | `getFeed()` in `engine.go` | Full recency + popularity + stance scoring |
| `UserAgent` (individual) | `OasisAgentProfile{Type:"individual"}` | MBTI, emotional reasoning, 1–3 posts/hour |
| `UserAgent` (group/org) | `OasisAgentProfile{Type:"group"}` | 3–8 posts/hour, 2.5× feed influence weight |
| `TwitterAgent` | `platform == "twitter"` | CREATE_POST, LIKE, REPOST, REPLY, FOLLOW |
| `RedditAgent` | `platform == "reddit"` | CREATE_POST, COMMENT, UPVOTE, DOWNVOTE |
| `InstagramAgent` | `platform == "instagram"` | CREATE_POST, LIKE, COMMENT, SHARE, STORY |
| `TikTokAgent` | `platform == "tiktok"` | CREATE_VIDEO, LIKE, COMMENT, SHARE, REPOST |
| `WhatsAppAgent` | `platform == "whatsapp"` | SEND_MESSAGE, FORWARD, REACT |
| `FacebookAgent` | `platform == "facebook"` | CREATE_POST, LIKE, COMMENT, SHARE, REACT, JOIN_GROUP |

---

## Feed algorithm

OASIS specifies a feed ranking function combining recency, engagement, and social graph distance. PicoFish implements:

```
score = exp(-decay × age_hours)                     // recency
      + log(1 + likes + reposts) × popWeight        // popularity  
      + stanceBoost                                  // echo chamber
```

| OASIS parameter | PicoFish env var | Default |
|---|---|---|
| Recency decay λ | `RECENCY_DECAY_RATE` | 0.15 |
| Popularity weight | `POPULARITY_LOG_WEIGHT` | 0.3 |
| Same-stance boost | `ECHO_CHAMBER_BOOST` | 1.8× |
| Opposite-stance suppression | `OPP_STANCE_SUPPRESSION` | 0.4× |

Stance is stored as a string on each `OasisAgentProfile` (`"pro"`, `"anti"`, `"neutral"`). The feed algorithm compares the post author's stance against the reader's stance for scoring.

---

## Action types

All OASIS-specified action types are implemented:

| OASIS action | Platform | PicoFish status |
|---|---|---|
| CREATE_POST | Twitter, Reddit, Instagram, TikTok, Facebook | ✓ |
| LIKE_POST / LIKE | Twitter, Instagram, Facebook | ✓ |
| REPOST / SHARE | Twitter, Instagram, Facebook | ✓ |
| REPLY_TO_POST / COMMENT | Twitter, Reddit, Instagram | ✓ |
| FOLLOW / UNFOLLOW | Twitter (individual agents only) | ✓ |
| UPVOTE / DOWNVOTE | Reddit | ✓ |
| STORY | Instagram | ✓ |
| REEL | Instagram | ✓ |
| CREATE_VIDEO | TikTok | ✓ |
| SEND_MESSAGE | WhatsApp | ✓ |
| FORWARD | WhatsApp | ✓ |
| REACT | WhatsApp, Facebook | ✓ |
| JOIN_GROUP | Facebook | ✓ |

---

## Agent memory

| OASIS concept | PicoFish implementation |
|---|---|
| Short-term memory | Last N actions in agent context window |
| Long-term memory | `EpisodicMemory` — event log with embeddings, semantic retrieval |
| Belief store | `BeliefStore` — per-topic belief strength map |
| Belief decay | `BeliefDecayPerHour` applied each simulated hour |
| Belief revision | Cosine similarity < `ContradictionThreshold` triggers update |
| Social graph | FOLLOW/UNFOLLOW actions update adjacency; group agents immune |

---

## Temporal simulation

| OASIS concept | PicoFish implementation |
|---|---|
| Hourly time steps | `runLoop` iterates hours 1..N |
| Activity scheduling | Hour multiplier curve (dead hours skipped, peak at 0.8–0.95 × total_hours) |
| Dead hour skip | Multiplier < `DEAD_HOUR_THRESHOLD` → hour skipped entirely |
| Injection events | `InjectionEvent` queue, fires at specified hour |

---

## What OASIS specifies that PicoFish extends

PicoFish adds behaviors not in the original OASIS spec:

| Extension | Description |
|---|---|
| **Group agent type** | Institutional agents with different posting rates, influence multipliers, and memory capacity |
| **GraphRAG context** | Agent actions are informed by the knowledge graph via hop traversal, giving agents factual grounding |
| **Episodic memory retrieval** | Agents retrieve semantically relevant past events when generating content |
| **Self-critique report loop** | ReACT report goes through N critique passes before delivery |
| **Prediction calibration** | Brier score tracking for extracted predictions |
| **Simulation replay** | Hour-by-hour frame recording with export |
| **Multi-scenario comparison** | Statistical divergence across parallel simulation variants |
| **Mid-simulation injection** | Breaking news events with configurable visibility modes |
| **Influence network** | PageRank computation on post-simulation action graph |

---

## What OASIS specifies that PicoFish does not implement

| OASIS feature | Status | Reason |
|---|---|---|
| Cross-platform agent migration | Not implemented | Complex scheduling; not required for output parity |
| Verified account flag | Not implemented | Can be added as `agent.Verified bool` field |
| Shadow-ban simulation | Not implemented | Requires platform-level state not modeled |
| Ad injection | Not implemented | Out of scope for simulation research use |

These gaps represent potential contribution areas. See [CONTRIBUTING.md](../CONTRIBUTING.md) for guidance.
