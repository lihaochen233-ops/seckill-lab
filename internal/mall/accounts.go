package mall

import (
	"context"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
)

// 密码存储使用每人独立随机盐和 PBKDF2；明文密码绝不写入数据库。
const passwordIterations = 600000

func HashPassword(password string) (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, b, passwordIterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", passwordIterations, hex.EncodeToString(b), hex.EncodeToString(key)), nil
}
func CheckPassword(encoded, password string) bool {
	p := strings.Split(encoded, "$")
	if len(p) != 4 || p[0] != "pbkdf2-sha256" {
		return false
	}
	n, err := strconv.Atoi(p[1])
	if err != nil || n < passwordIterations || n > 2000000 {
		return false
	}
	salt, err := hex.DecodeString(p[2])
	if err != nil {
		return false
	}
	want, err := hex.DecodeString(p[3])
	if err != nil || len(want) != 32 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, n, 32)
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}
func normalizeEmail(s string) (string, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	a, err := mail.ParseAddress(s)
	if err != nil || a.Address != s || len(s) > 160 {
		return "", bad("邮箱格式不正确")
	}
	return s, nil
}
func (d *DB) CreateUser(ctx context.Context, email, name, password, role string) (User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return User{}, err
	}
	name = strings.TrimSpace(name)
	if len([]rune(name)) < 2 || len([]rune(name)) > 30 || len(password) < 10 || len(password) > 128 {
		return User{}, bad("昵称需 2～30 字，密码需 10～128 字节")
	}
	if role != "admin" && role != "customer" {
		return User{}, bad("角色不合法")
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	u := User{Email: email, Name: name, Role: role, CreatedAt: nowMS()}
	r, err := d.SQL.ExecContext(ctx, "INSERT INTO mall_users(email,name,password_hash,role,created_at) VALUES(?,?,?,?,?)", email, name, hash, role, u.CreatedAt)
	if duplicate(err) {
		return User{}, conflict("email_exists", "该邮箱已注册")
	}
	if err != nil {
		return User{}, err
	}
	u.ID, err = r.LastInsertId()
	return u, err
}
func (d *DB) LoginUser(ctx context.Context, email, password string) (User, error) {
	email, err := normalizeEmail(email)
	if err != nil {
		return User{}, &Fault{"invalid_credentials", "邮箱或密码不正确", 401}
	}
	var u User
	var hash string
	err = d.SQL.QueryRowContext(ctx, "SELECT id,email,name,role,created_at,password_hash FROM mall_users WHERE email=?", email).Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &hash)
	if err != nil {
		if missing(err) != notFound {
			return User{}, err
		}
		// 不存在的账号也执行一次密码校验，减少根据响应时间枚举邮箱的机会。
		hash = "pbkdf2-sha256$600000$00000000000000000000000000000000$0000000000000000000000000000000000000000000000000000000000000000"
	}
	valid := CheckPassword(hash, password)
	if err != nil || !valid {
		return User{}, &Fault{"invalid_credentials", "邮箱或密码不正确", 401}
	}
	return u, nil
}
func (d *DB) Addresses(ctx context.Context, user int64) ([]Address, error) {
	rows, err := d.SQL.QueryContext(ctx, "SELECT id,recipient,phone,region,detail FROM mall_addresses WHERE user_id=? ORDER BY id DESC LIMIT 10", user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Address{}
	for rows.Next() {
		a := Address{UserID: user}
		if err = rows.Scan(&a.ID, &a.Recipient, &a.Phone, &a.Region, &a.Detail); err != nil {
			return nil, err
		}
		items = append(items, a)
	}
	return items, rows.Err()
}
func address(ctx context.Context, q querier, user, id int64) (Address, error) {
	a := Address{UserID: user}
	err := q.QueryRowContext(ctx, "SELECT id,recipient,phone,region,detail FROM mall_addresses WHERE id=? AND user_id=?", id, user).Scan(&a.ID, &a.Recipient, &a.Phone, &a.Region, &a.Detail)
	return a, missing(err)
}
func (d *DB) SaveAddress(ctx context.Context, user int64, a Address) (Address, error) {
	if err := a.Validate(); err != nil {
		return a, err
	}
	tx, err := d.SQL.BeginTx(ctx, nil)
	if err != nil {
		return a, err
	}
	defer tx.Rollback()
	var id int64
	if err = tx.QueryRowContext(ctx, "SELECT id FROM mall_users WHERE id=?"+d.Lock(), user).Scan(&id); err != nil {
		return a, err
	}
	if a.ID == 0 {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM mall_addresses WHERE user_id=?", user).Scan(&count); err != nil {
			return a, err
		}
		if count >= 10 {
			return a, bad("最多保存 10 个地址")
		}
		r, e := tx.ExecContext(ctx, "INSERT INTO mall_addresses(user_id,recipient,phone,region,detail) VALUES(?,?,?,?,?)", user, a.Recipient, a.Phone, a.Region, a.Detail)
		if e != nil {
			return a, e
		}
		a.ID, err = r.LastInsertId()
	} else {
		if _, err = address(ctx, tx, user, a.ID); err != nil {
			return a, err
		}
		_, err = tx.ExecContext(ctx, "UPDATE mall_addresses SET recipient=?,phone=?,region=?,detail=? WHERE id=? AND user_id=?", a.Recipient, a.Phone, a.Region, a.Detail, a.ID, user)
	}
	if err != nil {
		return a, err
	}
	a.UserID = user
	return a, tx.Commit()
}
func (d *DB) DeleteAddress(ctx context.Context, user, id int64) error {
	r, err := d.SQL.ExecContext(ctx, "DELETE FROM mall_addresses WHERE id=? AND user_id=?", id, user)
	if err != nil {
		return err
	}
	n, err := r.RowsAffected()
	if err == nil && n == 0 {
		return notFound
	}
	return err
}
