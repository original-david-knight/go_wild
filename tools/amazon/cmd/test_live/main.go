package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/original-david-knight/go_wild/tools/amazon"
)

func main() {
	keywords := flag.String("keywords", "water filter", "search keywords")
	marketplace := flag.String("marketplace", envOrDefault("AMAZON_MARKETPLACE", "US"), "marketplace code (US, UK, DE, FR, JP, CA, AU)")
	limit := flag.Int("limit", 3, "number of search results (1-10)")
	asin := flag.String("asin", "", "optional ASIN for get-items check; defaults to first search result ASIN")
	timeout := flag.Duration("timeout", 30*time.Second, "request timeout")
	flag.Parse()

	accessKey := firstNonEmptyEnv("AMAZON_PAAPI_ACCESS_KEY", "AMAZON_ACCESS_KEY")
	secretKey := firstNonEmptyEnv("AMAZON_PAAPI_SECRET_KEY", "AMAZON_SECRET_KEY")
	partnerTag := strings.TrimSpace(os.Getenv("AMAZON_PARTNER_TAG"))
	missing := missingCredentials(accessKey, secretKey, partnerTag)
	if len(missing) > 0 {
		log.Fatalf("missing required env var(s): %s", strings.Join(missing, ", "))
	}

	client := amazon.NewPAAClient(accessKey, secretKey, partnerTag, *marketplace)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	fmt.Printf("Running live Amazon PAAPI test\n")
	fmt.Printf(" marketplace=%s\n", strings.ToUpper(strings.TrimSpace(*marketplace)))
	fmt.Printf(" keywords=%q limit=%d\n", strings.TrimSpace(*keywords), *limit)

	searchResult, err := client.SearchItems(ctx, amazon.SearchInput{
		Keywords: strings.TrimSpace(*keywords),
		Limit:    *limit,
	})
	if err != nil {
		log.Fatalf("SearchItems failed: %v", err)
	}
	fmt.Printf("SearchItems ok: total_results=%d returned=%d\n", searchResult.TotalResults, len(searchResult.Products))
	for i, p := range searchResult.Products {
		fmt.Printf(" %d. asin=%s title=%q price=%s prime=%v in_stock=%v\n", i+1, p.ASIN, p.Title, p.Price, p.IsPrime, p.InStock)
	}

	lookupASIN := strings.TrimSpace(*asin)
	if lookupASIN == "" && len(searchResult.Products) > 0 {
		lookupASIN = strings.TrimSpace(searchResult.Products[0].ASIN)
	}
	if lookupASIN == "" {
		fmt.Println("No ASIN available to run GetItems lookup; exiting after SearchItems validation.")
		return
	}

	itemResult, err := client.GetItems(ctx, []string{lookupASIN})
	if err != nil {
		log.Fatalf("GetItems failed for ASIN %s: %v", lookupASIN, err)
	}
	fmt.Printf("GetItems ok: asin=%s returned=%d\n", lookupASIN, len(itemResult.Products))
	if len(itemResult.Products) > 0 {
		p := itemResult.Products[0]
		fmt.Printf(" product: title=%q price=%s seller=%q rank=%d\n", p.Title, p.Price, p.Seller, p.SalesRank)
	}
}

func firstNonEmptyEnv(names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	return ""
}

func envOrDefault(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func missingCredentials(accessKey, secretKey, partnerTag string) []string {
	var missing []string
	if strings.TrimSpace(accessKey) == "" {
		missing = append(missing, "AMAZON_PAAPI_ACCESS_KEY (or AMAZON_ACCESS_KEY)")
	}
	if strings.TrimSpace(secretKey) == "" {
		missing = append(missing, "AMAZON_PAAPI_SECRET_KEY (or AMAZON_SECRET_KEY)")
	}
	if strings.TrimSpace(partnerTag) == "" {
		missing = append(missing, "AMAZON_PARTNER_TAG")
	}
	return missing
}
