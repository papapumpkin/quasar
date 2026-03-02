# internal/board vs internal/fabric Analysis

## Executive Summary
**board is NOT a predecessor of fabric — fabric is the renamed, active replacement.**

On Feb 23, 2026 (commit 1e6cd62), the internal/board package was renamed to internal/fabric with semantic vocabulary changes. The board files remain as empty stubs (only `package board` declarations, 14 bytes each) because git prevents deletion. **fabric is the current, production package.**

---

## Detailed Comparison

### internal/board/ (Deprecated Stubs)
**Status**: Dead code - empty package stubs only

Files (all 14 bytes, just `package board`):
- board.go
- contract.go
- llmpoller.go
- poller.go
- publisher.go
- pushback.go
- sqlite.go

**Why they're empty**: Git deletion constraints during refactor. These files are no longer imported or used anywhere in the codebase.

**Last meaningful content**: Deleted in commit 1e6cd62 via refactoring from board → fabric. All prior implementation moved to fabric package.

---

### internal/fabric/ (Active Package)
**Status**: Production code with full implementation and test coverage

**Total files**: 11 non-test .go files

#### Core Type Definitions (fabric.go)
- **Entanglement**: Published interface contract (types, functions, interfaces, methods, packages, files)
  - Producer (phase that created it)
  - Consumer (optional)
  - Kind, Name, Signature, Package
  - Status: fulfilled/disputed/pending
  
- **Discovery**: Agent-surfaced issues requiring human or automated attention
  - Kind: entanglement_dispute, missing_dependency, file_conflict, requirements_ambiguity, budget_alert
  
- **Pulse**: Structured context emission from phases during execution
  - Kind: note, decision, failure, reviewer_feedback
  
- **Claim**: File ownership claim by a phase

- **Fabric Interface**: Central shared state store (18 methods)
  - Phase state tracking (SetPhaseState, GetPhaseState, AllPhaseStates)
  - Entanglement management (PublishEntanglement/s, EntanglementsFor, AllEntanglements)
  - File claims (ClaimFile, ReleaseClaims, ReleaseFileClaim, FileOwner, ClaimsFor, AllClaims)
  - Discovery handling (PostDiscovery, Discoveries, AllDiscoveries, ResolveDiscovery, UnresolvedDiscoveries)
  - Pulse emission (EmitPulse, PulsesFor, AllPulses)
  - Data purging (PurgeAll, PurgeFulfilledEntanglements, Close)

#### Module Breakdown

**1. fabric.go** (203 lines)
- Exports: Entanglement, Discovery, Pulse, Claim, Fabric interface
- Constants: phase states, entanglement kinds, statuses, pulse kinds
- Functions: ValidatePulseKind, ValidateDiscoveryKind
- **Purpose**: Type definitions and core interfaces for the fabric system
- **Internal imports**: None
- **Related to board**: This replaced the "board" concept with "fabric" vocabulary

**2. contracts.go** (144 lines)
- Exports: ContractEntry, ContractReport, ResolveContracts
- **Purpose**: Static analysis of phase contracts (producer-consumer relationships)
  - Checks fulfillment: consumer expects X, producer provides X
  - Identifies missing: consumer expects X, no producer found
  - Detects conflicts: multiple producers for same symbol
- **Key function**: ResolveContracts() — deterministic set-intersection checks against dependency graph
- **Internal imports**: None
- **New in fabric**: This was likely PhaseContract analysis in board

**3. contract_poller.go** (106 lines)
- Exports: MatchMode, ContractPoller, PollResult (from poller.go)
- **Purpose**: Deterministic polling based on static entanglement contracts
  - No LLM calls — pure set-intersection checks
  - MatchMode: Exact (kind+package+name) or Name-only (kind+name)
  - Returns: PollProceed (all contracts fulfilled), PollNeedInfo (missing entanglements), PollConflict (scope file claimed)
- **Key method**: Poll(ctx, phaseID, snap) → PollResult
- **Internal imports**: None
- **New in fabric**: Deterministic alternative to LLMPoller

**4. discovery.go** (45 lines)
- Exports: ValidateDiscoveryKind, Discovery.IsHail(), PendingHails()
- **Purpose**: Discovery kind validation and filtering
  - IsHail(): Returns true if discovery requires human attention (budget_alert is informational only)
  - PendingHails(): Returns unresolved discoveries that need human attention
- **Internal imports**: None (uses Fabric interface for query)
- **New in fabric**: Extraction of discovery logic

**5. llmpoller.go** (178 lines)
- Exports: LLMPoller, PhaseSpec
- **Purpose**: LLM-based polling to evaluate fabric readiness
  - Builds prompt from phase body + fabric snapshot
  - Invokes agent.Invoker
  - Parses response into PollResult (PROCEED/NEED_INFO/CONFLICT)
  - Fail-open: malformed responses default to PROCEED
- **Key methods**: Poll(), buildPollPrompt(), parseResponse(), helper extractors
- **Internal imports**: agent (agent.Invoker, agent.Agent, agent.RoleArchitect)
- **Relation to ContractPoller**: LLMPoller is more flexible but requires LLM calls; ContractPoller is deterministic

**6. poller.go** (162 lines)
- Exports: PollDecision, PollResult, Poller interface, BlockedPhase, BlockedTracker
- **Purpose**: Core polling abstraction and blocked phase management
  - Poller interface: Poll(ctx, phaseID, snap) → PollResult
  - BlockedPhase: tracks blocked phases with retry count, timestamp, last result
  - BlockedTracker: manages set of blocked phases (Get, Block, Unblock, All, Override, IsOverridden)
  - MaxPollRetries constant (5) for legacy escalation
  - NeedsEscalation(): deprecated, use PushbackHandler instead
- **Internal imports**: None
- **Design note**: This is the abstraction layer; implementations are ContractPoller or LLMPoller

**7. publisher.go** (366 lines)
- Exports: Publisher
- **Purpose**: Post-phase entanglement extraction and publication
  - Runs git diff --name-only to get changed files
  - Parses Go files with go/ast to extract exported symbols
  - Non-Go files produce file-level entanglements only
  - Claims files for the phase automatically
- **Key methods**: PublishPhase(), changedFiles(), extractGoSymbols(), extractFuncDecl(), extractTypeSpecs()
- **Helper functions**: FormatFuncSignature, FormatRecvType, FormatFieldType, ExprString, FormatTypeSignature, packageFromPath
- **Internal imports**: None (uses Fabric interface for writes)
- **Purpose**: Turns phase git diffs into entanglements (symbols published to fabric)

**8. pushback.go** (180 lines)
- Exports: PushbackAction, PushbackHandler
- **Purpose**: Decision logic for handling blocked phases
  - Decides whether to retry, escalate, or proceed (override block)
  - Distinguishes transient blocks (dependency will publish soon) from permanent ones
  - HandleNeedInfo: retries if plausible producer exists (2*MaxRetries cap); escalates at MaxRetries
  - HandleConflict: retries file-claim conflicts (transient); escalates interface conflicts (structural)
  - hasPlausibleProducer: checks if in-progress phase ID appears in missing-info as substring
  - isFileClaimConflict: checks if conflicting phase has file claims (transient)
- **Key methods**: Handle(), handleNeedInfo(), handleConflict()
- **Internal imports**: None
- **New in fabric**: Pushback handling extracted from retry logic

**9. snapshot.go** (306 lines)
- Exports: Snapshot, RenderSnapshot()
- **Purpose**: Snapshots and renders fabric state for LLM prompts
  - Aggregates: entanglements, file claims, phase progress, discoveries, pulses, phase states/cycles
  - RenderSnapshot(): formats into human-readable markdown for injection into prompts
  - Grouped by package with producer info, active file claims with phase state/cycle context
  - Relative timestamps (just now, Xm ago, Xh ago, Xd ago)
- **Helper functions**: renderSummaryLine, renderEntanglements, renderFileClaims, renderDiscoveries, renderPulses, groupEntanglementsByPackage, uniqueProducers, sortedKeys, relativeTime
- **Internal imports**: None
- **Purpose**: Renders fabric state into LLM-friendly markdown

**10. sqlite.go** (576 lines)
- Exports: ErrFileAlreadyClaimed, SQLiteFabric, NewSQLiteFabric()
- **Purpose**: SQLite-backed Fabric implementation
  - WAL mode for sub-millisecond reads with concurrent readers/writers
  - Schema: fabric (phase state), entanglements, file_claims, discoveries, pulses tables
  - MaxOpenConns=1 to avoid SQLITE_BUSY contention (WAL still benefits external readers)
  - PRAGMAs: journal_mode=WAL, busy_timeout=5000ms
- **Implements Fabric interface** (all 18 methods)
- **Internal imports**: None
- **Driver**: modernc.org/sqlite (pure-Go, no CGO)

**11. static.go** (500 lines)
- Exports: PhaseInput, PhaseContract, StaticScanner
- **Purpose**: Pre-execution static analysis of phase specs (no LLM, no execution)
  - Extracts expected entanglements from phase bodies via three strategies:
    1. Scope-based: resolve scope globs, parse matched Go files
    2. Files-section: parse ## Files markdown section
    3. Cross-reference: match producer outputs against consumer body text
  - PhaseContract: produces, consumes, scope, NewProduces map (symbols from files not yet on disk)
- **Key methods**: Scan(), scanPhase(), resolveScope(), parseGoFile(), extractFuncDeclSymbols(), extractTypeSpecSymbols(), parseFilesSection(), extractInlineSymbols(), crossReference(), containsSymbolRef()
- **Internal imports**: None
- **New in fabric**: Static contract extraction (likely new feature post-rename)

---

## Vocabulary Changes (board → fabric)

| Old (board) | New (fabric) | Meaning |
|------------|------------|---------|
| Board | Fabric | Shared state store |
| Contract | Entanglement | Interface dependency |
| PublishContract | PublishEntanglement | Declare symbol availability |
| SQLiteBoard | SQLiteFabric | SQLite implementation |
| StatePolling | StateScanning | Phase state waiting for context |
| BoardSnapshot | Snapshot | Fabric state view |
| WithBoard | WithFabric | Option for worker injection |

---

## Current Usage in Codebase

**Imported by**:
- cmd/fabric.go (CLI commands for fabric operations)
- cmd/discovery.go, cmd/pulse.go (CLI commands)
- cmd/fabric_*.go suite (entanglements, claims, diff, post, read, etc.)
- cmd/nebula_apply.go (phase execution with OnHail callback for discoveries)
- cmd/nebula_plan.go (StaticScanner for contract analysis)
- internal/nebula/worker_fabric.go (core fabric integration with phases)

**Never imported by**: internal/board (dead package)

---

## Design Patterns

### Poller Pattern
```
Poller interface:
  ├─ ContractPoller (deterministic, no LLM)
  └─ LLMPoller (flexible, LLM-based, fail-open)
```

### Discovery Filtering
- PendingHails() filters unresolved discoveries that require human attention
- Budget alerts are informational, not hails

### File Claim Conflict Handling
- Transient (file-claim): retry
- Structural (entanglement): escalate

### Retry Strategy
- DefaultMaxRetries = 3
- If plausible producer exists: 2*MaxRetries cap to allow producer to finish
- Without plausible producer: escalate at MaxRetries

### Static Analysis (no execution)
- PhaseInput → PhaseContract via Scan()
- Three extraction strategies combined
- CrossReference maps producer outputs to consumer body mentions

---

## Relationship Summary

**board** ← **fabric** (1:1 rename)
- fabric is the current, active package
- board is a defunct stub for backward compatibility (git deletion constraints)
- All functionality, tests, and vocabulary migrated from board → fabric
- Fabric adds new capabilities (StaticScanner, contract analysis, discovery management)

**No overlap**: They are sequential, not parallel.
