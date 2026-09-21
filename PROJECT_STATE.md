# Project State: SysAgent Engine

An elite, secure, and self-correcting Linux DevSecOps Orchestrator and Automation CLI tool that breaks down high-level linguistic instructions into structured execution pipelines with long-term memory RAG layers.

---

## 🛠️ Current Tech Stack
* **Language:** Go (Golang) 1.22+
* **Operating System:** Linux (Targeted deployment on host Debian/Ubuntu environments)
* **Database Engine:** SQLite (via `modernc.org/sqlite` pure-Go driver)
* **Inference Host:** Local Ollama API daemon instance servicing specialized weights on a dedicated host GPU (GTX 1080)
* **Models Utilized:**
  * **Planner Model (14B):** `qwen2.5-coder:14b` (Generates task-graph pipeline JSON execution schemas)
  * **Embedding Model:** `nomic-embed-text` (Generates vector weights for local semantic RAG features)
  * **Failure Interpreter Model (1.5B):** `qwen2.5-coder:1.5b` (Instruct/Chat model variant providing precise error diagnosis passes)

---

## 🗄️ Database Architecture Schema
The system uses a single unified SQLite file (`agent_knowledge.db`) consisting of isolated tables handling vector telemetry logs and short-term conversational context tracking.

### 1. `telemetry_logs`
Tracks historical task outputs alongside their high-dimensional vector embeddings for long-term RAG search injection.
```sql
CREATE TABLE IF NOT EXISTS telemetry_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT,
    command TEXT,
    success INTEGER,
    error_message TEXT,
    embedding TEXT, -- Marshaled JSON array string of float32 weights
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### 2. `chat_sessions` (Target Expansion Spec)
Tracks high-level conversational workspaces to allow multi-run continuity tracks.
```sql
CREATE TABLE IF NOT EXISTS chat_sessions (
    id TEXT PRIMARY KEY, -- Unique session identifier slug/UUID
    title TEXT DEFAULT 'Active Session',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

### 3. `session_history` (Target Expansion Spec)
Records specific interaction execution logs mapped relationally to a parent chat thread session.
```sql
CREATE TABLE IF NOT EXISTS session_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id TEXT,
    user_prompt TEXT,
    execution_plan TEXT, -- Raw machine-readable JSON text map of planned pipelines
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY(session_id) REFERENCES chat_sessions(id) ON DELETE CASCADE
);
```

---

## 🔌 Active API Endpoints
All operations communicate locally with the native Ollama daemon instance via an optimized custom HTTP client interface:
* **`/api/embeddings`** – Generates mathematical vector coordinates for user queries inside `GetEmbedding`.
* **`/api/generate`** – Interacts with the `qwen2.5-coder:14b` structural completion engine to derive pipeline graphs.
* **`/api/chat`** – Interacts with the `qwen2.5-coder:1.5b` Instruct model via structured `system`/`user` text frames to safely process failure interpretations.

---

## ⚡ Recent Structural & Architectural Changes
* **`sort.Slice` Precision Upgrades:** Refactored the semantic search engine to sort arrays in descending order by cosine similarity score. The `--forget` command confidently isolates and presents the highest percentage match at array index `0`.
* **Hybrid Privilege Containment:** Fixed `fork/exec` permissions crashes. Commands are sandboxed by default under an unprivileged system user (`sysagent`). When `sudo` is detected in a plan, the execution engine dynamically triggers an **Elevation Bypass Gateway**, binding straight to the host TTY (`os.Stdin`) to permit secure keyboard password input.
* **Ollama Client Hardening:** Integrated a strict 10-second `http.Client{Timeout}` guardrail into the network dispatcher wrapper to prevent background locking freezes.
* **Graceful Signal Catching:** Appended an active OS signal monitor listener thread catching `Ctrl+C` interrupts. If a run is terminated early, the agent sends asynchronous cancel flushes to Ollama's endpoints, instantly unloading memory weights from VRAM.
* **Interpreter Loop Patch:** Abandoned raw `-base` text completion prompting to prevent runaway infinite generation repetition cascades. Re-routed failure parsing to the `/api/chat` instruct endpoint using an uncapped token ceiling for precise, clean output summaries.

---

## 🎯 Immediate Goal & Next Steps
**Goal:** Implement the Multi-Run Conversational Session Matrix to unlock persistent continuity memory across separate CLI execution sessions.

### Step 1: Database Migration Expansion
Extend `pkg/db/store.go` to include the relational `chat_sessions` and `session_history` schemas.

### Step 2: Implement Core Storage & Retrieval Logic
Write the backend database query drivers inside `pkg/db/store.go`:
* `LogSessionInteraction(...)`
* `GetLastSessionInteraction(...)`
* `ListAllSessions(searchQuery string)`

### Step 3: Integrate Session Control Subcommands
Wire the new CLI routing matrix properties inside `cmd/sysagent/main.go` and `cmd_run.go`:
* **`--session <id>`** – Contextualizes execution under a specific session thread track.
* **`--list-sessions`** – Displays active session names, optionally filtering by string search patterns.
