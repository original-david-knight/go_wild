package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/original-david-knight/go_wild/data"
	"github.com/original-david-knight/go_wild/my"
)

func main() {
	gowild_my.LoadEnv()

	dbURL := flag.String("db", "", "PostgreSQL connection URL (or ADMIN_DATABASE_URL / DATABASE_URL env)")
	addr := flag.String("addr", "", "Admin web listen address for serve command (or ADMIN_ADDR env)")
	dryRun := flag.Bool("dry-run", false, "Show what would be deleted without deleting (delete command only)")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: admin [flags] <command> [args]\n\n")
		fmt.Fprintf(os.Stderr, "Commands:\n")
		fmt.Fprintf(os.Stderr, "  serve             Run local admin website\n")
		fmt.Fprintf(os.Stderr, "  list              List all known accounts\n")
		fmt.Fprintf(os.Stderr, "  info <pubkey>     Show account details and record counts\n")
		fmt.Fprintf(os.Stderr, "  promote <pubkey>  Promote account to premium\n")
		fmt.Fprintf(os.Stderr, "  delete <pubkey>   Delete account and related data\n")
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(1)
	}

	connStr := strings.TrimSpace(*dbURL)
	if connStr == "" {
		connStr = strings.TrimSpace(os.Getenv("ADMIN_DATABASE_URL"))
	}
	if connStr == "" {
		connStr = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if connStr == "" {
		log.Fatal("database URL required: use -db flag or ADMIN_DATABASE_URL / DATABASE_URL env")
	}

	db, err := gowild_data.NewPostgresDatabase(connStr)
	if err != nil {
		log.Fatalf("failed to connect: %v", err)
	}
	defer db.Close()

	if err := gowild_data.AddAllTables(db); err != nil {
		log.Fatalf("failed to register database tables: %v", err)
	}

	ctx := context.Background()

	switch args[0] {
	case "serve":
		if err := runAdminWeb(db, *addr); err != nil {
			log.Fatalf("admin web failed: %v", err)
		}
	case "list":
		if err := listAccountsCLI(ctx, db); err != nil {
			log.Fatalf("list failed: %v", err)
		}
	case "info":
		if len(args) < 2 {
			log.Fatal("usage: admin info <pubkey>")
		}
		if err := accountInfoCLI(ctx, db, args[1]); err != nil {
			log.Fatalf("info failed: %v", err)
		}
	case "promote":
		if len(args) < 2 {
			log.Fatal("usage: admin promote <pubkey>")
		}
		if err := promoteAccount(ctx, db, args[1], "", ""); err != nil {
			log.Fatalf("promote failed: %v", err)
		}
		fmt.Printf("Promoted %s to premium\n", args[1])
	case "delete":
		if len(args) < 2 {
			log.Fatal("usage: admin delete <pubkey>")
		}
		if err := deleteAccountCLI(ctx, db, args[1], *dryRun); err != nil {
			log.Fatalf("delete failed: %v", err)
		}
	default:
		log.Fatalf("unknown command: %s", args[0])
	}
}
