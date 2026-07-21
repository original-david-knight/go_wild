package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/agent_net"
	"github.com/original-david-knight/go_wild/data"
)

type tableCount struct {
	name  string
	count int
}

type deleteSummary struct {
	ByTable map[string]int
	Total   int
}

type accountRow struct {
	PublicKey string
	Name      string
	Tier      string
	Revoked   bool
	PostCount int
}

func newDeleteSummary() deleteSummary {
	return deleteSummary{ByTable: make(map[string]int)}
}

func (s *deleteSummary) add(table string, count int) {
	if count <= 0 {
		return
	}
	s.ByTable[table] += count
	s.Total += count
}

func normalizePubKey(pubkey string) string {
	return strings.TrimSpace(pubkey)
}

func queryByField(ctx context.Context, db gowild_data.Database, model any, field string, value string) ([]any, error) {
	var all []any
	offset := 0
	for {
		results, err := db.Table(model).Query(ctx, gowild_data.QueryOpts{
			Where:  map[string]any{field: value},
			Limit:  200,
			Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		if len(results) == 0 {
			break
		}
		all = append(all, results...)
		offset += len(results)
	}
	return all, nil
}

func getRecordID(record any) (string, error) {
	v := reflect.ValueOf(record)
	if !v.IsValid() {
		return "", fmt.Errorf("invalid record")
	}
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return "", fmt.Errorf("nil record")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return "", fmt.Errorf("record is not a struct")
	}
	id := v.FieldByName("ID")
	if !id.IsValid() || id.Kind() != reflect.String {
		return "", fmt.Errorf("record does not contain string ID field")
	}
	idValue := strings.TrimSpace(id.String())
	if idValue == "" {
		return "", fmt.Errorf("record has empty ID")
	}
	return idValue, nil
}

func deleteRecords(ctx context.Context, db gowild_data.Database, model any, records []any) (int, error) {
	table := db.Table(model)
	deleted := 0
	for _, r := range records {
		id, err := getRecordID(r)
		if err != nil {
			return deleted, err
		}
		if err := table.Delete(ctx, id); err != nil {
			return deleted, err
		}
		deleted++
	}
	return deleted, nil
}

func dedupeByID(records []any) []any {
	if len(records) == 0 {
		return records
	}
	seen := make(map[string]struct{}, len(records))
	unique := make([]any, 0, len(records))
	for _, r := range records {
		id, err := getRecordID(r)
		if err != nil {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, r)
	}
	return unique
}

func countAgentRecords(ctx context.Context, db gowild_data.Database, pubkey string) ([]tableCount, error) {
	pubkey = normalizePubKey(pubkey)
	if pubkey == "" {
		return nil, fmt.Errorf("public key is required")
	}

	counts := []tableCount{}

	for _, tc := range []struct {
		name  string
		model any
		field string
	}{
		{"premium_agents", gowild_agent_net.PremiumAgent{}, "public_key"},
		{"revoked_keys", gowild_agent_net.RevokedKey{}, "public_key"},
		{"agent_profiles", gowild_agent_net.AgentProfile{}, "public_key"},
		{"posts", gowild_agent_net.Post{}, "public_key"},
		{"used_nonces", gowild_agent_net.UsedNonce{}, "public_key"},
		{"rate_limits", gowild_agent_net.RateLimit{}, "public_key"},
		{"a2a_jobs (sent)", gowild_agent_net.A2AJob{}, "from_public_key"},
		{"a2a_jobs (received)", gowild_agent_net.A2AJob{}, "to_public_key"},
	} {
		results, err := queryByField(ctx, db, tc.model, tc.field, pubkey)
		if err != nil {
			return nil, err
		}
		counts = append(counts, tableCount{name: tc.name, count: len(results)})
	}

	sent, err := queryByField(ctx, db, gowild_agent_net.DirectMessage{}, "from_public_key", pubkey)
	if err != nil {
		return nil, err
	}
	received, err := queryByField(ctx, db, gowild_agent_net.DirectMessage{}, "to_public_key", pubkey)
	if err != nil {
		return nil, err
	}
	counts = append(counts, tableCount{name: "direct_messages (sent)", count: len(sent)})
	counts = append(counts, tableCount{name: "direct_messages (received)", count: len(received)})

	// Count A2A job events linked to this account's jobs.
	jobsSent, err := queryByField(ctx, db, gowild_agent_net.A2AJob{}, "from_public_key", pubkey)
	if err != nil {
		return nil, err
	}
	jobsReceived, err := queryByField(ctx, db, gowild_agent_net.A2AJob{}, "to_public_key", pubkey)
	if err != nil {
		return nil, err
	}
	jobs := dedupeByID(append(jobsSent, jobsReceived...))
	eventCount := 0
	for _, job := range jobs {
		jobID, err := getRecordID(job)
		if err != nil {
			continue
		}
		events, err := queryByField(ctx, db, gowild_agent_net.A2AJobEvent{}, "job_id", jobID)
		if err != nil {
			return nil, err
		}
		eventCount += len(events)
	}
	counts = append(counts, tableCount{name: "a2a_job_events", count: eventCount})

	return counts, nil
}

func listAccounts(ctx context.Context, db gowild_data.Database) ([]accountRow, error) {
	accountsByKey := map[string]*accountRow{}
	upsert := func(pubkey string) *accountRow {
		pubkey = normalizePubKey(pubkey)
		if pubkey == "" {
			return nil
		}
		row, ok := accountsByKey[pubkey]
		if !ok {
			row = &accountRow{PublicKey: pubkey, Tier: "free"}
			accountsByKey[pubkey] = row
		}
		return row
	}

	premiums, err := db.Table(gowild_agent_net.PremiumAgent{}).GetAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range premiums {
		agent, ok := r.(*gowild_agent_net.PremiumAgent)
		if !ok {
			continue
		}
		row := upsert(agent.PublicKey)
		if row == nil {
			continue
		}
		row.Tier = "premium"
		row.Revoked = agent.Revoked
	}

	profiles, err := db.Table(gowild_agent_net.AgentProfile{}).GetAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range profiles {
		profile, ok := r.(*gowild_agent_net.AgentProfile)
		if !ok {
			continue
		}
		row := upsert(profile.PublicKey)
		if row == nil {
			continue
		}
		row.Name = strings.TrimSpace(profile.Name)
	}

	posts, err := db.Table(gowild_agent_net.Post{}).GetAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, r := range posts {
		post, ok := r.(*gowild_agent_net.Post)
		if !ok {
			continue
		}
		row := upsert(post.PublicKey)
		if row == nil {
			continue
		}
		row.PostCount++
	}

	messages, err := db.Table(gowild_agent_net.DirectMessage{}).GetAll(ctx)
	if err == nil {
		for _, r := range messages {
			msg, ok := r.(*gowild_agent_net.DirectMessage)
			if !ok {
				continue
			}
			upsert(msg.FromPublicKey)
			upsert(msg.ToPublicKey)
		}
	}

	jobs, err := db.Table(gowild_agent_net.A2AJob{}).GetAll(ctx)
	if err == nil {
		for _, r := range jobs {
			job, ok := r.(*gowild_agent_net.A2AJob)
			if !ok {
				continue
			}
			upsert(job.FromPublicKey)
			upsert(job.ToPublicKey)
		}
	}

	rows := make([]accountRow, 0, len(accountsByKey))
	for _, row := range accountsByKey {
		rows = append(rows, *row)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Tier != rows[j].Tier {
			return rows[i].Tier == "premium"
		}
		if rows[i].PostCount != rows[j].PostCount {
			return rows[i].PostCount > rows[j].PostCount
		}
		if rows[i].Name != rows[j].Name {
			return rows[i].Name < rows[j].Name
		}
		return rows[i].PublicKey < rows[j].PublicKey
	})

	return rows, nil
}

func promoteAccount(ctx context.Context, db gowild_data.Database, pubkey string, chain string, txHash string) error {
	pubkey = normalizePubKey(pubkey)
	if pubkey == "" {
		return fmt.Errorf("public key is required")
	}

	normalizedChain := strings.ToLower(strings.TrimSpace(chain))
	if normalizedChain == "" {
		normalizedChain = gowild_agent_net.ChainSolana
	}
	switch normalizedChain {
	case gowild_agent_net.ChainSolana, gowild_agent_net.ChainEthereum, gowild_agent_net.ChainBase:
	default:
		return fmt.Errorf("unsupported chain %q", chain)
	}

	txHash = strings.TrimSpace(txHash)
	now := time.Now().UTC()

	return db.RunInTransaction(ctx, func(tx gowild_data.Database) error {
		existingRows, err := queryByField(ctx, tx, gowild_agent_net.PremiumAgent{}, "public_key", pubkey)
		if err != nil {
			return err
		}

		if len(existingRows) > 0 {
			agent, ok := existingRows[0].(*gowild_agent_net.PremiumAgent)
			if !ok {
				return fmt.Errorf("failed to load existing premium account")
			}

			if txHash != "" {
				if err := validateTxHashOwnership(ctx, tx, txHash, pubkey); err != nil {
					return err
				}
				agent.TxHash = txHash
			}
			agent.Chain = normalizedChain
			agent.Revoked = false
			if agent.UpgradedAt.IsZero() {
				agent.UpgradedAt = now
			}
			agent.LastActiveAt = now
			return tx.Table(gowild_agent_net.PremiumAgent{}).Update(ctx, agent)
		}

		if txHash == "" {
			txHash = buildAdminTxHash(pubkey, now)
		}
		if err := validateTxHashOwnership(ctx, tx, txHash, pubkey); err != nil {
			return err
		}

		agent := &gowild_agent_net.PremiumAgent{
			ID:           pubkey,
			PublicKey:    pubkey,
			TxHash:       txHash,
			Chain:        normalizedChain,
			UpgradedAt:   now,
			Revoked:      false,
			LastActiveAt: now,
		}
		return tx.Table(gowild_agent_net.PremiumAgent{}).Insert(ctx, agent)
	})
}

func validateTxHashOwnership(ctx context.Context, db gowild_data.Database, txHash string, targetPubkey string) error {
	rows, err := queryByField(ctx, db, gowild_agent_net.PremiumAgent{}, "tx_hash", txHash)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}
	agent, ok := rows[0].(*gowild_agent_net.PremiumAgent)
	if !ok {
		return fmt.Errorf("failed to validate tx hash ownership")
	}
	if agent.PublicKey != targetPubkey {
		return fmt.Errorf("transaction hash already used by another account")
	}
	return nil
}

func buildAdminTxHash(pubkey string, now time.Time) string {
	prefix := pubkey
	if len(prefix) > 8 {
		prefix = prefix[:8]
	}
	prefix = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, prefix)
	return fmt.Sprintf("admin-%d-%s", now.UnixNano(), prefix)
}

func deleteAccountCascade(ctx context.Context, db gowild_data.Database, pubkey string) (deleteSummary, error) {
	pubkey = normalizePubKey(pubkey)
	if pubkey == "" {
		return deleteSummary{}, fmt.Errorf("public key is required")
	}

	summary := newDeleteSummary()

	err := db.RunInTransaction(ctx, func(tx gowild_data.Database) error {
		for _, tc := range []struct {
			tableName string
			model     any
			field     string
		}{
			{"premium_agents", gowild_agent_net.PremiumAgent{}, "public_key"},
			{"revoked_keys", gowild_agent_net.RevokedKey{}, "public_key"},
			{"agent_profiles", gowild_agent_net.AgentProfile{}, "public_key"},
			{"posts", gowild_agent_net.Post{}, "public_key"},
			{"used_nonces", gowild_agent_net.UsedNonce{}, "public_key"},
			{"rate_limits", gowild_agent_net.RateLimit{}, "public_key"},
		} {
			records, err := queryByField(ctx, tx, tc.model, tc.field, pubkey)
			if err != nil {
				return err
			}
			deleted, err := deleteRecords(ctx, tx, tc.model, records)
			if err != nil {
				return err
			}
			summary.add(tc.tableName, deleted)
		}

		sent, err := queryByField(ctx, tx, gowild_agent_net.DirectMessage{}, "from_public_key", pubkey)
		if err != nil {
			return err
		}
		received, err := queryByField(ctx, tx, gowild_agent_net.DirectMessage{}, "to_public_key", pubkey)
		if err != nil {
			return err
		}
		messages := dedupeByID(append(sent, received...))
		deletedMessages, err := deleteRecords(ctx, tx, gowild_agent_net.DirectMessage{}, messages)
		if err != nil {
			return err
		}
		summary.add("direct_messages", deletedMessages)

		jobsFrom, err := queryByField(ctx, tx, gowild_agent_net.A2AJob{}, "from_public_key", pubkey)
		if err != nil {
			return err
		}
		jobsTo, err := queryByField(ctx, tx, gowild_agent_net.A2AJob{}, "to_public_key", pubkey)
		if err != nil {
			return err
		}
		jobs := dedupeByID(append(jobsFrom, jobsTo...))

		jobEvents := make([]any, 0)
		for _, job := range jobs {
			jobID, err := getRecordID(job)
			if err != nil {
				return err
			}
			events, err := queryByField(ctx, tx, gowild_agent_net.A2AJobEvent{}, "job_id", jobID)
			if err != nil {
				return err
			}
			jobEvents = append(jobEvents, events...)
		}
		jobEvents = dedupeByID(jobEvents)
		deletedEvents, err := deleteRecords(ctx, tx, gowild_agent_net.A2AJobEvent{}, jobEvents)
		if err != nil {
			return err
		}
		summary.add("a2a_job_events", deletedEvents)

		deletedJobs, err := deleteRecords(ctx, tx, gowild_agent_net.A2AJob{}, jobs)
		if err != nil {
			return err
		}
		summary.add("a2a_jobs", deletedJobs)

		return nil
	})

	if err != nil {
		return deleteSummary{}, err
	}

	return summary, nil
}

func listAccountsCLI(ctx context.Context, db gowild_data.Database) error {
	accounts, err := listAccounts(ctx, db)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		fmt.Println("No accounts found.")
		return nil
	}

	fmt.Printf("%-48s %-10s %-8s %-8s %s\n", "PUBLIC KEY", "TIER", "REVOKED", "POSTS", "NAME")
	fmt.Printf("%-48s %-10s %-8s %-8s %s\n", strings.Repeat("-", 44), "----", "-------", "-----", "----")
	for _, account := range accounts {
		revoked := ""
		if account.Revoked {
			revoked = "YES"
		}
		display := account.PublicKey
		if len(display) > 44 {
			display = display[:20] + "..." + display[len(display)-20:]
		}
		fmt.Printf("%-48s %-10s %-8s %-8d %s\n", display, account.Tier, revoked, account.PostCount, account.Name)
	}
	fmt.Printf("\nTotal: %d accounts\n", len(accounts))
	return nil
}

func accountInfoCLI(ctx context.Context, db gowild_data.Database, pubkey string) error {
	pubkey = normalizePubKey(pubkey)
	if pubkey == "" {
		return fmt.Errorf("public key is required")
	}

	fmt.Printf("Account: %s\n\n", pubkey)

	premiumRows, err := queryByField(ctx, db, gowild_agent_net.PremiumAgent{}, "public_key", pubkey)
	if err != nil {
		return err
	}
	if len(premiumRows) == 0 {
		fmt.Printf("  Tier:     free\n")
	} else if agent, ok := premiumRows[0].(*gowild_agent_net.PremiumAgent); ok {
		fmt.Printf("  Tier:     premium\n")
		fmt.Printf("  Chain:    %s\n", agent.Chain)
		fmt.Printf("  TxHash:   %s\n", agent.TxHash)
		fmt.Printf("  Revoked:  %v\n", agent.Revoked)
		fmt.Printf("  Upgraded: %s\n", agent.UpgradedAt.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Active:   %s\n", agent.LastActiveAt.Format("2006-01-02 15:04:05"))
	}

	profileRows, err := queryByField(ctx, db, gowild_agent_net.AgentProfile{}, "public_key", pubkey)
	if err != nil {
		return err
	}
	if len(profileRows) > 0 {
		if profile, ok := profileRows[0].(*gowild_agent_net.AgentProfile); ok {
			fmt.Printf("  Name:     %s\n", profile.Name)
			if profile.URL != "" {
				fmt.Printf("  URL:      %s\n", profile.URL)
			}
		}
	}

	fmt.Println()

	counts, err := countAgentRecords(ctx, db, pubkey)
	if err != nil {
		return err
	}
	total := 0
	for _, tc := range counts {
		fmt.Printf("  %-30s %d\n", tc.name, tc.count)
		total += tc.count
	}
	fmt.Printf("  %-30s %d\n", "TOTAL", total)
	return nil
}

func deleteAccountCLI(ctx context.Context, db gowild_data.Database, pubkey string, dryRun bool) error {
	pubkey = normalizePubKey(pubkey)
	if pubkey == "" {
		return fmt.Errorf("public key is required")
	}

	counts, err := countAgentRecords(ctx, db, pubkey)
	if err != nil {
		return err
	}
	total := 0
	for _, tc := range counts {
		total += tc.count
	}
	if total == 0 {
		fmt.Printf("No records found for account %s\n", pubkey)
		return nil
	}

	fmt.Printf("Account: %s\n", pubkey)
	fmt.Println("Records to delete:")
	for _, tc := range counts {
		if tc.count > 0 {
			fmt.Printf("  %-30s %d\n", tc.name, tc.count)
		}
	}
	fmt.Printf("  %-30s %d\n", "TOTAL", total)

	if dryRun {
		fmt.Println("\n[dry-run] No records deleted.")
		return nil
	}

	fmt.Printf("\nType 'yes' to confirm deletion: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	if strings.TrimSpace(scanner.Text()) != "yes" {
		fmt.Println("Aborted.")
		return nil
	}

	summary, err := deleteAccountCascade(ctx, db, pubkey)
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(summary.ByTable))
	for table := range summary.ByTable {
		keys = append(keys, table)
	}
	sort.Strings(keys)
	for _, table := range keys {
		fmt.Printf("  deleted %-20s %d\n", table, summary.ByTable[table])
	}
	fmt.Printf("\nDone. Deleted %d records for account %s\n", summary.Total, pubkey)
	return nil
}
