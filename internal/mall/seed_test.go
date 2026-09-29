package mall

import (
	"path/filepath"
	"testing"
)

func TestSeedSeparatesDeploymentAndPreview(t *testing.T) {
	for _, demo := range []bool{false, true} {
		name := "stack"
		if demo {
			name = "demo"
		}
		t.Run(name, func(t *testing.T) {
			dsn := "file:" + filepath.ToSlash(filepath.Join(t.TempDir(), "seed.db"))
			d, err := OpenDB(testCtx, "sqlite", dsn)
			must(t, err)
			t.Cleanup(func() { d.SQL.Close() })
			must(t, d.Migrate(testCtx))
			ids, err := d.Seed(testCtx, "owner@example.com", "initial-password-123", demo)
			must(t, err)
			var admins, customers, products, activities int
			must(t, d.SQL.QueryRow("SELECT COUNT(*) FROM mall_users WHERE role='admin'").Scan(&admins))
			must(t, d.SQL.QueryRow("SELECT COUNT(*) FROM mall_users WHERE role='customer'").Scan(&customers))
			must(t, d.SQL.QueryRow("SELECT COUNT(*) FROM mall_products").Scan(&products))
			must(t, d.SQL.QueryRow("SELECT COUNT(*) FROM mall_activities").Scan(&activities))
			if admins != 1 {
				t.Fatalf("administrators = %d, want 1", admins)
			}
			if demo {
				if customers != 1 || products == 0 || activities == 0 || len(ids) == 0 {
					t.Fatalf("preview data missing: customers=%d products=%d activities=%d ids=%v", customers, products, activities, ids)
				}
			} else if customers != 0 || products != 0 || activities != 0 || len(ids) != 0 {
				t.Fatalf("sample data leaked into deployment: customers=%d products=%d activities=%d ids=%v", customers, products, activities, ids)
			}
			var passwordHash string
			must(t, d.SQL.QueryRow("SELECT password_hash FROM mall_users WHERE email='owner@example.com'").Scan(&passwordHash))
			// Reinitialization must preserve the configured account and merchant inventory.
			p, err := d.SaveProduct(testCtx, 0, Product{Name: "商家商品", Category: "桌面数码", Image: "keyboard", Price: 10000, OriginalPrice: 10000, Stock: 7, Status: "active"})
			must(t, err)
			ids, err = d.Seed(testCtx, "replacement@example.com", "replacement-password-456", demo)
			must(t, err)
			var newHash string
			var newProducts, newUsers, stock int
			must(t, d.SQL.QueryRow("SELECT password_hash FROM mall_users WHERE email='owner@example.com'").Scan(&newHash))
			must(t, d.SQL.QueryRow("SELECT COUNT(*) FROM mall_users").Scan(&newUsers))
			must(t, d.SQL.QueryRow("SELECT COUNT(*) FROM mall_products").Scan(&newProducts))
			must(t, d.SQL.QueryRow("SELECT stock FROM mall_products WHERE id=?", p.ID).Scan(&stock))
			if newHash != passwordHash || newUsers != admins+customers || newProducts != products+1 || stock != 7 || len(ids) != 0 {
				t.Fatal("reinitialization changed existing accounts or inventory")
			}
		})
	}
}
