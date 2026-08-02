package objectives_planner

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	agentnode "github.com/original-david-knight/go_wild/agent_node"
	"github.com/original-david-knight/go_wild/data"
)

const (
	maxLeafRetries       = 3
	baseLeafRetryBackoff = 2 * time.Minute
	maxLeafRetryBackoff  = 30 * time.Minute
)

// workItem represents a unit of work in the scheduler queue.
type workItem struct {
	Objective  *Objective
	Priority   int
	EnqueuedAt time.Time
}

// workQueue is a mutex-protected priority queue of work items.
type workQueue struct {
	mu    sync.Mutex
	items []*workItem
}

// newWorkQueue creates an empty work queue.
func newWorkQueue() *workQueue {
	return &workQueue{}
}

// push adds an item to the queue if not already present. Returns true if added.
func (q *workQueue) push(item *workItem) bool {
	q.mu.Lock()
	defer q.mu.Unlock()

	// Skip if already enqueued
	for _, existing := range q.items {
		if existing.Objective.ID == item.Objective.ID {
			return false
		}
	}

	q.items = append(q.items, item)
	return true
}

// pop removes and returns the highest priority item (lowest number = highest priority).
// Returns nil if the queue is empty.
func (q *workQueue) pop() *workItem {
	q.mu.Lock()
	defer q.mu.Unlock()

	if len(q.items) == 0 {
		return nil
	}

	bestIdx := 0
	for i, item := range q.items {
		if item.Priority < q.items[bestIdx].Priority {
			bestIdx = i
		} else if item.Priority == q.items[bestIdx].Priority && item.EnqueuedAt.Before(q.items[bestIdx].EnqueuedAt) {
			bestIdx = i
		}
	}

	item := q.items[bestIdx]
	q.items = append(q.items[:bestIdx], q.items[bestIdx+1:]...)
	return item
}

// count returns the number of items in the queue.
func (q *workQueue) count() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}

// remove removes all items with the given objective ID.
func (q *workQueue) remove(objectiveID string) {
	q.mu.Lock()
	defer q.mu.Unlock()

	filtered := q.items[:0]
	for _, item := range q.items {
		if item.Objective.ID != objectiveID {
			filtered = append(filtered, item)
		}
	}
	q.items = filtered
}

// Scheduler is the long-running daemon that evaluates, plans, and executes objectives.
type Scheduler struct {
	store    *ObjectiveStore
	activity *ActivityStore
	planner  *StrategicPlanner
	engine   *ExecutionEngine
	cfg      Config

	queue    *workQueue
	inFlight sync.Map // objectiveID → true; prevents re-enqueue while executing
	stop     chan struct{}
	done     chan struct{}
}

// NewScheduler creates a scheduler with the given dependencies.
func NewScheduler(store *ObjectiveStore, activity *ActivityStore, planner *StrategicPlanner, engine *ExecutionEngine, cfg Config) *Scheduler {
	return &Scheduler{
		store:    store,
		activity: activity,
		planner:  planner,
		engine:   engine,
		cfg:      cfg,
		queue:    newWorkQueue(),
		stop:     make(chan struct{}),
		done:     make(chan struct{}),
	}
}

// Run starts the scheduler goroutines and blocks until ctx is cancelled or Stop is called.
func (s *Scheduler) Run(ctx context.Context) error {
	log.Println("[scheduler] starting")

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(3)
	go func() {
		defer wg.Done()
		s.cronTicker(ctx)
	}()
	go func() {
		defer wg.Done()
		s.continuousEvaluator(ctx)
	}()
	go func() {
		defer wg.Done()
		s.workExecutor(ctx)
	}()

	// Wait for stop signal or context cancellation
	select {
	case <-ctx.Done():
	case <-s.stop:
		cancel()
	}

	log.Println("[scheduler] shutting down, waiting for goroutines...")
	wg.Wait()
	close(s.done)
	log.Println("[scheduler] stopped")
	return nil
}

// Stop signals the scheduler to shut down gracefully.
func (s *Scheduler) Stop() {
	select {
	case <-s.stop:
		// Already stopped
	default:
		close(s.stop)
	}
}

// cronTicker scans for pending and cron-scheduled objectives every 10 seconds.
func (s *Scheduler) cronTicker(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Run once immediately on start
	s.scanPendingObjectives(ctx)
	s.scanCronObjectives(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.scanPendingObjectives(ctx)
			s.scanCronObjectives(ctx)
		}
	}
}

// scanPendingObjectives enqueues any pending or active root objectives for planning and execution.
func (s *Scheduler) scanPendingObjectives(ctx context.Context) {
	now := time.Now().UTC()

	for _, status := range []ObjectiveStatus{StatusPending, StatusActive} {
		objs, err := s.store.GetByStatus(ctx, status)
		if err != nil {
			log.Printf("[scheduler] %s scan error: %v", status, err)
			continue
		}
		for _, obj := range objs {
			if !obj.CooldownUntil.IsZero() && now.Before(obj.CooldownUntil) {
				continue
			}
			// Only enqueue root objectives — children are handled by their parent's execution
			if obj.ParentID != "" {
				continue
			}
			// Skip if already being executed
			if _, running := s.inFlight.Load(obj.ID); running {
				continue
			}
			if s.queue.push(&workItem{
				Objective:  obj,
				Priority:   obj.Priority,
				EnqueuedAt: now,
			}) {
				log.Printf("[scheduler] enqueuing %s objective: %s", status, obj.Title)
			}
		}
	}
}

func (s *Scheduler) scanCronObjectives(ctx context.Context) {
	objs, err := s.store.GetByScheduleType(ctx, ScheduleCron)
	if err != nil {
		log.Printf("[scheduler] cron scan error: %v", err)
		return
	}

	now := time.Now().UTC()
	for _, obj := range objs {
		if obj.Status == StatusCompleted || obj.Status == StatusFailed {
			continue
		}
		if obj.ScheduleCron == "" {
			continue
		}
		if !obj.CooldownUntil.IsZero() && now.Before(obj.CooldownUntil) {
			continue
		}
		if matchesCron(obj.ScheduleCron, now) {
			if _, running := s.inFlight.Load(obj.ID); running {
				continue
			}
			if s.queue.push(&workItem{
				Objective:  obj,
				Priority:   obj.Priority,
				EnqueuedAt: now,
			}) {
				log.Printf("[scheduler] cron match: %s (%s)", obj.Title, obj.ScheduleCron)
			}
		}
	}
}

// continuousEvaluator scans active objectives every 5 minutes.
func (s *Scheduler) continuousEvaluator(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.evaluateActiveObjectives(ctx)
		}
	}
}

func (s *Scheduler) evaluateActiveObjectives(ctx context.Context) {
	actives, err := s.store.GetByStatus(ctx, StatusActive)
	if err != nil {
		log.Printf("[scheduler] evaluator scan error: %v", err)
		return
	}

	now := time.Now().UTC()
	for _, obj := range actives {
		if !obj.CooldownUntil.IsZero() && now.Before(obj.CooldownUntil) {
			continue
		}
		// Also check continuous-schedule objectives
		if obj.ScheduleType == ScheduleContinuous {
			if _, running := s.inFlight.Load(obj.ID); running {
				continue
			}
			if s.queue.push(&workItem{
				Objective:  obj,
				Priority:   obj.Priority,
				EnqueuedAt: now,
			}) {
				log.Printf("[scheduler] continuous re-evaluation: %s", obj.Title)
			}
		}
	}
}

// workExecutor pulls from the queue and executes work items with bounded concurrency.
func (s *Scheduler) workExecutor(ctx context.Context) {
	maxConc := s.cfg.MaxConcurrency
	if maxConc <= 0 {
		maxConc = 4
	}
	sem := make(chan struct{}, maxConc)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				item := s.queue.pop()
				if item == nil {
					break
				}

				// Acquire semaphore slot
				select {
				case sem <- struct{}{}:
				case <-ctx.Done():
					return
				}

				s.inFlight.Store(item.Objective.ID, true)
				wg.Add(1)
				go func(item *workItem) {
					defer wg.Done()
					defer func() { <-sem }()
					defer s.inFlight.Delete(item.Objective.ID)
					s.executeWorkItem(ctx, item)
				}(item)
			}
		}
	}
}

func (s *Scheduler) executeWorkItem(ctx context.Context, item *workItem) {
	obj := item.Objective
	now := time.Now().UTC()

	// Re-check cooldown
	if !obj.CooldownUntil.IsZero() && now.Before(obj.CooldownUntil) {
		log.Printf("[scheduler] skipping %s (cooldown until %s)", obj.Title, obj.CooldownUntil)
		return
	}

	// Re-read from DB to get latest status (may have changed since enqueue)
	fresh, err := s.store.Get(ctx, obj.ID)
	if err != nil {
		log.Printf("[scheduler] re-read %s failed: %v", obj.Title, err)
		return
	}
	obj = fresh

	// Skip objectives that shouldn't be executed
	if obj.Status == StatusBlocked || obj.Status == StatusPaused || obj.Status == StatusCompleted {
		log.Printf("[scheduler] skipping %s objective: %s", obj.Status, obj.Title)
		return
	}

	log.Printf("[scheduler] executing: %s", obj.Title)
	obj.Status = StatusActive
	s.store.Update(ctx, obj)

	s.activity.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: obj.ID,
		EventType:   "scheduler_execute",
		Severity:    SeverityInfo,
		Summary:     fmt.Sprintf("Scheduler executing: %s", obj.Title),
	})

	// Step 1: Strategic decomposition if no children yet
	children, err := s.store.GetChildren(ctx, obj.ID)
	if err != nil {
		log.Printf("[scheduler] get children for %s: %v", obj.Title, err)
		return
	}

	if len(children) == 0 {
		if s.planner == nil {
			log.Printf("[scheduler] planner unavailable for strategic planning: %s", obj.Title)
			s.setCooldown(ctx, obj, 1*time.Minute)
			return
		}

		// Check for pending escalations — don't re-plan while waiting for answers
		if s.hasPendingEscalations(ctx, obj.ID) {
			log.Printf("[scheduler] waiting for escalation answers: %s", obj.Title)
			return
		}

		log.Printf("[scheduler] strategic planning for: %s", obj.Title)
		tree, _ := s.store.GetTree(ctx, obj.ID)
		toolCatalog := s.getToolCatalogForObjective(obj)
		memory := s.buildEscalationMemory(ctx, obj.ID)

		planOutput, _, err := s.planner.PlanStrategic(ctx, obj.Title+". "+obj.Description, tree, toolCatalog, memory)
		if err != nil {
			log.Printf("[scheduler] strategic planning failed for %s: %v", obj.Title, err)
			s.activity.LogTaskFailed(ctx, obj.ID, fmt.Sprintf("Strategic planning failed: %v", err), nil)
			s.setCooldown(ctx, obj, 5*time.Minute)
			return
		}

		// If planner needs clarification, create escalations and pause
		if len(planOutput.ClarifyingQuestions) > 0 {
			log.Printf("[scheduler] planner needs %d clarifications for: %s", len(planOutput.ClarifyingQuestions), obj.Title)
			s.createEscalations(ctx, obj.ID, planOutput.ClarifyingQuestions)
			obj.Status = StatusBlocked
			obj.LastResult = fmt.Sprintf("Waiting for %d clarifications", len(planOutput.ClarifyingQuestions))
			s.store.Update(ctx, obj)
			s.activity.LogEvent(ctx, &ActivityEvent{
				ObjectiveID: obj.ID,
				EventType:   "clarification_needed",
				Severity:    SeverityWarning,
				Summary:     fmt.Sprintf("Planner needs %d clarifications before proceeding", len(planOutput.ClarifyingQuestions)),
				Details:     map[string]any{"reasoning": planOutput.Reasoning},
			})
			return
		}

		if len(planOutput.Mutations) > 0 {
			if err := s.store.ApplyMutations(ctx, planOutput.Mutations); err != nil {
				log.Printf("[scheduler] apply strategic mutations for %s: %v", obj.Title, err)
			}
			s.activity.LogPlanCreated(ctx, obj.ID, fmt.Sprintf("Strategic plan: %s (%d mutations)", obj.Title, len(planOutput.Mutations)), map[string]any{
				"reasoning":      planOutput.Reasoning,
				"mutation_count": len(planOutput.Mutations),
				"memory":         memory,
			})
		}

		// Re-read children after mutations
		children, err = s.store.GetChildren(ctx, obj.ID)
		if err != nil {
			log.Printf("[scheduler] get children after strategic plan for %s: %v", obj.Title, err)
			return
		}
		log.Printf("[scheduler] strategic plan created %d children for: %s", len(children), obj.Title)
	}

	// Step 2: Find leaf objectives and run tactical planning + execution on each.
	// GetLeafTasks returns leaves that are pending or active.
	leaves, err := s.store.GetLeafTasks(ctx, obj.ID)
	if err != nil {
		log.Printf("[scheduler] get leaves for %s: %v", obj.Title, err)
		return
	}

	// If no actionable leaves, check why
	if len(leaves) == 0 {
		allLeaves := s.getAllLeaves(ctx, obj.ID)
		var failedCount int
		for _, leaf := range allLeaves {
			if leaf.Status == StatusFailed {
				failedCount++
			}
		}

		if failedCount > 0 {
			s.blockMissionForFailedLeaves(ctx, obj, allLeaves)
			return
		}

		// Propagate completion up the tree
		s.propagateCompletion(ctx, obj.ID)

		// Review: is the mission truly done, or is there more work?
		s.reviewAndContinue(ctx, obj)
		return
	}

	// Filter to incomplete leaves
	var pending []*Objective
	for _, leaf := range leaves {
		if leaf.Status != StatusCompleted {
			pending = append(pending, leaf)
		}
	}

	if len(pending) == 0 {
		s.propagateCompletion(ctx, obj.ID)
		s.reviewAndContinue(ctx, obj)
		return
	}

	log.Printf("[scheduler] %d pending leaf tasks for: %s", len(pending), obj.Title)
	if s.planner == nil || s.engine == nil {
		log.Printf("[scheduler] planner/engine unavailable for leaf execution: %s", obj.Title)
		s.setCooldown(ctx, obj, 1*time.Minute)
		return
	}

	// Execute leaves with bounded concurrency and per-leaf timeout
	maxLeafConc := s.cfg.MaxConcurrency
	if maxLeafConc <= 0 {
		maxLeafConc = 4
	}
	leafTimeout := 5 * time.Minute

	type leafResult struct {
		leaf       *Objective
		sufficient bool
	}
	results := make(chan leafResult, len(pending))
	leafSem := make(chan struct{}, maxLeafConc)
	var wg sync.WaitGroup

	for _, leaf := range pending {
		wg.Add(1)
		go func(leaf *Objective) {
			defer wg.Done()
			leafSem <- struct{}{}
			defer func() { <-leafSem }()
			leafCtx, cancel := context.WithTimeout(ctx, leafTimeout)
			defer cancel()
			sufficient := s.executeLeaf(leafCtx, obj.ID, leaf)
			results <- leafResult{leaf: leaf, sufficient: sufficient}
		}(leaf)
	}

	wg.Wait()
	close(results)

	allSufficient := true
	for r := range results {
		if !r.sufficient {
			allSufficient = false
		}
	}

	// After executing all leaves, propagate completion and review
	s.propagateCompletion(ctx, obj.ID)
	if allSufficient {
		s.reviewAndContinue(ctx, obj)
	} else {
		obj.LastResult = fmt.Sprintf("Executed %d leaves, continuing", len(pending))
		s.store.Update(ctx, obj)
	}
}

// executeLeaf plans and executes a single leaf objective. Returns true if sufficient.
func (s *Scheduler) executeLeaf(ctx context.Context, rootID string, leaf *Objective) bool {
	leafChildren, _ := s.store.GetChildren(ctx, leaf.ID)
	toolCatalog, validToolNames := s.getToolInfoForObjective(leaf)

	// Build prior context from previous activity on this leaf
	priorContext := s.buildPriorContext(ctx, leaf.ID)

	log.Printf("[scheduler] tactical planning for leaf: %s", leaf.Title)
	planOutput, graph, err := s.planner.PlanTactical(ctx, leaf, leafChildren, toolCatalog, priorContext, validToolNames)
	if err != nil {
		log.Printf("[scheduler] tactical planning failed for %s: %v", leaf.Title, err)
		s.scheduleLeafRetry(ctx, leaf, fmt.Sprintf("Planning failed: %v", err))
		return false
	}

	// Filter out 'add' mutations if leaf is at max depth
	if leaf.Depth >= MaxTreeDepth {
		var filtered []TreeMutation
		for _, m := range planOutput.Mutations {
			if m.Action == MutationAdd {
				log.Printf("[scheduler] dropping add mutation at max depth for: %s", leaf.Title)
				continue
			}
			filtered = append(filtered, m)
		}
		planOutput.Mutations = filtered
	}

	if len(planOutput.Mutations) > 0 {
		if err := s.store.ApplyMutations(ctx, planOutput.Mutations); err != nil {
			log.Printf("[scheduler] apply tactical mutations for %s: %v", leaf.Title, err)
		}
	}

	if graph == nil || len(graph.Nodes) == 0 {
		freshChildren, _ := s.store.GetChildren(ctx, leaf.ID)
		if len(freshChildren) > 0 {
			leaf.Status = StatusActive
			leaf.LastResult = fmt.Sprintf("Decomposed into %d child objectives", len(freshChildren))
			s.store.Update(ctx, leaf)
			log.Printf("[scheduler] leaf %s decomposed into %d children", leaf.Title, len(freshChildren))
			return false
		}
		log.Printf("[scheduler] no execution graph for leaf: %s (no new children)", leaf.Title)
		s.scheduleLeafRetry(ctx, leaf, "Planner returned no execution graph")
		return false
	}

	s.LogGraph(ctx, rootID, leaf.Title, planOutput.Reasoning, graph)

	leaf.Status = StatusActive
	s.store.Update(ctx, leaf)

	results, err := s.engine.Execute(ctx, leaf, graph)
	if err != nil {
		log.Printf("[scheduler] execution failed for %s: %v", leaf.Title, err)
		s.scheduleLeafRetry(ctx, leaf, fmt.Sprintf("Execution failed: %v", err))
		return false
	}

	nodeCount := len(results.NodeResults)
	if results.Sufficient {
		leaf.Status = StatusCompleted
		leaf.CompletedAt = time.Now().UTC()
		leaf.CooldownUntil = time.Time{}
		deleteMetadataKey(leaf, "retry_count")
	} else {
		s.scheduleLeafRetry(ctx, leaf, fmt.Sprintf("Execution insufficient after %d nodes", nodeCount))
		return false
	}
	leaf.LastResult = fmt.Sprintf("%d nodes completed, sufficient=%v", nodeCount, results.Sufficient)
	s.store.Update(ctx, leaf)

	s.activity.LogTaskCompleted(ctx, leaf.ID, fmt.Sprintf("Executed %d nodes for: %s", nodeCount, leaf.Title), map[string]any{
		"result_count": nodeCount,
		"sufficient":   results.Sufficient,
	})

	return results.Sufficient
}

func (s *Scheduler) scheduleLeafRetry(ctx context.Context, leaf *Objective, reason string) {
	retryCount := getMetadataInt(leaf, "retry_count") + 1
	setMetadataInt(leaf, "retry_count", retryCount)

	if retryCount >= maxLeafRetries {
		leaf.Status = StatusFailed
		leaf.CooldownUntil = time.Time{}
		leaf.LastResult = fmt.Sprintf("%s (retries exhausted: %d/%d)", reason, retryCount, maxLeafRetries)
		s.store.Update(ctx, leaf)
		s.activity.LogTaskFailed(ctx, leaf.ID, leaf.LastResult, map[string]any{
			"retry_count": retryCount,
		})
		return
	}

	backoff := retryBackoff(retryCount)
	leaf.Status = StatusPending
	leaf.CooldownUntil = time.Now().UTC().Add(backoff)
	leaf.LastResult = fmt.Sprintf("%s (retry %d/%d in %s)", reason, retryCount, maxLeafRetries, backoff)
	s.store.Update(ctx, leaf)

	s.activity.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: leaf.ID,
		EventType:   "task_retry_scheduled",
		Severity:    SeverityWarning,
		Summary:     leaf.LastResult,
		Details: map[string]any{
			"retry_count": retryCount,
			"backoff_sec": int(backoff.Seconds()),
			"reason":      reason,
		},
	})
}

func retryBackoff(retryCount int) time.Duration {
	if retryCount <= 1 {
		return baseLeafRetryBackoff
	}
	backoff := baseLeafRetryBackoff << (retryCount - 1)
	if backoff > maxLeafRetryBackoff {
		return maxLeafRetryBackoff
	}
	return backoff
}

func (s *Scheduler) blockMissionForFailedLeaves(ctx context.Context, root *Objective, allLeaves []*Objective) {
	var failedTitles []string
	for _, leaf := range allLeaves {
		if leaf.Status == StatusFailed {
			failedTitles = append(failedTitles, leaf.Title)
		}
	}
	if len(failedTitles) == 0 {
		return
	}

	if !s.hasPendingEscalations(ctx, root.ID) {
		s.createEscalations(ctx, root.ID, []ClarifyingQuestion{{
			Question: "One or more tasks repeatedly failed. Please provide guidance on how to proceed.",
			Context:  fmt.Sprintf("Failed tasks: %s", strings.Join(failedTitles, ", ")),
		}})
	}

	root.Status = StatusBlocked
	root.LastResult = fmt.Sprintf("%d leaf tasks failed and need guidance", len(failedTitles))
	s.store.Update(ctx, root)

	s.activity.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: root.ID,
		EventType:   "mission_blocked",
		Severity:    SeverityWarning,
		Summary:     root.LastResult,
		Details: map[string]any{
			"failed_count":  len(failedTitles),
			"failed_titles": failedTitles,
		},
	})

	log.Printf("[scheduler] mission blocked after failed leaves: %s (%d failed)", root.Title, len(failedTitles))
}

func getMetadataInt(obj *Objective, key string) int {
	if obj == nil || obj.Metadata == nil {
		return 0
	}
	raw, ok := obj.Metadata[key]
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float32:
		return int(v)
	case float64:
		return int(v)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err == nil {
			return n
		}
	}
	return 0
}

func setMetadataInt(obj *Objective, key string, value int) {
	if obj.Metadata == nil {
		obj.Metadata = map[string]any{}
	}
	obj.Metadata[key] = value
}

func deleteMetadataKey(obj *Objective, key string) {
	if obj == nil || obj.Metadata == nil {
		return
	}
	delete(obj.Metadata, key)
}

// buildPriorContext gathers previous execution results for a leaf to give the planner
// awareness of what was already tried and what the outcomes were.
// Also includes user directives from the root mission.
func (s *Scheduler) buildPriorContext(ctx context.Context, objectiveID string) string {
	var sb strings.Builder

	// Include user directives from the root mission (walk up the tree)
	rootID := s.findRootID(ctx, objectiveID)
	if rootID != "" {
		directives := s.getUserDirectives(ctx, rootID)
		if len(directives) > 0 {
			sb.WriteString("## Human Directives (follow these closely)\n")
			for _, d := range directives {
				sb.WriteString(fmt.Sprintf("- [%s] %s\n", d.CreatedAt.Format("2006-01-02 15:04"), d.Summary))
			}
			sb.WriteString("\n")
		}
	}

	events, err := s.activity.GetEvents(ctx, objectiveID, 20)
	if err != nil || len(events) == 0 {
		return sb.String()
	}

	sb.WriteString("Previous execution history for this objective (most recent first):\n")
	hasRelevant := false
	for _, ev := range events {
		switch ev.EventType {
		case "node_completed":
			text, _ := ev.Details["text"].(string)
			if text != "" {
				sb.WriteString(fmt.Sprintf("- [%s] Node completed: %s\n", ev.CreatedAt.Format("15:04"), truncateStr(text, 300)))
				hasRelevant = true
			}
		case "task_completed":
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", ev.CreatedAt.Format("15:04"), ev.Summary))
			hasRelevant = true
		case "task_failed":
			sb.WriteString(fmt.Sprintf("- [%s] FAILED: %s\n", ev.CreatedAt.Format("15:04"), ev.Summary))
			hasRelevant = true
		case "evaluation_completed":
			sufficient, _ := ev.Details["sufficient"].(bool)
			reasoning, _ := ev.Details["reasoning"].(string)
			sb.WriteString(fmt.Sprintf("- [%s] Evaluation: sufficient=%v", ev.CreatedAt.Format("15:04"), sufficient))
			if reasoning != "" {
				sb.WriteString(fmt.Sprintf(" — %s", truncateStr(reasoning, 200)))
			}
			sb.WriteString("\n")
			hasRelevant = true
		}
	}

	if !hasRelevant {
		// Trim the "Previous execution history" header if nothing was added
		result := sb.String()
		return strings.TrimSuffix(result, "Previous execution history for this objective (most recent first):\n")
	}
	return sb.String()
}

// findRootID walks up the parent chain to find the root objective ID.
func (s *Scheduler) findRootID(ctx context.Context, objectiveID string) string {
	current := objectiveID
	for i := 0; i < 10; i++ { // safety limit
		obj, err := s.store.Get(ctx, current)
		if err != nil {
			return current
		}
		if obj.ParentID == "" {
			return obj.ID
		}
		current = obj.ParentID
	}
	return current
}

// hasPendingEscalations checks if an objective has unresolved escalations.
func (s *Scheduler) hasPendingEscalations(ctx context.Context, objectiveID string) bool {
	results, err := s.store.DB().Table(Escalation{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"objective_id": objectiveID, "status": string(EscalationPending)},
		Limit: 1,
	})
	return err == nil && len(results) > 0
}

// createEscalations persists clarifying questions as escalations.
// Deduplicates: skips questions that already exist (pending or resolved) for this objective.
func (s *Scheduler) createEscalations(ctx context.Context, objectiveID string, questions []ClarifyingQuestion) {
	// Gather existing escalation questions for this objective (all statuses)
	existing, _ := s.store.DB().Table(Escalation{}).Query(ctx, gowild_data.QueryOpts{
		Where: map[string]any{"objective_id": objectiveID},
	})
	existingQuestions := make(map[string]bool)
	for _, r := range existing {
		if esc, ok := r.(*Escalation); ok {
			existingQuestions[esc.Question] = true
		}
	}

	created := 0
	for _, q := range questions {
		if existingQuestions[q.Question] {
			log.Printf("[scheduler] skipping duplicate escalation: %s", truncateStr(q.Question, 80))
			continue
		}
		esc := &Escalation{
			ID:          fmt.Sprintf("esc-%d", time.Now().UnixNano()),
			ObjectiveID: objectiveID,
			Question:    q.Question,
			Context:     q.Context,
			Severity:    SeverityWarning,
			Status:      EscalationPending,
			CreatedAt:   time.Now().UTC(),
		}
		if err := s.store.DB().Table(Escalation{}).Insert(ctx, esc); err != nil {
			log.Printf("[scheduler] create escalation error: %v", err)
		}
		existingQuestions[q.Question] = true
		created++
	}
	log.Printf("[scheduler] created %d/%d escalations (deduped) for %s", created, len(questions), objectiveID)
}

// buildEscalationMemory formats resolved escalation answers and user directives as context for re-planning.
func (s *Scheduler) buildEscalationMemory(ctx context.Context, objectiveID string) string {
	var sb strings.Builder

	// Include user directives (highest priority — these are direct human guidance)
	directives := s.getUserDirectives(ctx, objectiveID)
	if len(directives) > 0 {
		sb.WriteString("## Human Directives (follow these closely)\n")
		for _, d := range directives {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", d.CreatedAt.Format("2006-01-02 15:04"), d.Summary))
		}
		sb.WriteString("\n")
	}

	// Include resolved escalation answers
	results, err := s.store.DB().Table(Escalation{}).Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"objective_id": objectiveID, "status": string(EscalationResolved)},
		OrderBy: "resolved_at",
	})
	if err == nil && len(results) > 0 {
		sb.WriteString("The human provided the following answers to clarifying questions:\n")
		for _, r := range results {
			if esc, ok := r.(*Escalation); ok {
				sb.WriteString(fmt.Sprintf("Q: %s\nA: %s\n\n", esc.Question, esc.Resolution))
			}
		}
	}

	return sb.String()
}

// getUserDirectives returns user_directive events for the given objective, ordered by creation time.
func (s *Scheduler) getUserDirectives(ctx context.Context, objectiveID string) []*ActivityEvent {
	results, err := s.activity.DB().Table(ActivityEvent{}).Query(ctx, gowild_data.QueryOpts{
		Where:   map[string]any{"objective_id": objectiveID, "event_type": "user_directive"},
		OrderBy: "created_at",
	})
	if err != nil {
		return nil
	}
	var directives []*ActivityEvent
	for _, r := range results {
		if ev, ok := r.(*ActivityEvent); ok {
			directives = append(directives, ev)
		}
	}
	return directives
}

func (s *Scheduler) LogGraph(ctx context.Context, objectiveID, title, reasoning string, graph *agentnode.NodeGraph) {
	nodes := make([]map[string]any, 0, len(graph.Nodes))
	for _, n := range graph.Nodes {
		node := map[string]any{
			"id":     string(n.ID),
			"prompt": n.Prompt,
			"type":   string(n.Type),
		}
		if len(n.DependsOn) > 0 {
			deps := make([]string, len(n.DependsOn))
			for i, d := range n.DependsOn {
				deps[i] = string(d)
			}
			node["depends_on"] = deps
		}
		if len(n.ToolNames) > 0 {
			node["tools"] = n.ToolNames
		}
		nodes = append(nodes, node)
	}
	s.activity.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: objectiveID,
		EventType:   "graph_planned",
		Severity:    SeverityInfo,
		Summary:     fmt.Sprintf("Planned %d node graph for: %s", len(graph.Nodes), title),
		Details: map[string]any{
			"reasoning": reasoning,
			"nodes":     nodes,
		},
	})
}

// getToolCatalogForObjective returns a tool catalog that includes company-scoped tools
// for the given objective. This ensures the planner knows about all available tools.
func (s *Scheduler) getToolCatalogForObjective(obj *Objective) string {
	if s.engine == nil {
		return ""
	}
	return agentnode.ToolCatalog(s.engine.resolveTools(obj))
}

// getToolInfoForObjective returns the tool catalog string and a set of valid tool names.
func (s *Scheduler) getToolInfoForObjective(obj *Objective) (string, map[string]bool) {
	if s.engine == nil {
		return "", map[string]bool{}
	}
	registry := s.engine.resolveTools(obj)
	names := make(map[string]bool, len(registry))
	for name := range registry {
		names[name] = true
	}
	return agentnode.ToolCatalog(registry), names
}

func (s *Scheduler) setCooldown(ctx context.Context, obj *Objective, d time.Duration) {
	obj.CooldownUntil = time.Now().UTC().Add(d)
	s.store.Update(ctx, obj)
}

// reviewAndContinue runs after all leaves are done. It asks the LLM whether the
// mission is truly complete. If more work is needed, the review produces new
// objectives and the scheduler continues on the next tick.
func (s *Scheduler) reviewAndContinue(ctx context.Context, root *Objective) {
	tree, err := s.store.GetTree(ctx, root.ID)
	if err != nil {
		log.Printf("[scheduler] review: get tree failed: %v", err)
		return
	}

	toolCatalog := s.getToolCatalogForObjective(root)
	memory := s.buildEscalationMemory(ctx, root.ID)

	log.Printf("[scheduler] reviewing mission: %s", root.Title)
	s.activity.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: root.ID,
		EventType:   "mission_review",
		Severity:    SeverityInfo,
		Summary:     "Reviewing mission progress — checking what remains",
	})

	review, err := s.planner.reviewMission(ctx, root.Title+". "+root.Description, tree, toolCatalog, memory)
	if err != nil {
		log.Printf("[scheduler] review failed for %s: %v", root.Title, err)
		s.setCooldown(ctx, root, 5*time.Minute)
		return
	}

	if len(review.Mutations) == 0 {
		// Mission is truly complete
		log.Printf("[scheduler] mission review: %s is COMPLETE", root.Title)
		root.Status = StatusCompleted
		root.CompletedAt = time.Now().UTC()
		root.LastResult = "Mission complete: " + truncateStr(review.Reasoning, 200)
		s.store.Update(ctx, root)
		s.activity.LogEvent(ctx, &ActivityEvent{
			ObjectiveID: root.ID,
			EventType:   "mission_complete",
			Severity:    SeverityInfo,
			Summary:     "Mission complete: " + truncateStr(review.Reasoning, 200),
		})
		return
	}

	// More work needed — apply new objectives.
	// Fix up parent references: the LLM may use the root's title as parent_id
	// instead of its UUID. Replace title references with the root's actual ID.
	for i := range review.Mutations {
		if review.Mutations[i].Action == MutationAdd && review.Mutations[i].ParentID == root.Title {
			review.Mutations[i].ParentID = root.ID
		}
	}
	log.Printf("[scheduler] mission review: %s needs %d more objectives", root.Title, len(review.Mutations))
	if err := s.store.ApplyMutations(ctx, review.Mutations); err != nil {
		log.Printf("[scheduler] review mutations failed for %s: %v", root.Title, err)
	}

	s.activity.LogEvent(ctx, &ActivityEvent{
		ObjectiveID: root.ID,
		EventType:   "mission_continued",
		Severity:    SeverityInfo,
		Summary:     fmt.Sprintf("Review: %d new objectives added — %s", len(review.Mutations), truncateStr(review.Reasoning, 200)),
		Details:     map[string]any{"reasoning": review.Reasoning, "mutation_count": len(review.Mutations)},
	})

	// Keep root active for the next round
	root.Status = StatusActive
	root.LastResult = fmt.Sprintf("Round complete, %d new objectives added", len(review.Mutations))
	s.store.Update(ctx, root)
}

// propagateCompletion walks the tree bottom-up, marking objectives as completed
// when all their children are completed. This ensures status bubbles up from
// leaves → parents → root rather than jumping directly.
func (s *Scheduler) propagateCompletion(ctx context.Context, rootID string) {
	tree, err := s.store.GetTree(ctx, rootID)
	if err != nil {
		return
	}

	// Build parent→children map
	childrenOf := make(map[string][]*Objective)
	for _, obj := range tree {
		if obj.ParentID != "" {
			childrenOf[obj.ParentID] = append(childrenOf[obj.ParentID], obj)
		}
	}

	// Process bottom-up: for each non-leaf non-root, check if all children are completed.
	// Skip the root — its completion is decided by reviewAndContinue.
	// Repeat until no more changes (handles multi-level propagation).
	changed := true
	for changed {
		changed = false
		for _, obj := range tree {
			if obj.Status == StatusCompleted || obj.ID == rootID {
				continue
			}
			children := childrenOf[obj.ID]
			if len(children) == 0 {
				continue // leaf — skip
			}
			allDone := true
			for _, child := range children {
				if child.Status != StatusCompleted {
					allDone = false
					break
				}
			}
			if allDone {
				obj.Status = StatusCompleted
				obj.CompletedAt = time.Now().UTC()
				obj.LastResult = fmt.Sprintf("All %d children completed", len(children))
				s.store.Update(ctx, obj)
				log.Printf("[scheduler] propagated completion: %s", obj.Title)
				changed = true
			}
		}
	}
}

// getAllLeaves returns all leaf objectives (no children) regardless of status.
func (s *Scheduler) getAllLeaves(ctx context.Context, rootID string) []*Objective {
	tree, err := s.store.GetTree(ctx, rootID)
	if err != nil {
		return nil
	}
	parents := make(map[string]bool)
	for _, obj := range tree {
		if obj.ParentID != "" {
			parents[obj.ParentID] = true
		}
	}
	var leaves []*Objective
	for _, obj := range tree {
		if !parents[obj.ID] && obj.ID != rootID {
			leaves = append(leaves, obj)
		}
	}
	return leaves
}

// matchesCron checks if a 5-field cron expression (minute hour dom month dow)
// matches the given time. Supports * (any) and specific numbers.
func matchesCron(expr string, t time.Time) bool {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false
	}

	minute := t.Minute()
	hour := t.Hour()
	dom := t.Day()
	month := int(t.Month())
	dow := int(t.Weekday()) // 0=Sunday

	return matchesCronField(fields[0], minute) &&
		matchesCronField(fields[1], hour) &&
		matchesCronField(fields[2], dom) &&
		matchesCronField(fields[3], month) &&
		matchesCronField(fields[4], dow)
}

// matchesCronField checks if a single cron field matches a value.
// Supports: * (any), single number, comma-separated values, and step values (*/N).
func matchesCronField(field string, value int) bool {
	if field == "*" {
		return true
	}

	// Handle step values: */N
	if strings.HasPrefix(field, "*/") {
		step, err := strconv.Atoi(field[2:])
		if err != nil || step <= 0 {
			return false
		}
		return value%step == 0
	}

	// Handle comma-separated values
	for _, part := range strings.Split(field, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		if n == value {
			return true
		}
	}

	return false
}
