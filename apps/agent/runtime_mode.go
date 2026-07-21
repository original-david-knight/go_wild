package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_data"
	"github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/tools/broker"
)

type agentRuntime struct {
	agentID      string
	brokerClient *broker.Client
	service      *data.AgentService
	db           gowild_data.Database
	brokerOnly   bool
	brokerErr    error
	directErr    error
}

func (r *agentRuntime) close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func (r *agentRuntime) usingBroker() bool {
	return r != nil && r.brokerClient != nil
}

func (r *agentRuntime) usingDirectService() bool {
	return r != nil && r.service != nil
}

func (r *agentRuntime) requiresBroker() bool {
	return r != nil && r.brokerOnly
}

func (r *agentRuntime) startupError() error {
	if r == nil {
		return fmt.Errorf("agent runtime is nil")
	}
	if r.usingBroker() || r.usingDirectService() {
		return nil
	}

	if r.requiresBroker() {
		if r.brokerErr != nil {
			return fmt.Errorf("broker unavailable for managed agent launch: %w", r.brokerErr)
		}
		return fmt.Errorf("broker unavailable for managed agent launch")
	}
	return nil
}

func initializeAgentRuntime(ctx context.Context, agentID string) *agentRuntime {
	runtime := &agentRuntime{
		agentID:    agentID,
		brokerOnly: managedBrokerLaunch(),
	}

	brokerClient, brokerErr := tryBrokerClient(ctx)
	if brokerErr == nil {
		runtime.brokerClient = brokerClient
		return runtime
	}
	runtime.brokerErr = brokerErr
	if runtime.requiresBroker() {
		return runtime
	}

	service, db, directErr := tryDirectAgentService(ctx, agentID)
	if directErr == nil {
		runtime.service = service
		runtime.db = db
		return runtime
	}
	runtime.directErr = directErr
	return runtime
}

func managedBrokerLaunch() bool {
	return strings.TrimSpace(os.Getenv(broker.BrokerOnlyEnv)) == "1" ||
		strings.TrimSpace(os.Getenv(broker.AgentEthPrivateKeyEnv)) != ""
}

func tryBrokerClient(ctx context.Context) (*broker.Client, error) {
	client := broker.NewClient()
	socketPath := strings.TrimPrefix(client.Endpoint(), "unix://")
	if strings.TrimSpace(socketPath) == "" {
		return nil, fmt.Errorf("broker socket path is empty")
	}
	if _, err := os.Stat(socketPath); err != nil {
		return nil, fmt.Errorf("broker socket unavailable: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if _, err := client.CallTool(pingCtx, "get_agent_name", map[string]any{}); err != nil {
		return nil, err
	}
	return client, nil
}

func tryDirectAgentService(ctx context.Context, agentID string) (*data.AgentService, gowild_data.Database, error) {
	db, err := openConfiguredDB()
	if err != nil {
		return nil, nil, err
	}

	service := data.NewAgentService(db, agentID)
	if _, err := service.EnsureAgent(ctx); err != nil {
		_ = db.Close()
		return nil, nil, fmt.Errorf("failed to initialize agent in direct mode: %w", err)
	}

	return service, db, nil
}

func openConfiguredDB() (gowild_data.Database, error) {
	dbURL := strings.TrimSpace(os.Getenv("GOWILD_DATABASE_URL"))
	if dbURL == "" {
		dbURL = defaultDatabaseURL
	}
	return openDBURL(dbURL)
}

func openDBURL(dbURL string) (gowild_data.Database, error) {
	dbURL = strings.TrimSpace(dbURL)
	if dbURL == "" {
		return nil, fmt.Errorf("database URL is empty")
	}

	var (
		db  gowild_data.Database
		err error
	)

	switch {
	case looksLikeSQLiteDSN(dbURL):
		dsn := strings.TrimPrefix(dbURL, "sqlite://")
		dsn = strings.TrimPrefix(dsn, "sqlite:")
		db, err = gowild_data.NewSqliteDatabase(dsn)
	default:
		db, err = gowild_data.NewPostgresDatabase(dbURL)
	}
	if err != nil {
		return nil, err
	}

	if err := gowild_data.AddAllTables(db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("failed to register database tables: %w", err)
	}

	return db, nil
}

func looksLikeSQLiteDSN(dbURL string) bool {
	dbURL = strings.TrimSpace(strings.ToLower(dbURL))
	switch {
	case dbURL == ":memory:":
		return true
	case strings.HasPrefix(dbURL, "sqlite://"):
		return true
	case strings.HasPrefix(dbURL, "sqlite:"):
		return true
	case strings.HasPrefix(dbURL, "file:"):
		return true
	case strings.HasSuffix(dbURL, ".db"):
		return true
	case strings.HasSuffix(dbURL, ".sqlite"):
		return true
	case strings.HasSuffix(dbURL, ".sqlite3"):
		return true
	default:
		return false
	}
}
