+++
id = "dolt-schema"
title = "Design Dolt SQL schema for agentmail"
type = "task"
priority = 1
+++

Design and implement the SQL schema for the agentmail database. Create the file
`internal/agentmail/schema.sql` containing the table definitions, and a Go
function to initialize the database.

## Tables

### `agents`
Tracks registered agents and their heartbeat status.

```sql
CREATE TABLE agents (
    id VARCHAR(64) PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    role VARCHAR(64) NOT NULL,
    registered_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_heartbeat TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### `messages`
Inter-agent messages with optional channel targeting.

```sql
CREATE TABLE messages (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    sender_id VARCHAR(64) NOT NULL,
    channel VARCHAR(64) NOT NULL DEFAULT 'broadcast',
    subject VARCHAR(255) NOT NULL,
    body TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (sender_id) REFERENCES agents(id)
);
```

### `file_claims`
Advisory file locks — agents declare which files they're working on.

```sql
CREATE TABLE file_claims (
    file_path VARCHAR(512) NOT NULL,
    agent_id VARCHAR(64) NOT NULL,
    claimed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (file_path),
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);
```

### `changes`
Change announcements — agents declare what they modified and why.

```sql
CREATE TABLE changes (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    agent_id VARCHAR(64) NOT NULL,
    file_path VARCHAR(512) NOT NULL,
    summary TEXT NOT NULL,
    announced_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (agent_id) REFERENCES agents(id)
);
```

## Go initialization

Create `internal/agentmail/schema.go` with:

- An embedded `schema.sql` file using `//go:embed schema.sql`
- A function `InitDB(db *sql.DB) error` that executes each CREATE TABLE statement
- The function should be idempotent (use `CREATE TABLE IF NOT EXISTS`)

## Tests

Add `internal/agentmail/schema_test.go` that verifies `InitDB` runs without error
against a test Dolt instance (or use `sqltest` with an in-memory MySQL-compatible
driver if available).
