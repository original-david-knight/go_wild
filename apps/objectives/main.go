package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	agentnode "github.com/original-david-knight/go_wild/agent_node"
	"github.com/original-david-knight/go_wild/data"
	obj "github.com/original-david-knight/go_wild/objectives"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "run":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "Usage: objectives run \"<mission description>\"")
			os.Exit(1)
		}
		mission := strings.Join(os.Args[2:], " ")
		if err := runMission(mission); err != nil {
			log.Fatalf("Error: %v", err)
		}
	case "status":
		fmt.Println("Status command not yet implemented (Phase 4)")
	case "daemon":
		if err := runDaemon(); err != nil {
			log.Fatalf("Error: %v", err)
		}
	default:
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintln(os.Stderr, "Usage: objectives <command> [args]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  run \"<mission>\"  Plan and execute a mission")
	fmt.Fprintln(os.Stderr, "  status           Show objective tree status")
	fmt.Fprintln(os.Stderr, "  daemon           Run as a long-lived scheduler")
}

func runMission(mission string) error {
	ctx := context.Background()
	cfg := obj.LoadConfig()

	if cfg.GeminiAPIKey == "" {
		return fmt.Errorf("GEMINI_API_KEY not set")
	}

	if cfg.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL not set")
	}

	// Initialize database
	db, err := gowild_data.NewPostgresDatabase(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if err := gowild_data.AddAllTables(db); err != nil {
		return fmt.Errorf("create tables: %w", err)
	}

	store := obj.NewObjectiveStore(db, "")
	activity := obj.NewActivityStore(db, "")
	memory := obj.NewMemoryStore(db)

	// Create evaluator
	evaluator, err := obj.NewPostExecutionEvaluator(cfg.GeminiAPIKey, cfg.Model, memory)
	if err != nil {
		return fmt.Errorf("create evaluator: %w", err)
	}

	// Step 1: Strategic planning — decompose mission into objective tree
	log.Printf("Planning mission: %s", mission)

	planner, err := obj.NewStrategicPlanner(cfg.GeminiAPIKey, cfg.SmartModel)
	if err != nil {
		return fmt.Errorf("create planner: %w", err)
	}

	// Get tool catalog for planner context
	tools := agentnode.DefaultWebTools()
	toolCatalog := agentnode.ToolCatalog(tools)

	// Build memory context for strategic planning
	memoryCtx, _ := memory.FormatMemoryContext(ctx, "")

	planOutput, _, err := planner.PlanStrategic(ctx, mission, nil, toolCatalog, memoryCtx)
	if err != nil {
		return fmt.Errorf("strategic planning: %w", err)
	}
	_ = memoryCtx // memory context available for future planner integration

	log.Printf("Plan reasoning: %s", planOutput.Reasoning)
	log.Printf("Mutations: %d", len(planOutput.Mutations))

	// Apply tree mutations
	if err := store.ApplyMutations(ctx, planOutput.Mutations); err != nil {
		return fmt.Errorf("apply mutations: %w", err)
	}

	activity.LogPlanCreated(ctx, "", fmt.Sprintf("Strategic plan for: %s", mission), map[string]any{
		"reasoning":      planOutput.Reasoning,
		"mutation_count": len(planOutput.Mutations),
	})

	// Step 2: Find root objectives and get leaf tasks
	roots, err := store.GetRoots(ctx)
	if err != nil {
		return fmt.Errorf("get roots: %w", err)
	}

	if len(roots) == 0 {
		log.Println("No objectives created by planner")
		return nil
	}

	log.Printf("Created %d root objective(s)", len(roots))

	// Step 3: Tactical planning for each leaf goal
	engine := obj.NewExecutionEngine(cfg, activity)
	engine.SetEvaluator(evaluator)

	for _, root := range roots {
		leaves, err := store.GetLeafTasks(ctx, root.ID)
		if err != nil {
			log.Printf("Error getting leaf tasks for %s: %v", root.Title, err)
			continue
		}

		log.Printf("Root: %s — %d leaf tasks", root.Title, len(leaves))

		for _, leaf := range leaves {
			log.Printf("  Planning leaf: %s", leaf.Title)

			children, err := store.GetChildren(ctx, leaf.ID)
			if err != nil {
				log.Printf("    Error getting children: %v", err)
				continue
			}

			tacticalPlan, graph, err := planner.PlanTactical(ctx, leaf, children, toolCatalog, "", nil)
			if err != nil {
				log.Printf("    Tactical planning failed: %v", err)
				activity.LogTaskFailed(ctx, leaf.ID, fmt.Sprintf("Tactical planning failed: %v", err), nil)
				continue
			}

			log.Printf("    Plan: %s", tacticalPlan.Reasoning)

			// Apply any tactical mutations
			if len(tacticalPlan.Mutations) > 0 {
				if err := store.ApplyMutations(ctx, tacticalPlan.Mutations); err != nil {
					log.Printf("    Apply tactical mutations failed: %v", err)
				}
			}

			// Execute if we have a graph
			if graph != nil && len(graph.Nodes) > 0 {
				log.Printf("    Executing %d nodes...", len(graph.Nodes))

				// Mark as active
				leaf.Status = obj.StatusActive
				store.Update(ctx, leaf)

				results, err := engine.Execute(ctx, leaf, graph)
				if err != nil {
					log.Printf("    Execution failed: %v", err)
					leaf.Status = obj.StatusFailed
					leaf.LastResult = err.Error()
					store.Update(ctx, leaf)
					continue
				}

				// Store results summary
				resultJSON, _ := json.MarshalIndent(results.NodeResults, "", "  ")
				leaf.Status = obj.StatusCompleted
				leaf.LastResult = string(resultJSON)
				store.Update(ctx, leaf)

				log.Printf("    Completed with %d results (sufficient=%v)", len(results.NodeResults), results.Sufficient)

				// Print results
				for nodeID, result := range results.NodeResults {
					var text string
					if err := json.Unmarshal(result, &text); err == nil {
						log.Printf("    [%s] %s", nodeID, truncate(text, 200))
					} else {
						log.Printf("    [%s] %s", nodeID, truncate(string(result), 200))
					}
				}
			} else {
				log.Printf("    No execution graph produced")
			}
		}
	}

	log.Println("Mission execution complete")
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func runDaemon() error {
	cfg := obj.LoadConfig()

	if cfg.GeminiAPIKey == "" {
		return fmt.Errorf("GEMINI_API_KEY not set")
	}
	if cfg.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL not set")
	}

	// Initialize database
	db, err := gowild_data.NewPostgresDatabase(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	if err := gowild_data.AddAllTables(db); err != nil {
		return fmt.Errorf("create tables: %w", err)
	}

	store := obj.NewObjectiveStore(db, "")
	activity := obj.NewActivityStore(db, "")
	memory := obj.NewMemoryStore(db)

	planner, err := obj.NewStrategicPlanner(cfg.GeminiAPIKey, cfg.Model)
	if err != nil {
		return fmt.Errorf("create planner: %w", err)
	}

	engine := obj.NewExecutionEngine(cfg, activity)

	evaluator, err := obj.NewPostExecutionEvaluator(cfg.GeminiAPIKey, cfg.Model, memory)
	if err != nil {
		return fmt.Errorf("create evaluator: %w", err)
	}
	engine.SetEvaluator(evaluator)

	scheduler := obj.NewScheduler(store, activity, planner, engine, cfg)

	// Start API server
	api := obj.NewAPIServer(store, activity)
	go func() {
		log.Printf("Dashboard at http://localhost%s", cfg.ListenAddr)
		if err := api.ListenAndServe(cfg.ListenAddr); err != nil {
			log.Fatalf("API server error: %v", err)
		}
	}()

	// Graceful shutdown on SIGINT/SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Println("Starting objectives daemon...")
	return scheduler.Run(ctx)
}
