package mall

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

const productCols = "id,name,subtitle,description,category,image,price_cents,original_price_cents,stock,initial_stock,status,featured,created_at"

func scanProduct(row rowScanner) (p Product, err error) {
	err = row.Scan(&p.ID, &p.Name, &p.Subtitle, &p.Description, &p.Category, &p.Image, &p.Price, &p.OriginalPrice, &p.Stock, &p.InitialStock, &p.Status, &p.Featured, &p.CreatedAt)
	return p, missing(err)
}
func (d *DB) Product(ctx context.Context, id int64, admin bool) (Product, error) {
	q := "SELECT " + productCols + " FROM mall_products WHERE id=?"
	if !admin {
		q += " AND status='active'"
	}
	return scanProduct(d.SQL.QueryRowContext(ctx, q, id))
}
func (d *DB) Products(ctx context.Context, search, category, sortBy string, page, size int, admin bool) (Page[Product], error) {
	result := Page[Product]{Items: []Product{}, Page: page, Size: size}
	where := " WHERE 1=1"
	args := []any{}
	if !admin {
		where += " AND status='active'"
	}
	if search != "" {
		where += " AND (name LIKE ? OR subtitle LIKE ?)"
		s := "%" + search + "%"
		args = append(args, s, s)
	}
	if category != "" {
		where += " AND category=?"
		args = append(args, category)
	}
	if err := d.SQL.QueryRowContext(ctx, "SELECT COUNT(*) FROM mall_products"+where, args...).Scan(&result.Total); err != nil {
		return result, err
	}
	order := "featured DESC,id DESC"
	switch sortBy {
	case "price_asc":
		order = "price_cents ASC,id DESC"
	case "price_desc":
		order = "price_cents DESC,id DESC"
	case "newest":
		order = "id DESC"
	}
	args = append(args, size, (page-1)*size)
	rows, err := d.SQL.QueryContext(ctx, "SELECT "+productCols+" FROM mall_products"+where+" ORDER BY "+order+" LIMIT ? OFFSET ?", args...)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return result, err
		}
		result.Items = append(result.Items, p)
	}
	return result, rows.Err()
}
func (d *DB) SaveProduct(ctx context.Context, actor int64, p Product) (Product, error) {
	p.Name = strings.TrimSpace(p.Name)
	if len([]rune(p.Name)) < 2 || len([]rune(p.Name)) > 100 || len([]rune(p.Subtitle)) > 120 || len([]rune(p.Description)) > 3000 || p.Price <= 0 || p.Price > 100000000 || p.OriginalPrice < p.Price || p.OriginalPrice > 100000000 || p.Stock < 0 || p.Stock > 1000000 {
		return p, bad("商品名称、金额或库存不合法")
	}
	if p.Category != "桌面数码" && p.Category != "通勤随行" && p.Category != "品质生活" {
		return p, bad("请选择有效分类")
	}
	if p.Status != "active" && p.Status != "hidden" {
		return p, bad("商品状态无效")
	}
	switch p.Image {
	case "keyboard", "headphones", "lamp", "speaker", "bag", "bottle", "watch", "camera", "mouse":
	default:
		return p, bad("请选择有效商品插画")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if p.ID == 0 {
		p.CreatedAt = nowMS()
		p.InitialStock = p.Stock
		r, e := tx.ExecContext(ctx, "INSERT INTO mall_products(name,subtitle,description,category,image,price_cents,original_price_cents,stock,initial_stock,status,featured,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)", p.Name, p.Subtitle, p.Description, p.Category, p.Image, p.Price, p.OriginalPrice, p.Stock, p.Stock, p.Status, p.Featured, p.CreatedAt)
		if e != nil {
			return p, e
		}
		p.ID, err = r.LastInsertId()
	} else {
		old, e := scanProduct(tx.QueryRowContext(ctx, "SELECT "+productCols+" FROM mall_products WHERE id=?"+d.Lock(), p.ID))
		if e != nil {
			return p, e
		}
		// 编辑商品信息不能覆盖实时库存；补货走独立的加法接口。
		p.Stock, p.InitialStock, p.CreatedAt = old.Stock, old.InitialStock, old.CreatedAt
		_, err = tx.ExecContext(ctx, "UPDATE mall_products SET name=?,subtitle=?,description=?,category=?,image=?,price_cents=?,original_price_cents=?,status=?,featured=? WHERE id=?", p.Name, p.Subtitle, p.Description, p.Category, p.Image, p.Price, p.OriginalPrice, p.Status, p.Featured, p.ID)
	}
	if err != nil {
		return p, err
	}
	if err = audit(ctx, tx, actor, "product.save", fmt.Sprint(p.ID)); err != nil {
		return p, err
	}
	return p, tx.Commit()
}
func (d *DB) Restock(ctx context.Context, actor, id, quantity int64) error {
	if quantity <= 0 || quantity > 100000 {
		return bad("补货数量必须为 1～100000")
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	r, err := tx.ExecContext(ctx, "UPDATE mall_products SET stock=stock+?,initial_stock=initial_stock+? WHERE id=?", quantity, quantity, id)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return notFound
	}
	if err = audit(ctx, tx, actor, "product.restock", fmt.Sprintf("%d:+%d", id, quantity)); err != nil {
		return err
	}
	return tx.Commit()
}
func (d *DB) Cart(ctx context.Context, user int64) ([]CartItem, error) {
	rows, err := d.SQL.QueryContext(ctx, "SELECT product_id,quantity FROM mall_cart WHERE user_id=? ORDER BY product_id", user)
	if err != nil {
		return nil, err
	}
	lines := []Line{}
	for rows.Next() {
		var l Line
		if err = rows.Scan(&l.ProductID, &l.Quantity); err != nil {
			rows.Close()
			return nil, err
		}
		lines = append(lines, l)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	items := []CartItem{}
	for _, l := range lines {
		p, err := d.Product(ctx, l.ProductID, true)
		if err != nil {
			return nil, err
		}
		items = append(items, CartItem{p, l.Quantity})
	}
	return items, nil
}
func (d *DB) SetCart(ctx context.Context, user, product, quantity int64) error {
	if product <= 0 || quantity < 0 || quantity > 99 {
		return bad("数量需为 0～99")
	}
	if quantity == 0 {
		_, err := d.SQL.ExecContext(ctx, "DELETE FROM mall_cart WHERE user_id=? AND product_id=?", user, product)
		return err
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM mall_users WHERE id=?"+d.Lock(), user).Scan(&id); err != nil {
		return err
	}
	p, err := scanProduct(tx.QueryRowContext(ctx, "SELECT "+productCols+" FROM mall_products WHERE id=?", product))
	if err != nil {
		return err
	}
	if p.Status != "active" || p.Stock < quantity {
		return conflict("stock_shortage", "商品已下架或库存不足")
	}
	var old int64
	err = tx.QueryRowContext(ctx, "SELECT quantity FROM mall_cart WHERE user_id=? AND product_id=?", user, product).Scan(&old)
	if err == sql.ErrNoRows {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM mall_cart WHERE user_id=?", user).Scan(&count); err != nil {
			return err
		}
		if count >= 30 {
			return bad("购物车最多 30 种商品")
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO mall_cart(user_id,product_id,quantity) VALUES(?,?,?)", user, product, quantity)
	} else if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE mall_cart SET quantity=? WHERE user_id=? AND product_id=?", quantity, user, product)
	}
	if err != nil {
		return err
	}
	return tx.Commit()
}
