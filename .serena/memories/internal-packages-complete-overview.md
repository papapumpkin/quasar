# Complete Overview of Internal Packages

This is a comprehensive analysis of all non-test Go files in the smaller internal packages of Quasar.

## Package: internal/agent

**Purpose**: Defines agent types, roles, and the Invoker interface for executing Claude-based coder/reviewer/architect agents.

### Files:
1. **agent.go** (69 lines)
   - Exports: Role (type), RoleCoder/RoleReviewer/RoleArchitect (constants)
   - Types: MCPConfig, Agent, InvocationResult, ReviewReport, Invoker (interface)
   - No internal imports
   - Provides core types for agent roles (coder, reviewer, architect) and invocation results

2. **coder.go** (40 lines)
   - Exports: DefaultCoderSystemPrompt (const string, ~700 lines)
   - No exports of types/functions
   - No internal imports
   - Contains the system prompt template for coder agent

3. **reviewer.go** (40 lines)
   - Exports: DefaultReviewerSystemPrompt (const string, ~700 lines)
   - No exports of types/functions
   - No internal imports
   - Contains the system prompt template for reviewer agent

4. **prompt.go** (60 lines)
   - Exports: FabricProtocol (const), PromptOpts (struct)
   - Functions: BuildSystemPrompt
   - No internal imports
   - Defines coordination protocol for fabric-based multi-quasar execution

5. **prompt_layout.go** (80+ lines)
   - Exports: PromptZone (type), ZoneStablePrefix/ZoneVolatileSuffix (const)
   - Constants: LabelProjectContext, LabelBasePrompt, LabelFabricProtocol, etc. (8 labels)
   - Types: PromptManifest
   - Variable: ContentZone
   - Functions: NewPromptManifest, (PromptZone).String()
   - No internal imports
   - Implements prompt caching zones for Anthropic cache efficiency

**Cross-Package Relationships**: 
- Used by loop.Loop and claude.Invoker for building prompts
- Types consumed by loop and nebula packages

---

## Package: internal/beads

**Purpose**: Wraps the Beads CLI tool for task/issue management, implementing the Client interface.

### Files:
1. **types.go** (44 lines)
   - Exports: Client (interface), Bead (struct), CreateOpts (struct), UpdateOpts (struct)
   - Interface methods: Create, Show, Update, Close, AddComment, Validate
   - No internal imports
   - Defines data types for bead operations

2. **beads.go** (189 lines)
   - Exports: CLI (struct)
   - Methods: (*CLI).QuickCreate, (*CLI).Create, (*CLI).Show, (*CLI).Update, (*CLI).Close, (*CLI).AddComment, (*CLI).Validate
   - Helper functions: buildQuickCreateArgs, buildCreateArgs, buildShowArgs, buildUpdateArgs, buildCloseArgs, buildAddCommentArgs
   - No internal imports
   - CLI implementation that shells out to beads binary

**Cross-Package Relationships**:
- Client interface consumed by loop.Loop and nebula.WorkerGroup
- Used throughout for task creation and updates

---

## Package: internal/claude

**Purpose**: Invokes the Claude CLI as a subprocess and parses JSON output to satisfy agent.Invoker interface.

### Files:
1. **types.go** (variable-length)
   - Exports: CLIResponse (struct)
   - Fields: Type, Subtype, IsError, DurationMs, DurationAPIMs, NumTurns, Result, SessionID, TotalCostUSD
   - No internal imports
   - Defines the parsed response structure from Claude CLI

2. **claude.go** (80+ lines)
   - Exports: Invoker (struct), NewInvoker (function)
   - Methods: (*Invoker).Invoke, (*Invoker).Validate
   - Helper functions: buildEnv, buildArgs, sha256Hex
   - Imports: internal/agent
   - Implements the agent.Invoker interface by shelling to claude CLI

3. **setsid_unix.go** + **setsid_windows.go**
   - Platform-specific session handling

**Cross-Package Relationships**:
- Implements agent.Invoker interface
- Used by loop.Loop to execute agents

---

## Package: internal/config

**Purpose**: Loads runtime configuration from .quasar.yaml, environment variables, and CLI flags.

### Files:
1. **config.go** (74 lines)
   - Exports: Config (struct), DefaultLintCommands (var), Load (function)
   - Config fields: ClaudePath, BeadsPath, WorkDir, MaxReviewCycles, MaxBudgetUSD, Model, CoderSystemPrompt, ReviewerSystemPrompt, Verbose, LintCommands, CacheOptimization, CacheVerbose, ProjectContextPath, MaxContextTokens
   - External: viper (config loading)
   - No internal imports
   - Implements Viper-based configuration with precedence: CLI flags > env vars > .yaml > defaults

**Cross-Package Relationships**:
- Used by cmd layer to initialize loop and nebula executions
- Provides configuration to multiple packages

---

## Package: internal/ansi

**Purpose**: Provides ANSI escape code constants and helpers for terminal output.

### Files:
1. **ansi.go** (34 lines)
   - Exports: Reset, Bold, Dim, Blue, Yellow, Green, Red, Cyan, Magenta (string constants)
   - Exports: ClearLine, CursorUpFmt (string constants)
   - Functions: CursorUp(n int) -> string
   - No internal imports
   - Single source of truth for terminal styling codes

**Cross-Package Relationships**:
- Used extensively by ui.Printer for colored output
- Provides constants aliased in ui package

---

## Package: internal/telemetry

**Purpose**: Records structured JSON event stream for nebula executions, making runs auditable and replayable.

### Files:
1. **telemetry.go** (80+ lines)
   - Exports: Event (struct), Emitter (struct), 21 event kind constants (KindEpochStart, KindAgentDone, etc.)
   - Functions: NewEmitter
   - Methods: (*Emitter).Emit, (*Emitter).Close
   - No internal imports
   - Implements JSONL event streaming with concurrency-safe Emitter

**Cross-Package Relationships**:
- Used by nebula execution to record all state transitions
- Provides audit trail for nebula runs

---

## Package: internal/filter

**Purpose**: Deterministic pre-reviewer checks (build, vet, lint, test, claims) to validate coder output before reviewer invocation.

### Files:
1. **filter.go** (43 lines)
   - Exports: Filter (interface), Result (struct), CheckResult (struct)
   - Filter.Run method
   - Methods: (*Result).FirstFailure()
   - No internal imports
   - Core filter interface and result types

2. **chain.go** (60+ lines)
   - Exports: ClaimChecker (interface), Check (struct), Chain (struct), DefaultChain (function)
   - Methods: (*Chain).Run, (*Chain).RunCheck, (*Chain).RunFrom
   - Helper functions: buildCheck, vetCheck, lintCheck, testCheck, claimsCheck, runCommand
   - No internal imports
   - Chain implementation with sequential check execution

3. **errors.go** (variable-length)
   - Exports: Error (struct), ParseResult (struct)
   - Functions: ParseCheckOutput, parseBuildErrors, parseVetErrors, parseLintErrors, parseTestErrors, parseStandardErrors
   - Helper functions: dedup, cleanPath, atoi
   - Variables: errLineRe, errLineNoColRe, testFailRe, panicTraceRe, linterNameRe (regex patterns)
   - Parses output from build, vet, lint, test commands

**Cross-Package Relationships**:
- Filter interface consumed by loop.Loop
- Chains multiple deterministic checks before reviewer invocation

---

## Package: internal/ui

**Purpose**: Stderr-based UI printer with ANSI colors for user-facing status output.

### Files:
1. **ui.go** (100+ lines)
   - Exports: UI (interface), Printer (struct), BeadChild (struct), HailInfo (struct)
   - Imports: internal/ansi
   - Interface methods: TaskStarted, TaskComplete, CycleStart, AgentStart, AgentDone, CycleSummary, IssuesFound, Approved, MaxCyclesReached, BudgetExceeded, Error, Info, AgentOutput, BeadUpdate, RefactorApplied, FindingLifecycle, HailReceived, HailResolved
   - Methods on Printer: Banner, Prompt, (all UI interface methods)
   - All output to stderr via fmt.Fprintf

2. **plan.go** (variable-length)
   - Exports: planClr (struct)
   - Methods: (*Printer).ExecutionPlanRender, (*Printer).ExecutionPlanDiff, (*Printer).ExecutionPlanSaved
   - Functions: planColors, planRenderContracts, entanglementPkg, planRenderRisks, planRenderStats
   - Renders execution plans for nebula

3. **nebula.go** (variable-length)
   - Methods on Printer: NebulaValidateResult, NebulaPlan, NebulaApplyDone, NebulaWorkerResults, ReviewReport, NebulaShow, NebulaProgressBar, NebulaProgressBarDone, NebulaStatus
   - Functions: NebulaProgressBarLine, nebulaAvgParallelism, formatDuration, pluralS
   - Renders nebula-specific output

4. **dagrender.go** (variable-length)
   - Exports: DAGRenderer (struct), NodeStatus (struct)
   - Methods: (*DAGRenderer).Render, renderFull, buildBox, borderStyle, colorize, layoutRow, drawRow, drawConnectors, renderCompact, compactNode, applyColor, visibleLen
   - Functions: containsStr
   - Renders task DAG as ASCII art

**Cross-Package Relationships**:
- UI interface consumed by loop.Loop
- All human-readable output goes to stderr (stdout reserved for structured data)

---

## Package: internal/dag

**Purpose**: Directed acyclic graph engine for task dependencies with topological sorting, cycle detection, and impact-aware scheduling.

### Files:
1. **dag.go** (80+ lines)
   - Exports: DAG (struct), Node (struct), Wave (struct)
   - Sentinel errors: ErrCycle, ErrNodeNotFound, ErrDuplicateNode, ErrSelfEdge
   - Methods: (*DAG).AddNode, AddEdge, Remove, AddNodeIdempotent, RemoveEdge, Connected, DirectDependents, DepsFor, ComputeWaves, Node, Nodes, Len, TopologicalSort, Ready, Ancestors, Descendants, HasPath
   - Helper types: nodeHeap (for priority-based sorting)
   - No internal imports
   - Core DAG structure and topological operations

2. **analyzer.go** (variable-length)
   - Exports: TaskAnalyzer (struct)
   - Functions: NewTaskAnalyzer
   - Methods: (*TaskAnalyzer).AddTask, AddDependency, RemoveTask, Analyze, ExecutionOrder, ReadyTasks, ReadyWithDone, ImpactScores, Tracks, CriticalPath, DAG, Len, Report
   - No internal imports
   - Analyzes DAG for execution order and impact

3. **strategy.go** (variable-length)
   - Exports: ReportStrategy (interface), ExecutionPlanStrategy, ImpactReportStrategy, TrackAssignmentStrategy, CriticalPathStrategy
   - Methods implementing ReportStrategy.Render
   - Functions: sortedDeps, bottleneckThreshold, computeCriticalPath
   - No internal imports
   - Different rendering strategies for DAG analysis

4. **scoring.go** (variable-length)
   - Exports: ScoringOptions (struct), DefaultScoringOptions (function)
   - Methods: (*DAG).ComputeImpact
   - Constant: ErrAlphaOutOfRange
   - No internal imports
   - Computes impact scores using PageRank and Betweenness

5. **tracks.go** (variable-length)
   - Exports: Track (struct)
   - Methods: (*DAG).ComputeTracks
   - No internal imports
   - Groups related nodes into tracks

6. **betweenness.go** (variable-length)
   - Methods: (*DAG).BetweennessCentrality, brandesBFS, brandesAccumulate
   - Implements Brandes algorithm for betweenness centrality

7. **pagerank.go** (variable-length)
   - Exports: PageRankOptions (struct), DefaultPageRankOptions (function)
   - Methods: (*DAG).PageRank
   - No internal imports
   - Implements PageRank algorithm

8. **unionfind.go** (variable-length)
   - Exports: UnionFind (struct)
   - Functions: NewUnionFind
   - Methods: (*UnionFind).Add, Find, Union, Connected, Components
   - No internal imports
   - Union-Find data structure for component tracking

**Cross-Package Relationships**:
- Used by nebula scheduler (tycho) for task resolution
- Used by fabric for entanglement analysis

---

## Package: internal/snapshot

**Purpose**: Produces deterministic project snapshots for prompt injection with stable project context for prompt caching.

### Files:
1. **scanner.go** (80+ lines)
   - Exports: Scanner (struct), DefaultMaxSize, DefaultMaxDepth (constants)
   - Methods: (*Scanner).Scan, applyDefaults, detectProject, listFiles, gitListFiles, walkFiles, readConventions
   - Functions: truncateUTF8, formatHeader, detectGo, detectNode, detectRust, detectPython, detectSetupPy, parseFileList
   - Variable: conventionsFiles (slice)
   - No internal imports
   - Scans project structure and conventions files

2. **tree.go** (variable-length)
   - Exports: dirNode (struct), collapseThreshold (constant)
   - Functions: buildTree, insertPath, renderTree, renderNode
   - No internal imports
   - Builds and renders directory tree for snapshot

3. **budget.go** (variable-length)
   - Exports: ContextBudget (struct), DefaultMaxContextTokens, charsPerToken, projectFraction, fabricFraction, priorWorkFraction (constants)
   - Functions: EstimateTokens, truncateLayer, truncateFabric, parseFabricSections, truncateSectionBody
   - Methods: (*ContextBudget).Compose
   - No internal imports
   - Manages token budget for context injection

**Cross-Package Relationships**:
- Used by loop to build project context for prompts
- Consumed by checkpoint for stable caching

---

## Package: internal/checkpoint

**Purpose**: Serializes coder-reviewer loop state for persistence and resume across process restarts.

### Files:
1. **checkpoint.go** (100+ lines)
   - Exports: Checkpoint (struct), Finding (struct), Version (constant = 1)
   - Sentinel errors: ErrIncompatibleVersion, ErrGitSHAMismatch, ErrInvalidCheckpoint
   - Functions: FromCycleState, FindingFromReview, findingsFromReview, findingsToReview, Path, Save, Load, LoadAll, extractPhaseID, Validate, Remove, CurrentGitSHA
   - Methods: (*Checkpoint).ToCycleState, (Finding).ToReviewFinding
   - Imports: internal/loop
   - Stores full loop state plus metadata (git SHA, phase ID, nebula name)

2. **hook.go** (variable-length)
   - Exports: Hook (struct)
   - Methods: (*Hook).OnEvent
   - Imports: (implied) internal/loop via event handling
   - Persists state at transition points

**Cross-Package Relationships**:
- Imports internal/loop for CycleState type
- Consumed by nebula for phase resumption
- Enables checkpoint-aware resume logic

---

## Package: internal/neutron

**Purpose**: Archives epoch state and cleans up stale entries in the fabric.

### Files:
1. **neutron.go** (80+ lines)
   - Exports: Neutron (struct), ArchiveOptions (struct)
   - Functions: Archive, Purge, openNeutronDB, ReadNeutron
   - Sentinel errors: ErrActiveClaims, ErrUnresolvedDiscoveries
   - Constant: neutronSchema (DDL for SQLite)
   - Imports: internal/fabric
   - Archives epoch state to standalone SQLite files

2. **reaper.go** (80+ lines)
   - Exports: Reaper (struct), ReapAction (struct)
   - Constants: DefaultStaleClaim, DefaultStaleEpoch
   - Methods: (*Reaper).Run, reapClaims, flagStaleEpochs
   - Imports: internal/fabric
   - Identifies and cleans up stale fabric state

**Cross-Package Relationships**:
- Imports internal/fabric for Fabric interface
- Used by nebula to manage epoch lifecycle

---

## Package: internal/tycho

**Purpose**: DAG-aware task scheduler that resolves dependencies and determines eligible tasks for execution.

### Files:
1. **tycho.go** (100+ lines)
   - Exports: Scheduler (struct), SnapshotBuilder (interface), EligibleResolver (interface), StaleItem (struct)
   - Methods: (*Scheduler).Eligible, AnyInFlight, Scan, flatScan, Reevaluate, BlockedCount, StaleCheck, EscalateAllBlocked, PhaseComplete, HandlePollBlock, HandleEscalation, escalatePhase, logger
   - Functions: toSet
   - Imports: internal/dag, internal/fabric
   - Coordinates task scheduling with DAG resolution

2. **wave_scan.go** (variable-length)
   - Exports: WaveScanner (struct)
   - Methods: (*WaveScanner).ScanWaves, handleBlock, pruneDescendants, logger
   - Imports: internal/dag, internal/fabric
   - Wave-aware scanning with topology-aware pruning

**Cross-Package Relationships**:
- Imports internal/dag for DAG operations
- Imports internal/fabric for Fabric interface
- Used by nebula WorkerGroup for task eligibility

---

## Package: internal/ansi (already covered above)

---

## Cross-Package Import Graph Summary

### Import Directions (arrows point to consumed packages):
- **agent**: standalone, no internal imports
- **beads**: standalone, no internal imports
- **claude**: → agent
- **config**: standalone, no internal imports
- **ansi**: standalone, no internal imports
- **ui**: → ansi
- **telemetry**: standalone, no internal imports
- **filter**: standalone, no internal imports
- **dag**: standalone, no internal imports
- **snapshot**: standalone, no internal imports
- **checkpoint**: → loop
- **neutron**: → fabric
- **tycho**: → dag, fabric

### Key Observations:
1. **Agent-related cluster**: agent, coder, reviewer, prompt, prompt_layout, claude are tightly coupled
2. **Beads cluster**: beads is a standalone wrapper
3. **UI cluster**: ansi → ui (clean dependency)
4. **DAG cluster**: dag is self-contained, consumed by tycho and nebula
5. **State packages**: snapshot, checkpoint, telemetry are specialized storage/serialization
6. **Orchestration**: tycho (scheduler) bridges dag + fabric
7. **Fabric integration**: neutron and tycho both depend on fabric

### Cyclic Dependencies:
- checkpoint imports loop (for CycleState type)
- tycho imports dag + fabric
- No circular imports detected

---

## Size Characteristics
- **Smallest packages**: ansi (~50 lines)
- **Small packages**: agent/beads/claude/config/telemetry (~100-200 lines each)
- **Medium packages**: ui, filter, snapshot (~300-400 lines each)
- **Large packages**: dag (~1000+ lines with scoring, tracks, betweenness, pagerank)
- **Integration packages**: checkpoint, neutron, tycho (~300-500 lines with multiple concerns)
