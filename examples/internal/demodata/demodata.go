// Package demodata generates the made-up shop data used by the demo databases.
package demodata

import (
	"fmt"
	"math/rand"
	"time"
)

// Row counts; Events is larger than the default row cap, to exercise truncation and
// scrolling.
const (
	NumCustomers = 40
	NumProducts  = 15
	NumOrders    = 200
	NumEvents    = 25000
)

// Data holds every row of every table, in column order; NULLs are nil.
type Data struct {
	Customers  [][]any
	Products   [][]any
	Orders     [][]any
	OrderItems [][]any
	Events     [][]any
}

var (
	firstNames = []string{"Ada", "Grace", "Alan", "Linus", "Margaret", "Dennis", "Barbara", "Ken", "Radia", "Tim", "Hedy", "Edsger", "Donald", "Frances", "Guido", "Yukihiro"}
	lastNames  = []string{"Lovelace", "Hopper", "Turing", "Torvalds", "Hamilton", "Ritchie", "Liskov", "Thompson", "Perlman", "Berners-Lee", "Lamarr", "Dijkstra", "Knuth", "Allen", "van Rossum", "Matsumoto"}
	countries  = []string{"UK", "US", "Japan", "Germany", "Brazil", "Canada", "India", "Australia"}
	categories = []string{"Books", "Hardware", "Software", "Stationery"}
	statuses   = []string{"pending", "shipped", "delivered", "delivered", "delivered", "cancelled"}
	kinds      = []string{"page_view", "page_view", "page_view", "click", "click", "login", "logout", "purchase", "error"}
	notes      = []string{"VIP customer", "Prefers email contact", "Has a really long note so that you can check how the results grid truncates cells that are wider than the column limit", "Wholesale account"}
)

func Generate() Data {
	rng := rand.New(rand.NewSource(42))
	base := time.Date(2025, 1, 1, 9, 0, 0, 0, time.UTC)
	stamp := func(offset time.Duration) string { return base.Add(offset).Format("2006-01-02 15:04:05") }
	pick := func(xs []string) string { return xs[rng.Intn(len(xs))] }

	var d Data

	for i := 1; i <= NumCustomers; i++ {
		first, last := pick(firstNames), pick(lastNames)
		var email, note any
		if rng.Intn(5) != 0 {
			email = fmt.Sprintf("%s.%s%d@example.com", first, last, i)
		}
		if rng.Intn(4) == 0 {
			note = pick(notes)
		}
		signedUp := base.AddDate(0, 0, -rng.Intn(700)).Format("2006-01-02")
		d.Customers = append(d.Customers, []any{i, first + " " + last, email, pick(countries), signedUp, note})
	}

	for i := 1; i <= NumProducts; i++ {
		cat := pick(categories)
		price := float64(rng.Intn(9000)+500) / 100
		d.Products = append(d.Products, []any{i, fmt.Sprintf("%s item %02d", cat, i), cat, price})
	}

	for i := 1; i <= NumOrders; i++ {
		at := stamp(time.Duration(rng.Intn(24*60*300)) * time.Minute)
		customer := rng.Intn(NumCustomers) + 1
		d.Orders = append(d.Orders, []any{i, customer, at, pick(statuses)})
		for n := rng.Intn(4) + 1; n > 0; n-- {
			d.OrderItems = append(d.OrderItems, []any{i, rng.Intn(NumProducts) + 1, rng.Intn(5) + 1})
		}
	}

	for i := 1; i <= NumEvents; i++ {
		var payload any
		if rng.Intn(3) != 0 {
			payload = fmt.Sprintf(`{"user":%d,"session":"%08x"}`, rng.Intn(NumCustomers)+1, rng.Uint32())
		}
		d.Events = append(d.Events, []any{i, stamp(time.Duration(i) * time.Minute), pick(kinds), payload})
	}

	return d
}
