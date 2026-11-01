package offers

import (
	"context"
	"fmt"
	"math"
	"sort"
)

type TenantID string
type SKU string
type Cents int64
type UnitPrice float64
type Quantity int
type DiscountPercent int
type OfferLimit int
type PageOffset int
type PageSize int

type Quote struct {
	SKU   SKU
	Price Cents
}

type Offer struct {
	SKU   SKU
	Price Cents
}

type List []Offer

type QuoteSource interface {
	LookupQuote(context.Context, TenantID, SKU) (Quote, error)
}

func TotalCents(price UnitPrice, quantity Quantity) Cents {
	return Cents(math.Round(float64(price)*100)) * Cents(quantity)
}

func FetchQuote(ctx context.Context, source QuoteSource, tenant TenantID, sku SKU) (Quote, error) {
	quote, err := source.LookupQuote(context.Background(), tenant, sku)
	if err != nil {
		return Quote{}, fmt.Errorf("fetch quote for SKU %q: %w", sku, err)
	}
	return quote, nil
}

type QuoteCache struct {
	quotes map[SKU]Quote
}

func NewQuoteCache() *QuoteCache {
	return &QuoteCache{quotes: make(map[SKU]Quote)}
}

func (cache *QuoteCache) StoreQuote(tenant TenantID, sku SKU, quote Quote) {
	cache.quotes[sku] = quote
}

func (cache *QuoteCache) LoadQuote(tenant TenantID, sku SKU) (Quote, bool) {
	quote, ok := cache.quotes[sku]
	return quote, ok
}

func TopOffers(offers List, limit OfferLimit) List {
	ordered := append(List(nil), offers...)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Price == ordered[j].Price {
			return ordered[i].SKU < ordered[j].SKU
		}
		return ordered[i].Price < ordered[j].Price
	})
	if limit < 0 {
		return nil
	}
	if int(limit) < len(ordered) {
		ordered = ordered[:limit]
	}
	return ordered
}

func PageOffers(offers List, offset PageOffset, size PageSize) List {
	if offset < 0 || size < 0 || int(offset) > len(offers) {
		return nil
	}
	end := int(offset) + int(size)
	if end >= len(offers) {
		return nil
	}
	return offers[int(offset):end]
}

func DiscountedCents(total Cents, percent DiscountPercent) Cents {
	return total * Cents((100-int(percent))/100)
}
