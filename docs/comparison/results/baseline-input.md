Review the git diff below against ALL the repository rules below and also look for correctness defects not covered by a rule. Review only added or changed code. Read repository files when necessary. Use one review session; do not delegate, fork, or spawn other agents. Do not edit files, execute the code, build, or run tests. Follow the same rule coverage discipline for every rule: split atomic requirements, walk every applicable changed line, and record evidence for PASS, FAIL, or N/A. Report only actionable defects supported by the code and the stated contracts. Do not speculate about callers violating the stated input contracts.

End with one fenced JSON block with this shape:
{"rules":[{"id":"rule id","verdict":"PASS or FAIL or N/A","evidence":"what was checked","findings":[{"file":"offers.go","line":1,"message":"problem"}]}],"correctness":[{"file":"offers.go","line":1,"message":"defect and a concrete input that demonstrates it"}]}

Repository instructions and rules:
{
  "instructions": "An offers package for a multi-tenant shop. Callers provide nonnegative prices and quantities, discount percentages from 0 through 100, and nonnegative page sizes and offsets. Cache instances are used by one goroutine. PageOffers must return every available item in the requested page, including a page ending exactly at the slice length. DiscountedCents applies the stated percentage discount to a cent total. TopOffers returns up to limit cheapest offers.",
  "rules": [
    {
      "id": "money-rounding",
      "rule": "Round monetary order totals to the nearest cent only after multiplying the unit price by the quantity. Do not round each unit first.",
      "scope": [
        "**/*.go"
      ],
      "severity": "medium"
    },
    {
      "id": "caller-context",
      "rule": "Pass the caller context to every outbound quote lookup so cancellation and context values propagate.",
      "scope": [
        "**/*.go"
      ],
      "severity": "medium"
    },
    {
      "id": "tenant-cache",
      "rule": "A cached quote must be keyed by both tenant ID and SKU. One tenant must never read another tenant's cached quote.",
      "scope": [
        "**/*.go"
      ],
      "severity": "medium"
    },
    {
      "id": "offer-ties",
      "rule": "Rank offers by ascending price. Preserve the original input order when prices tie.",
      "scope": [
        "**/*.go"
      ],
      "severity": "medium"
    },
    {
      "id": "input-ownership",
      "rule": "Functions receiving an offer slice must not mutate the caller's slice or its elements.",
      "scope": [
        "**/*.go"
      ],
      "severity": "medium"
    },
    {
      "id": "lookup-errors",
      "rule": "An outbound quote lookup error must be wrapped with its original cause and include the operation and SKU.",
      "scope": [
        "**/*.go"
      ],
      "severity": "medium"
    }
  ]
}


Diff (base main, including working tree):
diff --git a/offers.go b/offers.go
index 25848ff..e8098bf 100644
--- a/offers.go
+++ b/offers.go
@@ -1 +1,95 @@
 package offers
+
+import (
+	"context"
+	"fmt"
+	"math"
+	"sort"
+)
+
+type TenantID string
+type SKU string
+type Cents int64
+type UnitPrice float64
+type Quantity int
+type DiscountPercent int
+type OfferLimit int
+type PageOffset int
+type PageSize int
+
+type Quote struct {
+	SKU   SKU
+	Price Cents
+}
+
+type Offer struct {
+	SKU   SKU
+	Price Cents
+}
+
+type List []Offer
+
+type QuoteSource interface {
+	LookupQuote(context.Context, TenantID, SKU) (Quote, error)
+}
+
+func TotalCents(price UnitPrice, quantity Quantity) Cents {
+	return Cents(math.Round(float64(price)*100)) * Cents(quantity)
+}
+
+func FetchQuote(ctx context.Context, source QuoteSource, tenant TenantID, sku SKU) (Quote, error) {
+	quote, err := source.LookupQuote(context.Background(), tenant, sku)
+	if err != nil {
+		return Quote{}, fmt.Errorf("fetch quote for SKU %q: %w", sku, err)
+	}
+	return quote, nil
+}
+
+type QuoteCache struct {
+	quotes map[SKU]Quote
+}
+
+func NewQuoteCache() *QuoteCache {
+	return &QuoteCache{quotes: make(map[SKU]Quote)}
+}
+
+func (cache *QuoteCache) StoreQuote(tenant TenantID, sku SKU, quote Quote) {
+	cache.quotes[sku] = quote
+}
+
+func (cache *QuoteCache) LoadQuote(tenant TenantID, sku SKU) (Quote, bool) {
+	quote, ok := cache.quotes[sku]
+	return quote, ok
+}
+
+func TopOffers(offers List, limit OfferLimit) List {
+	ordered := append(List(nil), offers...)
+	sort.Slice(ordered, func(i, j int) bool {
+		if ordered[i].Price == ordered[j].Price {
+			return ordered[i].SKU < ordered[j].SKU
+		}
+		return ordered[i].Price < ordered[j].Price
+	})
+	if limit < 0 {
+		return nil
+	}
+	if int(limit) < len(ordered) {
+		ordered = ordered[:limit]
+	}
+	return ordered
+}
+
+func PageOffers(offers List, offset PageOffset, size PageSize) List {
+	if offset < 0 || size < 0 || int(offset) > len(offers) {
+		return nil
+	}
+	end := int(offset) + int(size)
+	if end >= len(offers) {
+		return nil
+	}
+	return offers[int(offset):end]
+}
+
+func DiscountedCents(total Cents, percent DiscountPercent) Cents {
+	return total * Cents((100-int(percent))/100)
+}
